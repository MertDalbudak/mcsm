package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// wsAcceptOptions returns the upgrade options used for every WS endpoint.
// We don't use compression — log streams are mostly small ASCII frames
// and per-message-deflate adds CPU and a known set of CVEs.
//
// OriginPatterns is set from the configured CORS allowlist so a browser
// client can connect from the same origins that the REST API allows.
// When CORS is empty, we still allow same-origin (browser default).
func (s *Server) wsAcceptOptions() *websocket.AcceptOptions {
	opts := &websocket.AcceptOptions{
		InsecureSkipVerify: false,
	}
	if origs := s.cfg.API.CORS.AllowedOrigins; len(origs) > 0 {
		opts.OriginPatterns = origs
	}
	return opts
}

// wsCloseNormal sends a 1000 Normal close frame.
func wsCloseNormal(c *websocket.Conn) {
	_ = c.Close(websocket.StatusNormalClosure, "")
}

// wsKeepAlive runs a ping loop until ctx is canceled or the conn errors.
// MC servers go quiet for long periods (no players online); without
// pings, intermediate proxies kill idle connections.
func wsKeepAlive(ctx context.Context, c *websocket.Conn) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// wsWriteJSON sends one JSON-encoded message as a text frame.
func wsWriteJSON(ctx context.Context, c *websocket.Conn, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, body)
}

// wsReadControl spawns a goroutine that reads incoming text frames
// (clients can send `{ "type": "ping" }` per the spec) and replies
// with `{ "type": "pong" }`. Returns the cancel func and a channel
// closed when the read loop exits.
func wsReadControl(ctx context.Context, c *websocket.Conn) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, body, err := c.Read(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Debug("ws: read", "err", err)
				}
				return
			}
			var msg map[string]any
			if err := json.Unmarshal(body, &msg); err != nil {
				continue
			}
			if t, _ := msg["type"].(string); t == "ping" {
				_ = wsWriteJSON(ctx, c, map[string]string{"type": "pong"})
			}
			// Filter messages (per spec) — accepted but not yet enforced:
			// { "type": "filter", "level": ["warn","error"] }
		}
	}()
	return done
}

// notFound is a small helper for handlers that look up by path value.
func writeNotFound(w http.ResponseWriter, r *http.Request, code, msg string, details map[string]any) {
	WriteError(w, r, http.StatusNotFound, code, msg, details)
}
