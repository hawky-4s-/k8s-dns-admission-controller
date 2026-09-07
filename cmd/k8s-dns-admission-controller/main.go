package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hawky-4s-/k8s-dns-admission-controller/internal/admission"
	"github.com/hawky-4s-/k8s-dns-admission-controller/internal/config"
	"github.com/hawky-4s-/k8s-dns-admission-controller/internal/logging"
	"github.com/hawky-4s-/k8s-dns-admission-controller/internal/metrics"
	"github.com/hawky-4s-/k8s-dns-admission-controller/internal/server"
)

func main() {
	// 1. Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// 2. Setup logging
	logger := logging.NewLogger(cfg.LogLevel, cfg.LogFormat, os.Stdout)
	slog.SetDefault(logger)
	logger.Info("Configuration applied", "config", cfg)

	// 3. Setup metrics
	reg := prometheus.NewRegistry()
	metricsRecorder := metrics.NewRecorder(reg)

	// 4. Initialize components
	mutator := admission.NewMutator(cfg, logger)
	handler := admission.NewHandlerWithMetrics(mutator, logger, metricsRecorder)

	// 5. Create server config
	srvCfg := server.Config{
		Port:        cfg.Port,
		TLSCertPath: cfg.TLSCertPath,
		TLSKeyPath:  cfg.TLSKeyPath,
		Timeout:     cfg.Timeout,
	}

	// 6. Create router
	mux := http.NewServeMux()

	// Register application routes
	mux.HandleFunc("/mutate", handler.HandleMutate)

	// Server setup
	srv, err := server.New(srvCfg, mux)
	if err != nil {
		logger.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	// Register health endpoints
	mux.HandleFunc("/healthz", srv.HandleHealthz)
	mux.HandleFunc("/readyz", srv.HandleReadyz)

	// 7. Start metrics server
	metricsSrv := metrics.NewServer(reg, cfg.MetricsPort)
	go func() {
		logger.Info("starting metrics server", "port", cfg.MetricsPort)
		if err := metricsSrv.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server failed", "error", err)
		}
	}()

	// 8. Start webhook server in background
	go func() {
		logger.Info("starting webhook server", "port", cfg.Port)
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	logger.Info("shutting down servers...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown both servers
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("webhook server shutdown failed", "error", err)
	}
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics server shutdown failed", "error", err)
	}

	logger.Info("servers stopped")
}
