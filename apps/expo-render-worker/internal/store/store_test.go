package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"factorio-expo/apps/expo-render-worker/internal/jobs"
)

func TestClaimNextIsFIFOAndExclusive(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, ctx, true)

	first, err := s.Create(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := s.Create(ctx, "second"); err != nil {
		t.Fatal(err)
	}

	claimed, ok, _, err := s.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || claimed.ID != first.ID {
		t.Fatalf("claimed %q, want first job %q", claimed.ID, first.ID)
	}
	if _, ok, _, err := s.ClaimNext(ctx, "worker-b", time.Minute); err != nil || ok {
		t.Fatalf("second worker claim = %v, %v; want no claim", ok, err)
	}
}

// A job still running when the runner restarts was interrupted: the first
// claim after the restart fails it as retryable instead of running it again.
func TestRestartFailsInterruptedJob(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, ctx, true)
	job, err := s.Create(ctx, "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _, err := s.ClaimNext(ctx, "worker-a", time.Hour); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, ctx, false)
	_, ok, interrupted, err := reopened.ClaimNext(ctx, "worker-a", time.Hour)
	if err != nil || ok {
		t.Fatalf("claim after restart = %v, %v; want the interrupted job failed, not claimed", ok, err)
	}
	if len(interrupted) != 1 || interrupted[0] != job.ID {
		t.Fatalf("interrupted = %v, want [%s]", interrupted, job.ID)
	}
	failed, err := reopened.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != jobs.StateFailed || failed.FailureCode != jobs.InterruptedCode ||
		failed.PreviewPhase.Status != jobs.PhaseFailed || failed.PreviewPhase.FailureCode != jobs.InterruptedCode ||
		failed.AnalysisPhase.Status != jobs.PhaseFailed || !jobs.Retryable(failed.FailureCode) {
		t.Fatalf("job = %#v", failed)
	}
}

func TestCompletePersistsArtifact(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, ctx, true)
	created, err := s.Create(ctx, "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, _, err := s.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	now := time.Now().UTC()
	artifact := jobs.Artifact{
		JobID: claimed.ID, RelativePath: filepath.Join(claimed.ID, "preview.png"),
		MediaType: "image/png", Size: 123, SHA256: "abc", Width: 10, Height: 20,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := s.Complete(ctx, "worker-a", artifact); err != nil {
		t.Fatal(err)
	}
	completed, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.State != jobs.StateSucceeded || completed.PreviewArtifact == nil {
		t.Fatalf("completed job = %#v", completed)
	}
}

func TestGetKeepsPreviewAndAnalysisArtifactsSeparated(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, ctx, true)
	created, err := s.Create(ctx, "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, _, err := s.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	now := time.Now().UTC()
	preview := jobs.Artifact{
		JobID: claimed.ID, Phase: jobs.PhasePreview,
		RelativePath: filepath.Join(claimed.ID, "preview.png"),
		MediaType:    "image/png", Size: 123, SHA256: "preview",
		Width: 10, Height: 20, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	analysis := jobs.Artifact{
		JobID: claimed.ID, Phase: jobs.PhaseAnalysis,
		RelativePath: filepath.Join(claimed.ID, "analysis-result.json"),
		MediaType:    "application/json", Size: 456, SHA256: "analysis",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := s.CompletePhase(ctx, "worker-a", claimed.ID, jobs.PhasePreview, &preview); err != nil {
		t.Fatal(err)
	}
	if err := s.CompletePhase(ctx, "worker-a", claimed.ID, jobs.PhaseAnalysis, &analysis); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkArtifactDeleted(ctx, claimed.ID, jobs.PhaseAnalysis, jobs.ArtifactDeleted); err != nil {
		t.Fatal(err)
	}

	completed, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.PreviewArtifact == nil || completed.PreviewArtifact.Phase != jobs.PhasePreview ||
		completed.PreviewArtifact.MediaType != "image/png" || completed.PreviewArtifact.State != jobs.ArtifactAvailable {
		t.Fatalf("preview artifact = %#v", completed.PreviewArtifact)
	}
	if completed.AnalysisArtifact == nil || completed.AnalysisArtifact.Phase != jobs.PhaseAnalysis ||
		completed.AnalysisArtifact.MediaType != "application/json" || completed.AnalysisArtifact.State != jobs.ArtifactDeleted {
		t.Fatalf("analysis artifact = %#v", completed.AnalysisArtifact)
	}
}

func TestCleanupSucceededJobsKeepsOnlyAvailableArtifacts(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, ctx, true)
	created, err := s.Create(ctx, "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, _, err := s.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	now := time.Now().UTC()
	if err := s.Complete(ctx, "worker-a", jobs.Artifact{
		JobID: claimed.ID, RelativePath: filepath.Join(claimed.ID, "preview.png"),
		MediaType: "image/png", Size: 123, SHA256: "abc", Width: 10, Height: 20,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.CleanupSucceededJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Fatalf("cleanup removed %d jobs while artifact is available", deleted)
	}
	if err := s.MarkArtifactDeleted(ctx, created.ID, jobs.PhasePreview, jobs.ArtifactDeleted); err != nil {
		t.Fatal(err)
	}
	deleted, err = s.CleanupSucceededJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("cleanup removed %d jobs, want 1", deleted)
	}
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after cleanup error = %v, want ErrNotFound", err)
	}
}

func openTestStore(t *testing.T, ctx context.Context, clean bool) *Store {
	t.Helper()
	databaseURL := os.Getenv("TEST_RUNNER_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_RUNNER_DATABASE_URL (migrated runner database) to run PostgreSQL runner store tests")
	}
	s, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if clean {
		if _, err := s.db.ExecContext(ctx, `TRUNCATE runner_artifacts, runner_jobs RESTART IDENTITY CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestCreateIdempotentReturnsSameJobForSameKey(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, ctx, true)
	first, err := s.CreateIdempotent(ctx, "key-1", "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CreateIdempotent(ctx, "key-1", "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateIdempotent(ctx, "key-2", "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	keyless, err := s.CreateIdempotent(ctx, "", "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	keylessToo, err := s.CreateIdempotent(ctx, "", "blueprint")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatalf("same key gave %q and %q", first.ID, again.ID)
	}
	if other.ID == first.ID || keyless.ID == keylessToo.ID || keyless.ID == first.ID {
		t.Fatal("distinct or missing keys must create distinct jobs")
	}
}

// A job whose lease expired (the worker hung or crashed) is failed as
// interrupted at the next claim, and the queue moves on.
func TestClaimNextFailsJobsWithExpiredLease(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t, ctx, true)
	hung, err := s.Create(ctx, "hung")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	next, err := s.Create(ctx, "next")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _, err := s.ClaimNext(ctx, "worker-a", time.Minute); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE runner_jobs SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, hung.ID); err != nil {
		t.Fatal(err)
	}
	claimed, ok, interrupted, err := s.ClaimNext(ctx, "worker-a", time.Minute)
	if err != nil || !ok || claimed.ID != next.ID {
		t.Fatalf("claim = %q, %v, %v; want %q", claimed.ID, ok, err, next.ID)
	}
	if len(interrupted) != 1 || interrupted[0] != hung.ID {
		t.Fatalf("interrupted = %v, want [%s]", interrupted, hung.ID)
	}
	job, err := s.Get(ctx, hung.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.State != jobs.StateFailed || job.FailureCode != jobs.InterruptedCode ||
		job.PreviewPhase.Status != jobs.PhaseFailed || job.AnalysisPhase.Status != jobs.PhaseFailed {
		t.Fatalf("job = %#v", job)
	}
}
