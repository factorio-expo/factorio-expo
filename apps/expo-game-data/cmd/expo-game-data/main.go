package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"factorio-expo/apps/expo-game-data/internal/pipeline"
	"factorio-expo/apps/expo-game-data/internal/portal"
)

func main() {
	dataDir := envOr("DATA_DIR", "/data")
	config := pipeline.Config{
		FactorioRoot:   envOr("FACTORIO_ROOT", "/app/factorio"),
		FactorioBin:    envOr("FACTORIO_BIN", "/app/factorio/bin/x64/factorio"),
		FactorioConfig: envOr("FACTORIO_CONFIG", "/app/config.ini"),
		DataDir:        dataDir,
		ModsetsFile:    envOr("MODSETS_FILE", "/app/modsets.json"),
		ModTablePath:   os.Getenv("MOD_TABLE_OUTPUT"),
		AssetDir:       os.Getenv("ASSET_OUTPUT_DIR"),
		SourceDir:      os.Getenv("SOURCE_OUTPUT_DIR"),
		Portal: &portal.Client{
			HTTP:     &http.Client{Timeout: 30 * time.Minute},
			BaseURL:  portal.DefaultBaseURL,
			Username: os.Getenv("FACTORIO_USERNAME"),
			Token:    os.Getenv("FACTORIO_TOKEN"),
			CacheDir: filepath.Join(dataDir, "mod-cache"),
		},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := pipeline.Run(ctx, config); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
