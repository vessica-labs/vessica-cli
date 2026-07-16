package state

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlPlaneLeaseRejectsSecondReplica(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	first, err := db.AcquireControlPlaneLease(ctx, "control-plane", "holder-1", "deploy-1", "replica-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release(ctx)
	_, err = db.AcquireControlPlaneLease(ctx, "control-plane", "holder-2", "deploy-1", "replica-2", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "multiple control-plane replicas") {
		t.Fatalf("error=%v", err)
	}
}

func TestControlPlaneLeaseAllowsDeploymentHandoff(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("sqlite", filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	first, err := db.AcquireControlPlaneLease(ctx, "control-plane", "holder-1", "deploy-1", "replica-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.AcquireControlPlaneLease(ctx, "control-plane", "holder-2", "deploy-2", "replica-2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release(ctx)
	if err := first.Heartbeat(ctx); err == nil {
		t.Fatal("expected prior deployment to lose its lease")
	}
}
