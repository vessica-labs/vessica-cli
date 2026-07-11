package retention

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestParseDurationDays(t *testing.T) {
	d, err := ParseDuration("7d")
	if err != nil || d != MaxTTL {
		t.Fatalf("duration=%s err=%v", d, err)
	}
}

func TestEffectiveExpiryUsesExplicitRetention(t *testing.T) {
	now := time.Now().UTC()
	s := &state.Sandbox{
		ExpiresAt:     now.Add(time.Hour).Format(time.RFC3339Nano),
		RetainedUntil: now.Add(2 * time.Hour).Format(time.RFC3339Nano),
	}
	if got := EffectiveExpiry(s); !got.Equal(now.Add(2 * time.Hour)) {
		t.Fatalf("effective expiry=%s", got)
	}
}

func TestGCDryRunAndDestroyExpired(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(ctx, root, "solo"); err != nil {
		t.Fatal(err)
	}
	s, err := db.CreateSandbox(ctx, "", "docker", "")
	if err != nil {
		t.Fatal(err)
	}
	s.ExpiresAt = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	if err := db.UpdateSandbox(ctx, s); err != nil {
		t.Fatal(err)
	}
	dry, err := GC(ctx, db, root, GCOptions{DryRun: true})
	if err != nil || len(dry.WouldDestroy) != 1 || dry.WouldDestroy[0] != s.ID {
		t.Fatalf("dry=%#v err=%v", dry, err)
	}
	got, err := db.GetSandbox(ctx, s.ID)
	if err != nil || got.Status == "expired" {
		t.Fatalf("dry run mutated sandbox: %#v err=%v", got, err)
	}
	result, err := GC(ctx, db, root, GCOptions{})
	if err != nil || len(result.Destroyed) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	got, err = db.GetSandbox(ctx, s.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("sandbox=%#v err=%v", got, err)
	}
}
