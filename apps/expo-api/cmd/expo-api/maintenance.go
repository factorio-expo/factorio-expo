package main

import (
	"context"
	"time"

	"factorio-expo/apps/expo-api/internal/accountdeletion"
	"factorio-expo/apps/expo-api/internal/audit"
	"factorio-expo/apps/expo-api/internal/catalog"
	dbgen "factorio-expo/apps/expo-api/internal/db/generated"
	"factorio-expo/apps/expo-api/internal/maintenance"
	"factorio-expo/apps/expo-api/internal/preview"
	"factorio-expo/apps/expo-api/internal/sessions"
	"factorio-expo/apps/expo-api/internal/staffops"
)

// maintenanceDeps are what the housekeeping tasks work on.
type maintenanceDeps struct {
	catalog  *catalog.Service
	ops      *staffops.Service
	audit    *audit.Log
	deletion *accountdeletion.Service
	sessions *sessions.Service
	files    *preview.Files
	queries  *dbgen.Queries
	previews *preview.Dispatcher
}

// maintenanceRegistry is the one table of the API's housekeeping: what runs
// on a schedule, and the cleanups staff run from the console (listed in this
// order). Every task is idempotent, so replicas may run them side by side.
func maintenanceRegistry(d maintenanceDeps) *maintenance.Registry {
	return maintenance.New(maintenanceTasks(d), maintenance.StaffDeps{
		Totals: func(ctx context.Context) (maintenance.CleanupPreviewTotals, error) {
			totals, err := d.queries.PreviewFileTotals(ctx)
			return maintenance.CleanupPreviewTotals{
				TotalBytes: totals.TotalBytes, FullBytes: totals.FullBytes, FullFiles: totals.FullFiles,
			}, err
		},
		Audit: d.audit,
	})
}

// counted adapts a task that removes n items.
func counted(run func(context.Context) (int, error)) func(context.Context, *int64) (maintenance.CleanupResult, error) {
	return func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
		n, err := run(ctx)
		return maintenance.CleanupResult{Count: int64(n)}, err
	}
}

// pending adapts a dry run that counts items.
func pending(count func(context.Context) (int64, error)) func(context.Context, *int64) (maintenance.Pending, error) {
	return func(ctx context.Context, _ *int64) (maintenance.Pending, error) {
		n, err := count(ctx)
		return maintenance.Pending{Count: n}, err
	}
}

