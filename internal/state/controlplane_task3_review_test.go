package state

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestActionExecutionClaimHasOneConcurrentWinnerAndRetriesFailure(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	input := ActionLedgerInput{ActorID: "user", Tool: "conversation_send", PolicyDecision: "allowed", IdempotencyKey: "hashed-key"}
	start := make(chan struct{})
	results := make(chan *ActionExecutionClaim, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claim, err := db.ClaimActionExecution(ctx, input, time.Minute)
			results <- claim
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	winners := 0
	var winner *ActionExecutionClaim
	for claim := range results {
		if claim != nil && claim.Acquired {
			winners++
			winner = claim
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners=%d", winners)
	}
	if err := db.FailActionExecution(ctx, winner.Ledger.ID, winner.ClaimToken, `{"error":"temporary"}`); err != nil {
		t.Fatal(err)
	}
	retry, err := db.ClaimActionExecution(ctx, input, time.Minute)
	if err != nil || !retry.Acquired {
		t.Fatalf("failed action was not retryable: claim=%#v err=%v", retry, err)
	}
	if err = db.CompleteActionExecution(ctx, retry.Ledger.ID, retry.ClaimToken, `{"ok":true}`, 1); err != nil {
		t.Fatal(err)
	}
	replay, err := db.ClaimActionExecution(ctx, input, time.Minute)
	if err != nil || !replay.Replay || replay.Ledger.ResultJSON != `{"ok":true}` {
		t.Fatalf("completed action did not replay: claim=%#v err=%v", replay, err)
	}
}

func TestOutlookFinalizeRejectsStaleExpectedCheckpoint(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	first, _ := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "first", SubmittedBy: "connector"})
	firstValue, secondValue := "2026-08-11T08:00:00-07:00", "2026-08-11T09:00:00-07:00"
	if err := db.FinalizeOutlookIngestionBatch(ctx, first.ID, "", firstValue, `{"candidate":"first"}`, "", firstValue, `{"candidate":"first"}`); err != nil {
		t.Fatal(err)
	}
	stale, _ := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "stale", SubmittedBy: "connector"})
	if err := db.FinalizeOutlookIngestionBatch(ctx, stale.ID, "", secondValue, `{"candidate":"second"}`, firstValue, secondValue, `{"candidate":"second"}`); err == nil {
		t.Fatal("stale email checkpoint was accepted")
	}
	backward, _ := db.CreateOutlookIngestionBatch(ctx, OutlookIngestionBatchInput{IdempotencyKey: "backward", SubmittedBy: "connector"})
	if err := db.FinalizeOutlookIngestionBatch(ctx, backward.ID, firstValue, "2026-08-11T07:00:00-07:00", `{}`, firstValue, secondValue, `{}`); err == nil {
		t.Fatal("backward checkpoint was accepted")
	}
	email, _ := db.GetSourceCheckpoint(ctx, "outlook_email", "outlook")
	calendar, _ := db.GetSourceCheckpoint(ctx, "outlook_calendar", "outlook")
	if email.CheckpointJSON != `{"candidate":"first"}` || calendar.CheckpointJSON != `{"candidate":"first"}` {
		t.Fatalf("stale finalize partially advanced checkpoints: email=%s calendar=%s", email.CheckpointJSON, calendar.CheckpointJSON)
	}
}

func TestOAuthResourceFamilyReplayAndClientRevocation(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	client, err := db.UpsertOAuthClient(ctx, OAuthClientInput{ClientID: "client", Name: "Client"})
	if err != nil {
		t.Fatal(err)
	}
	resource := "https://vessica.example/mcp"
	refresh, err := db.IssueOAuthRefreshToken(ctx, OAuthRefreshTokenInput{ClientID: client.ClientID, ActorID: "user", MaterialHash: "refresh", FamilyID: "family", Resource: resource, ExpiresAt: FormatTime(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConsumeOAuthRefreshToken(ctx, refresh.MaterialHash, client.ClientID, resource); err != nil {
		t.Fatal(err)
	}
	access, err := db.IssueOAuthAccessToken(ctx, OAuthAccessTokenInput{ClientID: client.ClientID, ActorID: "user", TokenHash: "access", FamilyID: "family", Resource: resource, ExpiresAt: FormatTime(time.Now().Add(time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ConsumeOAuthRefreshToken(ctx, refresh.MaterialHash, client.ClientID, resource); err == nil {
		t.Fatal("refresh replay was accepted")
	}
	if _, err = db.GetOAuthAccessToken(ctx, access.TokenHash, resource); err == nil {
		t.Fatal("refresh replay did not revoke family access")
	}
	if _, err = db.IssueOAuthAccessToken(ctx, OAuthAccessTokenInput{ClientID: client.ClientID, ActorID: "user", TokenHash: "wrong-audience", Resource: resource, ExpiresAt: FormatTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOAuthAccessToken(ctx, "wrong-audience", "https://other.example/mcp"); err == nil {
		t.Fatal("access token authorized the wrong MCP resource")
	}
	if _, err = db.Exec(ctx, `UPDATE oauth_clients SET revoked_at=? WHERE id=?`, Now(), client.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GetOAuthAccessToken(ctx, "wrong-audience", resource); err == nil {
		t.Fatal("revoked client access token remained valid")
	}
}
