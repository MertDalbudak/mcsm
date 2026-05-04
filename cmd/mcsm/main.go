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

	"github.com/MertDalbudak/mcsm/internal/api"
	"github.com/MertDalbudak/mcsm/internal/buildinfo"
	"github.com/MertDalbudak/mcsm/internal/config"
	"github.com/MertDalbudak/mcsm/internal/discovery"
	"github.com/MertDalbudak/mcsm/internal/logging"
	"github.com/MertDalbudak/mcsm/internal/slot"
	"github.com/MertDalbudak/mcsm/internal/system"
)

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

	var temp *system.Temperature
	if cfg.System.Temperature != nil && cfg.System.Temperature.Sensor != "" {
		t, err := system.NewTemperature(cfg.System.Temperature.Sensor, 30*time.Second, 60)
		if err != nil {
			slog.Warn("system: temperature monitoring disabled",
				"sensor", cfg.System.Temperature.Sensor, "err", err)
		} else {
			temp = t
			go temp.Run(ctx)
		}
	}

	srv, err := api.New(api.Deps{
		Config:      cfg,
		Discovery:   disco,
		Slots:       slotMgr,
		Temperature: temp,
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