func maintenanceTasks(d maintenanceDeps) []maintenance.Task {
	return []maintenance.Task{
		{
			Name: "deleted_entries", Run: counted(d.ops.HardDeleteDeletedEntries),
			Staff: &maintenance.StaffView{
				Title:       "Hard-delete deleted entries",
				Description: "Removes soft-deleted entries for good: rows, revisions, comments, reports and their preview files. Cannot be undone.",
				Count:       pending(d.ops.CountSweepableDeletedEntries),
			},
		},
		{
			Name: "deleted_accounts", Run: counted(d.deletion.HardDeleteDeletedUsers),
			Staff: &maintenance.StaffView{
				Title:       "Hard-delete deleted accounts",
				Description: "Removes deleted accounts whose entries are all hard-deleted (run the one above first), banned or not: the row, its bans, reports and comment tombstones. The username becomes free (a banned account's stays refused by its ban hold until the ban ends). The API does this daily, with their entries, for accounts deleted over a week ago. Cannot be undone.",
				Count:       pending(d.deletion.CountHardDeletableUsers),
			},
		},
		{
			// The scheduled twin of deleted_accounts: it also removes the
			// entries, and only of accounts past their hold.
			Name: "purge_deleted_accounts", Every: 24 * time.Hour,
			Run: func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
				accounts, _, err := d.deletion.PurgeDeletedAccounts(ctx, time.Now().Add(-accountdeletion.DeletedAccountHold))
				return maintenance.CleanupResult{Count: int64(accounts)}, err
			},
		},
		{
			Name: "expired_entries", Every: time.Minute, Run: counted(d.catalog.SweepExpiredEntries),
			Staff: &maintenance.StaffView{
				Title:       "Sweep expired entries",
				Description: "Soft-deletes expired uploads and hard-deletes expired quick previews now (the API does this every minute).",
				Count:       pending(d.ops.CountExpiredEntries),
			},
		},
		{
			Name: "orphaned_preview_files",
			Run: func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
				count, bytes, err := d.files.DeleteOrphans(ctx, d.queries)
				return maintenance.CleanupResult{Count: int64(count), Bytes: &bytes}, err
			},
			Staff: &maintenance.StaffView{
				Title:       "Delete orphaned preview files",
				Description: "Deletes preview files and records that no preview references.",
				Count: func(ctx context.Context, _ *int64) (maintenance.Pending, error) {
					orphans, err := d.files.FindOrphans(ctx, d.queries)
					return maintenance.Pending{Count: int64(len(orphans.Sources)), Bytes: &orphans.Bytes}, err
				},
			},
		},
		{
			Name: "full_previews",
			Run: func(ctx context.Context, target *int64) (maintenance.CleanupResult, error) {
				count, bytes, err := d.files.EvictTo(ctx, d.queries, *target)
				return maintenance.CleanupResult{Count: int64(count), Bytes: &bytes}, err
			},
			Staff: &maintenance.StaffView{
				Title: "Evict full previews to a size", TakesTarget: true,
				Description: "Deletes full preview images, the oldest first, until all preview files fit the target. Thumbnails stay; a full image is rendered again when someone opens it.",
				Count: func(ctx context.Context, target *int64) (maintenance.Pending, error) {
					eviction, err := d.queries.CountEvictionsToTarget(ctx, *target)
					return maintenance.Pending{Count: eviction.Files, Bytes: &eviction.Bytes}, err
				},
			},
		},
		{
			Name: "expired_address_bans", Every: time.Hour,
			Run: func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
				pruned, err := d.catalog.PruneBanData(ctx)
				return maintenance.CleanupResult{Count: pruned}, err
			},
			Staff: &maintenance.StaffView{
				Title:       "Prune expired address bans and ban holds",
				Description: "Deletes address bans and deleted accounts' ban holds that have ended (the API does this hourly).",
				Count:       pending(d.ops.CountExpiredIPBans),
			},
		},
		{
			Name: "mod_detections", AtStart: true, Run: counted(d.catalog.RedetectMods),
			Staff: &maintenance.StaffView{
				Title:       "Re-detect mods",
				Description: "Re-detects the mods of leaves detected against an older mod table and re-indexes their entries (the API does this at startup).",
				Count:       pending(d.ops.CountStaleModDetections),
			},
		},
		{
			Name: "expired_sessions", Every: time.Hour,
			Run: func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
				pruned, err := d.sessions.PruneExpired(ctx)
				return maintenance.CleanupResult{Count: pruned}, err
			},
		},
		{
			Name: "view_days", Every: 24 * time.Hour,
			Run: func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
				pruned, err := d.catalog.PruneViewDays(ctx)
				return maintenance.CleanupResult{Count: pruned}, err
			},
		},
		{
			Name: "problem_reports", Every: time.Hour,
			Run: func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
				deleted, cleared, err := d.catalog.PruneProblemReports(ctx)
				return maintenance.CleanupResult{Count: deleted + cleared}, err
			},
		},
		{
			// The ship record boards' leaders and their record awards.
			Name: "leaderboards", Every: time.Minute, Run: counted(d.catalog.RefreshBoardLeaders),
		},
		{
			// The XP awards of challenges whose voting ended, once each.
			Name: "challenge_awards", Every: time.Minute, Run: counted(d.catalog.AwardClosedChallenges),
		},
		{
			// The preview file sweep, the storage budget and the safety net
			// for a lost render intent.
			Name: "preview_reconcile", Every: time.Minute,
			Run: func(ctx context.Context, _ *int64) (maintenance.CleanupResult, error) {
				return maintenance.CleanupResult{}, d.previews.Reconcile(ctx)
			},
		},
	}
}
