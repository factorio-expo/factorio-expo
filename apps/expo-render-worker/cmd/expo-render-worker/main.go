package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/artifacts"
	"factorio-expo/apps/expo-render-worker/internal/bridge"
	"factorio-expo/apps/expo-render-worker/internal/config"
	"factorio-expo/apps/expo-render-worker/internal/httpapi"
	"factorio-expo/apps/expo-render-worker/internal/rcon"
	"factorio-expo/apps/expo-render-worker/internal/store"
	"factorio-expo/apps/expo-render-worker/internal/worker"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	jobStore, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("open job store", "error", err)
		os.Exit(1)
	}
	defer jobStore.Close()

	inbox, err := artifacts.NewInbox(cfg.InboxPath, cfg.MaxArtifactBytes, cfg.MaxPixels)
	if err != nil {
		slog.Error("initialize inbox", "error", err)
		os.Exit(1)
	}
	retained, err := artifacts.NewStore(cfg.ArtifactPath, cfg.ArtifactTTL)
	if err != nil {
		slog.Error("initialize artifacts", "error", err)
		os.Exit(1)
	}
	rconClient := rcon.NewClient(cfg.RCONAddr, cfg.RCONPassword, cfg.RCONTimeout)
	defer rconClient.Close()
	bridgeClient := bridge.NewClient(rconClient)
	previewWorker := worker.New(
		"runner-1", jobStore, bridgeClient, inbox, retained,
		cfg.CaptureTimeout, cfg.PollInterval,
	)
	srv := httpapi.New(httpapi.Dependencies{
		Config:    cfg,
		Store:     jobStore,
		Bridge:    bridgeClient,
		Inbox:     inbox,
		Artifacts: retained,
		Worker:    previewWorker,
	})

	go previewWorker.Run(ctx)
	go srv.RunRetention(ctx)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("shut down HTTP server", "error", err)
		}
	}()

	slog.Info("factorio runner listening", "address", cfg.HTTPAddr, "rcon_address", cfg.RCONAddr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("serve runner HTTP", "error", err)
		os.Exit(1)
	}
}
