package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestOperatorMetricsExposeWorkspaceScopedCloudAgentSignals(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.EnsureWorkspace(context.Background(), root, "hosted"); err != nil {
		t.Fatal(err)
	}
	_, err = db.AppendActionLedger(context.Background(), state.ActionLedgerInput{ActorID: "user", Tool: "agents_list", PolicyDecision: "denied", ResultJSON: `{"error":"invalid_token"}`, LatencyMS: 25, IdempotencyKey: "denied-one"})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.UpsertSourceCheckpoint(context.Background(), "newsletter", "source-one", `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), `UPDATE source_checkpoints SET updated_at='2026-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	batch, _ := db.CreateOutlookIngestionBatch(context.Background(), state.OutlookIngestionBatchInput{IdempotencyKey: "batch", SubmittedBy: "test"})
	item, _, _ := db.UpsertOutlookIngestionItem(context.Background(), state.OutlookIngestionItemInput{BatchID: batch.ID, SourceID: "message", NormalizedJSON: `{}`})
	if _, err = db.RecordOutlookIngestionReceipt(context.Background(), batch.ID, item.ID, "rejected", `{}`, "invalid"); err != nil {
		t.Fatal(err)
	}

	server := New(appservice.New(db, root, config.Defaults()), "hosted")
	server.ServiceToken = "operator-token"
	user, err := db.UpsertDashboardUser(context.Background(), "github-member", "member", "Member", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.UpsertMembership(context.Background(), user.ID, "member"); err != nil {
		t.Fatal(err)
	}
	const memberSession = "member-session"
	if _, err = db.CreateDashboardSession(context.Background(), user.ID, "member", digest(memberSession), digest("csrf"), state.FormatTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	memberRequest := httptest.NewRequest(http.MethodGet, "/api/v1/operator", nil)
	memberRequest.AddCookie(&http.Cookie{Name: sessionCookie, Value: memberSession})
	memberResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(memberResponse, memberRequest)
	if memberResponse.Code != http.StatusForbidden {
		t.Fatalf("member operator access=%d %s", memberResponse.Code, memberResponse.Body.String())
	}
	request := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer operator-token")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		return rec
	}
	view := request("/api/v1/operator")
	if view.Code != http.StatusOK {
		t.Fatalf("operator=%d %s", view.Code, view.Body.String())
	}
	for _, expected := range []string{`"oauth_failures":1`, `"denied_actions":1`, `"rejected_records":1`, `"source-one"`, `"cos-briefing:morning"`, `"newsletter:daily"`} {
		if !strings.Contains(view.Body.String(), expected) {
			t.Fatalf("operator response missing %s: %s", expected, view.Body.String())
		}
	}
	metrics := request("/internal/dashboard/metrics")
	if metrics.Code != http.StatusOK {
		t.Fatalf("metrics=%d %s", metrics.Code, metrics.Body.String())
	}
	for _, expected := range []string{"vessica_mcp_errors_total 1", "vessica_mcp_latency_milliseconds_sum 25", "vessica_ingestion_rejected_records_total 1", "vessica_missing_briefings 3"} {
		if !strings.Contains(metrics.Body.String(), expected) {
			t.Fatalf("metrics missing %q: %s", expected, metrics.Body.String())
		}
	}
}
