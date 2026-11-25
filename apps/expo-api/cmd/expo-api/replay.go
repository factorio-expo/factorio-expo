package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/banhold"
	"factorio-expo/apps/expo-api/internal/catalog"
	"factorio-expo/apps/expo-api/internal/config"
	"factorio-expo/apps/expo-api/internal/db"
)

// replayDeletions is `expo-api replay-deletions`: after restoring a database
// dump, it deletes every account in the deleted-accounts ledger
// (API_DELETED_USERS_FILE) that the dump brought back, so deleted data does
// not return to use (docs/admin.md#account-deletion). Run it against the
// restored database before the API serves traffic; it is safe to run twice.
func replayDeletions() int {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay-deletions: load environment:", err)
		return 1
	}
	ids, err := accountdeletion.ReadLedger(cfg.DeletedUsersFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay-deletions:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay-deletions: connect to database:", err)
		return 1
	}
	defer pool.Close()
	banHolds := banhold.New(cfg.IPHashSecret)
	service := catalog.New(pool, catalog.Options{
		PreviewTTL: cfg.QuickPreviewTTL, IPBanDuration: cfg.IPBanDuration,
		BanHolds: banHolds, RenderQueue: cfg.RenderQueue,
	})
	deleted, err := accountdeletion.New(service, banHolds).ReplayDeleted(ctx, ids)
	fmt.Printf("replay-deletions: %d ids in the ledger, %d accounts deleted again\n", len(ids), deleted)
	if err != nil {
		fmt.Fprintln(os.Stderr, "replay-deletions:", err)
		return 1
	}
	return 0
}
