// Command mcsm is the daemon entrypoint. It loads config, starts the API
// server, and shuts down cleanly on SIGINT/SIGTERM.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"path/filepath"

	"github.com/MertDalbudak/mcsm/internal/api"
	"github.com/MertDalbudak/mcsm/internal/audit"
	"github.com/MertDalbudak/mcsm/internal/backup"
	"github.com/MertDalbudak/mcsm/internal/buildinfo"
	"github.com/MertDalbudak/mcsm/internal/config"
	"github.com/MertDalbudak/mcsm/internal/discovery"
	"github.com/MertDalbudak/mcsm/internal/logging"
	"github.com/MertDalbudak/mcsm/internal/metrics"
	"github.com/MertDalbudak/mcsm/internal/peers"
	"github.com/MertDalbudak/mcsm/internal/slot"
	"github.com/MertDalbudak/mcsm/internal/system"
)

// tempAdapter satisfies slot.DiscordTempProvider on top of the
// system.Temperature sampler. Lives in main so the slot package
// doesn't need to import internal/system.
type tempAdapter struct{ t *system.Temperature }

func (a tempAdapter) Snapshot() (float64, bool) {
	last, _ := a.t.Snapshot()
	if last == nil {
		return 0, false
	}
	return last.Celsius, true
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "mcsm: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("config", "/etc/mcsm/config.yaml", "path to config.yaml")
		printVer    = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *printVer {
		fmt.Printf("mcsm %s (%s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	}

	cfg, abs, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if _, _, err := logging.Setup(cfg.Logging); err != nil {
		return fmt.Errorf("logging: %w", err)
	}

	slog.Info("mcsm starting",
		"version", buildinfo.Version,
		"commit", buildinfo.Commit,
		"config", abs,
		"instance", cfg.Instance.Name,
		"slots", len(cfg.Slots),
		"discovery_roots", len(cfg.Discovery.Roots),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	disco := discovery.New(cfg.Discovery.Roots, cfg.Instance.Name, cfg.Discovery.ScanInterval)
	go disco.Run(ctx)

	host, _ := os.Hostname()
	slotMgr := slot.NewManager(cfg, host, disco)

	// Wire each slot's Discord /temp command to the temperature sampler
	// once it's constructed below.

	var temp *system.Temperature
	if cfg.System.Temperature != nil && cfg.System.Temperature.Sensor != "" {
		t, err := system.NewTemperature(cfg.System.Temperature.Sensor, 30*time.Second, 60)
		if err != nil {
			slog.Warn("system: temperature monitoring disabled",
				"sensor", cfg.System.Temperature.Sensor, "err", err)
		} else {
			temp = t
			go temp.Run(ctx)
			// Make CPU temperature available to every slot's Discord
			// /temp command. Adapter satisfies slot.DiscordTempProvider.
			adapter := tempAdapter{t: temp}
			for _, sl := range slotMgr.List() {
				sl.SetTempProvider(adapter)
			}
		}
	}

	var auditLog *audit.Logger
	if cfg.Audit.Enabled {
		l, err := audit.New(filepath.Join(cfg.Instance.DataDir, "audit"), cfg.Audit.Retention)
		if err != nil {
			slog.Warn("audit: disabled", "err", err)
		} else {
			auditLog = l
			go auditLog.RunJanitor(ctx)
		}
	}

	var metricsCol *metrics.Collectors
	if cfg.Metrics.Enabled {
		metricsCol = metrics.NewCollectors()
		metricsCol.InstanceInfo.Set(1, cfg.Instance.Name, buildinfo.Version)
	}

	var peerPool *peers.Pool
	if len(cfg.Peers.Peers) > 0 {
		peerPool = peers.NewPool(cfg.Peers)
		if metricsCol != nil {
			peerPool.SetObserver(func(name string, reachable bool, rttMS int64) {
				if reachable {
					metricsCol.PeerReachable.Set(1, name)
					metricsCol.PeerRTT.Set(float64(rttMS)/1000.0, name)
				} else {
					metricsCol.PeerReachable.Set(0, name)
				}
			})
		}
		go peerPool.Run(ctx)
	}

	backupStore, err := backup.New(filepath.Join(cfg.Instance.DataDir, "backups"))
	if err != nil {
		slog.Warn("backups: disabled", "err", err)
	}

	srv, err := api.New(api.Deps{
		Config:      cfg,
		Discovery:   disco,
		Slots:       slotMgr,
		Temperature: temp,
		Audit:       auditLog,
		Metrics:     metricsCol,
		Peers:       peerPool,
		Backups:     backupStore,
	})
	if err != nil {
		return fmt.Errorf("api: %w", err)
	}

	if err := srv.Run(ctx, 15*time.Second); err != nil {
		return fmt.Errorf("api server: %w", err)
	}
	slog.Info("mcsm stopped cleanly")
	return nil
}
