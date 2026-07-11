package state

import (
	"context"
	"os"
	"testing"
)

func TestPostgresHostedSchema(t *testing.T) {
	url := os.Getenv("TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	db, err := Open("postgres-url", url, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(ctx, "postgres-integration", "hosted"); err != nil {
		t.Fatal(err)
	}
	integration, err := db.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "one"}, "", "secret")
	if err != nil {
		t.Fatal(err)
	}
	_, job, duplicate, err := db.ReceiveWebhook(ctx, integration, "postgres-delivery", "Issue", []byte(`{"type":"Issue"}`))
	if err != nil || duplicate || job == nil {
		t.Fatalf("job=%#v duplicate=%v err=%v", job, duplicate, err)
	}
	claimed, err := db.ClaimJob(ctx, "postgres-test", 0)
	if err != nil || claimed == nil {
		t.Fatalf("claimed=%#v err=%v", claimed, err)
	}
}
