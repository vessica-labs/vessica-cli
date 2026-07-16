package state

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
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
	user, err := db.UpsertDashboardUser(ctx, "12345", "VessicaMember", "Vessica Member", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.UpsertMembership(ctx, user.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err = db.CreateDashboardSession(ctx, user.ID, "owner", "postgres-session", "postgres-csrf", expires); err != nil {
		t.Fatal(err)
	}
	invitation, err := db.CreateInvitation(ctx, "CaseSensitiveLogin", "member", "postgres-invitation", expires, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	member, err := db.UpsertDashboardUser(ctx, "67890", "casesensitivelogin", "Member", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AcceptInvitation(ctx, invitation.TokenHash, "CASESENSITIVELOGIN", member.ID); err != nil {
		t.Fatal(err)
	}
	operation, err := db.CreateHostingOperation(ctx, "railway_promotion", "postgres-operation", user.ID, map[string]any{"preview_origin": "https://preview.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendHostingOperationEvent(ctx, operation.ID, "verify", "running", "verifying", nil); err != nil {
		t.Fatal(err)
	}

	epic, err := db.CreateEpic(ctx, "Postgres concurrency", "atomic state transitions")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "test", "high", "railway", 1, false, "none", "", "")
	if err != nil {
		t.Fatal(err)
	}
	const eventCount = 32
	sequences := make(chan int64, eventCount)
	errors := make(chan error, eventCount)
	var wg sync.WaitGroup
	for i := 0; i < eventCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			event, appendErr := db.AppendEvent(ctx, runRecord.ID, "", "postgres.concurrent", map[string]any{"i": i})
			if appendErr != nil {
				errors <- appendErr
				return
			}
			sequences <- event.Seq
		}(i)
	}
	wg.Wait()
	close(errors)
	for appendErr := range errors {
		t.Fatalf("append concurrent Postgres event: %v", appendErr)
	}
	close(sequences)
	var got []int
	for sequence := range sequences {
		got = append(got, int(sequence))
	}
	sort.Ints(got)
	for i, sequence := range got {
		if sequence != i+1 {
			t.Fatalf("Postgres sequences=%v", got)
		}
	}

	ticket, err := db.CreateTicket(ctx, epic.ID, "feature", "Atomic claim", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimStart := make(chan struct{})
	claimResults := make(chan error, 2)
	for _, agent := range []string{"postgres-agent-1", "postgres-agent-2"} {
		wg.Add(1)
		go func(agent string) {
			defer wg.Done()
			<-claimStart
			_, _, claimErr := db.ClaimTicket(ctx, ticket.ID, agent, time.Minute)
			claimResults <- claimErr
		}(agent)
	}
	close(claimStart)
	wg.Wait()
	close(claimResults)
	winners := 0
	for claimErr := range claimResults {
		if claimErr == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("Postgres claim winners=%d, want 1", winners)
	}
}
