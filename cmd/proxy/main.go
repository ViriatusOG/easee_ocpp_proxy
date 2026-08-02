// Command proxy is the Easee OCPP Proxy entry point.
//
// It loads configuration, starts the single-port HTTP+WebSocket server, and shuts
// down gracefully on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Embed the IANA timezone database so schedule timezones (e.g. Europe/London)
	// resolve even on systems without a system zoneinfo (e.g. Windows).
	_ "time/tzdata"

	"github.com/ipeel/easee-ocpp-proxy/internal/config"
	"github.com/ipeel/easee-ocpp-proxy/internal/manager"
	"github.com/ipeel/easee-ocpp-proxy/internal/server"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to the YAML config file")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logger.Error("failed to load config", "path", *cfgPath, "err", err)
		os.Exit(1)
	}
	for _, warning := range cfg.Warnings() {
		logger.Warn("config", "warning", warning)
	}

	m := manager.New(cfg)
	srv := server.New(m, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("listening", "addr", cfg.ListenAddr, "admin", "http://"+cfg.ListenAddr+"/admin")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
}
