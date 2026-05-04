// Package api hosts the HTTP server, routing, middleware, and handlers
// for the MCSM REST surface documented in docs/api.md.
package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/MertDalbudak/mcsm/internal/audit"
	"github.com/MertDalbudak/mcsm/internal/config"
	"github.com/MertDalbudak/mcsm/internal/discovery"
	"github.com/MertDalbudak/mcsm/internal/metrics"
	"github.com/MertDalbudak/mcsm/internal/peers"
	"github.com/MertDalbudak/mcsm/internal/slot"
	"github.com/MertDalbudak/mcsm/internal/system"
)

// Server is the HTTP entrypoint. Sub-systems (discovery store, slot
// manager, peer pool, etc.) are injected as fields and accessed by the
// handler files in this package.
type Server struct {
	cfg       *config.Config
	auth      *Authenticator
	disco     *discovery.Store
	slotMgr   *slot.Manager
	temp      *system.Temperature // nil if not configured
	audit     *audit.Logger       // nil if disabled
	metrics   *metrics.Collectors // nil if disabled
	peers     *peers.Pool         // nil if no peers configured
	startedAt time.Time
	ready     atomic.Bool

	httpServer *http.Server
}

// Deps wires the subsystems Server depends on. Constructed in main.go.
type Deps struct {
	Config      *config.Config
	Discovery   *discovery.Store
	Slots       *slot.Manager
	Temperature *system.Temperature
	Audit       *audit.Logger
	Metrics     *metrics.Collectors
	Peers       *peers.Pool
}

// New constructs the Server but does not bind a port. Call Run.
func New(deps Deps) (*Server, error) {
	auth, err := NewAuthenticator(deps.Config.API.Tokens)
	if err != nil {
		return nil, fmt.Errorf("authenticator: %w", err)
	}
	return &Server{
		cfg:       deps.Config,
		auth:      auth,
		disco:     deps.Discovery,
		slotMgr:   deps.Slots,
		temp:      deps.Temperature,
		audit:     deps.Audit,
		metrics:   deps.Metrics,
		peers:     deps.Peers,
		startedAt: time.Now().UTC(),
	}, nil
}

// Run binds and serves. It blocks until ctx is canceled, then attempts a
// graceful shutdown bounded by shutdownTimeout.
func (s *Server) Run(ctx context.Context, shutdownTimeout time.Duration) error {
	mux := http.NewServeMux()
	s.register(mux)

	// Order matters: requestID must be outermost so the trace id exists
	// for both recoverer (panic logs) and accessLog. The audit middleware
	// runs innermost so it sees the final response status.
	handler := chain(requestID, recoverer, accessLog, s.auditMiddleware)(mux)

	s.httpServer = &http.Server{
		Addr:              s.cfg.API.Bind,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      0, // 0 so WS streams aren't killed; per-handler deadlines still apply
		IdleTimeout:       120 * time.Second,
	}

	listener, err := net.Listen("tcp", s.cfg.API.Bind)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.cfg.API.Bind, err)
	}

	tlsCfg, err := s.tlsConfig()
	if err != nil {
		return err
	}
	if tlsCfg != nil {
		s.httpServer.TLSConfig = tlsCfg
		listener = tls.NewListener(listener, tlsCfg)
		slog.Info("api listening", "addr", s.cfg.API.Bind, "tls", true)
	} else {
		slog.Info("api listening", "addr", s.cfg.API.Bind, "tls", false)
	}

	s.ready.Store(true)

	errCh := make(chan error, 1)
	go func() {
		err := s.httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.ready.Store(false)
		slog.Info("api shutting down", "timeout", shutdownTimeout)
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}
}

func (s *Server) tlsConfig() (*tls.Config, error) {
	t := s.cfg.API.TLS
	if t == nil {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load tls keypair: %w", err)
	}
	min := uint16(tls.VersionTLS12)
	if t.MinVersion == "1.3" {
		min = tls.VersionTLS13
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   min,
		NextProtos:   []string{"h2", "http/1.1"},
	}, nil
}
