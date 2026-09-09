package main

import (
	"agentx/server/internal/app"
	"agentx/server/internal/config"
	"agentx/server/internal/telemetry"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	if err := run(logger); err != nil {
		logger.Error("agentx-api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	telemetryCtx, telemetryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	shutdownTelemetry, err := telemetry.Configure(telemetryCtx, "agentx-api")
	telemetryCancel()
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := shutdownTelemetry(shutdownCtx); shutdownErr != nil {
			logger.Error("telemetry shutdown failed", "error", shutdownErr)
		}
	}()
	application, err := app.New(cfg)
	if err != nil {
		return err
	}
	defer application.Close()
	logger.Info("agentx-api listening", "address", cfg.Address)
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           application.Router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case err = <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-signals:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
