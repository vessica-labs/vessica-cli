package controlplane

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	runengine "github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type reviewTestLauncher struct{ prompt string }

func (l *reviewTestLauncher) Launch(context.Context, *state.Run) error      { return nil }
func (l *reviewTestLauncher) Destroy(context.Context, *state.Sandbox) error { return nil }
func (l *reviewTestLauncher) Prompt(_ context.Context, _ *state.Run, prompt string) (*runengine.PromptResult, error) {
	l.prompt = prompt
	return &runengine.PromptResult{Status: "completed", Output: "Updated the CTA.", Pushed: true}, nil
}

func TestReviewTokenRoundTripAndExpiry(t *testing.T) {
	server := &Server{APIToken: "review-secret"}
	token := server.reviewToken("run_test", time.Now().Add(time.Hour))
	if !server.verifyReviewToken("run_test", token) {
		t.Fatal("valid review token was rejected")
	}
	if server.verifyReviewToken("run_other", token) || server.verifyReviewToken("run_test", token+"x") {
		t.Fatal("review token was accepted for a different run or after tampering")
	}
	expired := server.reviewToken("run_test", time.Now().Add(-time.Minute))
	if server.verifyReviewToken("run_test", expired) {
		t.Fatal("expired review token was accepted")
	}
}

func TestReviewURLIncludesSignedAction(t *testing.T) {
	server := &Server{APIToken: "review-secret", Config: config.Config{Hosted: config.HostedConfig{ControlPlaneURL: "https://control.example/"}}}
	got := server.reviewURL("run_test", "approve")
	if !strings.HasPrefix(got, "https://control.example/review/runs/run_test?action=approve&token=") {
		t.Fatalf("review URL=%q", got)
	}
}

func TestReviewPanelRequiresPreviewCookieAndRendersControls(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	broker := NewPreviewBroker()
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer target.Close()
	if err := broker.Register(runRecord.ID, target.URL, func() {}); err != nil {
		t.Fatal(err)
	}
	capability, err := broker.Issue(runRecord.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server.PreviewBroker = broker
	req := httptest.NewRequest(http.MethodGet, "/review/runs/"+runRecord.ID+"/panel", nil)
	req.SetPathValue("run_id", runRecord.ID)
	req.Header.Set("Sec-Fetch-Dest", "iframe")
	req.AddCookie(&http.Cookie{Name: previewCookie, Value: capability})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, expected := range []string{"Request a revision", "Accept and Merge", "Rollback", "Pop out", "/window?token=", "new EventSource", "session=1", "messages=new Map", "data-jump", "activity_id", "Jump to latest"} {
		if !strings.Contains(rec.Body.String(), expected) {
			t.Fatalf("review panel missing %q", expected)
		}
	}
	body := rec.Body.String()
	if strings.Contains(body, `resumeEventStream("\"`) || !strings.Contains(body, `resumeEventStream("`) {
		t.Fatalf("review stream token was not emitted as one JavaScript string")
	}
	formData := strings.Index(body, "new FormData(form)")
	disableControls := strings.Index(body, "el.disabled=true")
	if formData < 0 || disableControls < 0 || formData > disableControls {
		t.Fatal("review panel must serialize the prompt before disabling its textarea")
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox") || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers=%v", rec.Header())
	}
}

func TestReviewEventStreamUsesSignedSequenceAndStopsAtCompletion(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	ctx := context.Background()
	if _, err := server.DB.AppendEvent(ctx, runRecord.ID, "", "run.phase.started", map[string]any{"phase": "old"}); err != nil {
		t.Fatal(err)
	}
	activity, err := server.DB.AppendEvent(ctx, runRecord.ID, "", "agent.activity", map[string]any{"kind": "command", "message": "pnpm test", "status": "completed", "activity_id": "item-1"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := server.DB.AppendEvent(ctx, runRecord.ID, "", "sandbox.prompt.completed", map[string]any{"pushed": false})
	if err != nil {
		t.Fatal(err)
	}
	token := server.reviewToken(runRecord.ID, time.Now().Add(time.Hour))

	latestReq := httptest.NewRequest(http.MethodGet, "/review/runs/"+runRecord.ID+"/events?latest=1&token="+url.QueryEscape(token), nil)
	latestReq.SetPathValue("run_id", runRecord.ID)
	latestRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(latestRec, latestReq)
	if latestRec.Code != http.StatusOK || !strings.Contains(latestRec.Body.String(), `"seq":`+fmt.Sprint(completed.Seq)) {
		t.Fatalf("latest status=%d body=%s", latestRec.Code, latestRec.Body.String())
	}

	streamReq := httptest.NewRequest(http.MethodGet, "/review/runs/"+runRecord.ID+"/events?after=1&token="+url.QueryEscape(token), nil)
	streamReq.SetPathValue("run_id", runRecord.ID)
	streamRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(streamRec, streamReq)
	body := streamRec.Body.String()
	if streamRec.Code != http.StatusOK || streamRec.Header().Get("Content-Type") != "text/event-stream" || streamRec.Header().Get("Access-Control-Allow-Origin") != "null" {
		t.Fatalf("stream status=%d headers=%v body=%s", streamRec.Code, streamRec.Header(), body)
	}
	if !strings.Contains(body, fmt.Sprintf("id: %d", activity.Seq)) || !strings.Contains(body, `"type":"agent.activity"`) || !strings.Contains(body, `"type":"sandbox.prompt.completed"`) {
		t.Fatalf("stream missing prompt events: %s", body)
	}
	if strings.Contains(body, "run.phase.started") {
		t.Fatalf("stream exposed unrelated run event: %s", body)
	}
}

func TestReviewEventStreamRejectsUnsignedRequests(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/review/runs/"+runRecord.ID+"/events", nil)
	req.SetPathValue("run_id", runRecord.ID)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReviewEventSessionReplaysLatestPromptForPopout(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	ctx := context.Background()
	_, _ = server.DB.AppendEvent(ctx, runRecord.ID, "", "sandbox.prompt.started", map[string]any{"prompt": "First request"})
	_, _ = server.DB.AppendEvent(ctx, runRecord.ID, "", "sandbox.prompt.completed", map[string]any{"pushed": true})
	started, _ := server.DB.AppendEvent(ctx, runRecord.ID, "", "sandbox.prompt.started", map[string]any{"prompt": "Make the proof cards denser"})
	_, _ = server.DB.AppendEvent(ctx, runRecord.ID, "", "agent.message", map[string]any{"message": "Updating the cards"})
	token := server.reviewToken(runRecord.ID, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/review/runs/"+runRecord.ID+"/events?session=1&token="+url.QueryEscape(token), nil)
	req.SetPathValue("run_id", runRecord.ID)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, expected := range []string{`"found":true`, `"terminal":false`, `"prompt":"Make the proof cards denser"`, `"after":` + fmt.Sprint(started.Seq-1)} {
		if !strings.Contains(rec.Body.String(), expected) {
			t.Fatalf("session missing %s: %s", expected, rec.Body.String())
		}
	}
}

func TestPreviewOverlayHidesWhileDetachedWindowIsOpen(t *testing.T) {
	overlay := (&Server{}).previewOverlay("run_test")
	for _, expected := range []string{"type==='detach'", "f.style.display='none'", "type==='attach'", "f.style.display='block'"} {
		if !strings.Contains(overlay, expected) {
			t.Fatalf("preview overlay missing detached-window behavior %q", expected)
		}
	}
}

func TestReviewStandaloneWindowUsesSignedToken(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	token := server.reviewToken(runRecord.ID, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/review/runs/"+runRecord.ID+"/window?token="+url.QueryEscape(token), nil)
	req.SetPathValue("run_id", runRecord.ID)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, expected := range []string{`class="is-window"`, "Request a revision", "Accept and Merge", "Close", `name="standalone" value="1"`} {
		if !strings.Contains(rec.Body.String(), expected) {
			t.Fatalf("standalone review window missing %q", expected)
		}
	}
	if strings.Contains(rec.Body.String(), `<button class="tool" type="button" data-popout`) {
		t.Fatal("standalone review window rendered a recursive pop-out control")
	}
}

func TestReviewPromptInvokesHostedSandboxPrompter(t *testing.T) {
	server, runRecord, launcher := reviewServerFixture(t)
	token := server.reviewToken(runRecord.ID, time.Now().Add(time.Hour))
	form := url.Values{"token": {token}, "panel": {"1"}, "prompt": {"Make the CTA smaller."}}
	req := httptest.NewRequest(http.MethodPost, "/review/runs/"+runRecord.ID+"/prompt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("run_id", runRecord.ID)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || launcher.prompt != "Make the CTA smaller." {
		t.Fatalf("status=%d prompt=%q body=%s", rec.Code, launcher.prompt, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Changes were committed") || !strings.Contains(rec.Body.String(), "Updated the CTA") || !strings.Contains(rec.Body.String(), "Refresh preview") {
		t.Fatalf("prompt result missing from panel: %s", rec.Body.String())
	}
}

func TestReviewPromptQueuesLinearIssueComment(t *testing.T) {
	server, runRecord, launcher := reviewServerFixture(t)
	connectReviewLinear(t, server, runRecord)
	token := server.reviewToken(runRecord.ID, time.Now().Add(time.Hour))
	form := url.Values{"token": {token}, "panel": {"1"}, "async": {"1"}, "prompt": {"Make the CTA more concise."}}
	req := httptest.NewRequest(http.MethodPost, "/review/runs/"+runRecord.ID+"/prompt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("run_id", runRecord.ID)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || launcher.prompt != "Make the CTA more concise." {
		t.Fatalf("status=%d prompt=%q body=%s", rec.Code, launcher.prompt, rec.Body.String())
	}
	message, err := server.DB.ClaimOutbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message == nil || message.Operation != "linear.comment" || !strings.HasPrefix(message.IdempotencyKey, "linear:review:request:") {
		t.Fatalf("message=%#v", message)
	}
	for _, expected := range []string{"linear-issue-1", "review_request", "Make the CTA more concise.", runRecord.ID, runRecord.PRURL} {
		if !strings.Contains(message.PayloadJSON, expected) {
			t.Fatalf("revision comment missing %q: %s", expected, message.PayloadJSON)
		}
	}
}

func TestReviewDecisionsQueueLinearIssueCommentsBeforeGitHub(t *testing.T) {
	tests := []struct {
		name       string
		action     func(context.Context, *Server, string) error
		key        string
		entityType string
		body       string
	}{
		{name: "approve", action: func(ctx context.Context, server *Server, runID string) error {
			_, err := server.approveRun(ctx, runID)
			return err
		}, key: "linear:run:approved:", entityType: "approval_comment", body: "Accept and Merge"},
		{name: "rollback", action: func(ctx context.Context, server *Server, runID string) error { return server.rollbackRun(ctx, runID) }, key: "linear:run:rollback:", entityType: "rollback_comment", body: "Rollback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, runRecord, _ := reviewServerFixture(t)
			connectReviewLinear(t, server, runRecord)
			runRecord.PRURL = "not-a-pull-request"
			if err := server.DB.UpdateRun(context.Background(), runRecord); err != nil {
				t.Fatal(err)
			}
			if err := test.action(context.Background(), server, runRecord.ID); err == nil {
				t.Fatal("expected malformed PR URL to stop the action")
			}
			message, err := server.DB.ClaimOutbox(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if message == nil || message.Operation != "linear.comment" || message.IdempotencyKey != test.key+runRecord.ID {
				t.Fatalf("message=%#v", message)
			}
			for _, expected := range []string{"linear-issue-1", test.entityType, test.body, runRecord.ID} {
				if !strings.Contains(message.PayloadJSON, expected) {
					t.Fatalf("decision comment missing %q: %s", expected, message.PayloadJSON)
				}
			}
		})
	}
}

func TestReviewPanelPromptReturnsOpaqueOriginJSON(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	token := server.reviewToken(runRecord.ID, time.Now().Add(time.Hour))
	form := url.Values{"token": {token}, "panel": {"1"}, "async": {"1"}, "prompt": {"Verify the CTA."}}
	req := httptest.NewRequest(http.MethodPost, "/review/runs/"+runRecord.ID+"/prompt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("run_id", runRecord.ID)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "null" {
		t.Fatalf("status=%d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) || !strings.Contains(rec.Body.String(), `"refresh":true`) {
		t.Fatalf("async result=%s", rec.Body.String())
	}
}

func TestCompletedRunProjectionAddsReviewLinks(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	ctx := context.Background()
	integration, err := server.DB.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "team-1"}, "", "oauth:linear")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.DB.UpsertExternalMapping(ctx, "linear", "epic", runRecord.EpicID, "linear-issue-1", nil, "synced"); err != nil {
		t.Fatal(err)
	}
	if err := server.SyncRunToLinear(ctx, runRecord.ID); err != nil {
		t.Fatal(err)
	}
	message, err := server.DB.ClaimOutbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if message == nil || message.IntegrationID != integration.ID || message.IdempotencyKey != "linear:run:completed:v4:"+runRecord.ID {
		t.Fatalf("message=%#v", message)
	}
	if !strings.Contains(message.PayloadJSON, "Accept and Merge") || !strings.Contains(message.PayloadJSON, "Rollback") || !strings.Contains(message.PayloadJSON, "/review/runs/") {
		t.Fatalf("completion projection has no review links: %s", message.PayloadJSON)
	}
}

func TestCompletedRunProjectionNeverPublishesRailwayLoopbackPreview(t *testing.T) {
	server, runRecord, _ := reviewServerFixture(t)
	ctx := context.Background()
	runRecord.PreviewURL = "http://127.0.0.1:3000"
	if err := server.DB.UpdateRun(ctx, runRecord); err != nil {
		t.Fatal(err)
	}
	if _, err := server.DB.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "team-1"}, "", "oauth:linear"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.DB.UpsertExternalMapping(ctx, "linear", "epic", runRecord.EpicID, "linear-issue-1", nil, "synced"); err != nil {
		t.Fatal(err)
	}
	if err := server.SyncRunToLinear(ctx, runRecord.ID); err != nil {
		t.Fatal(err)
	}
	message, err := server.DB.ClaimOutbox(ctx)
	if err != nil || message == nil {
		t.Fatalf("message=%#v err=%v", message, err)
	}
	if strings.Contains(message.PayloadJSON, "127.0.0.1") || strings.Contains(message.PayloadJSON, "https://control.example/previews/"+runRecord.ID+"/") {
		t.Fatalf("completion projection used non-public preview: %s", message.PayloadJSON)
	}
}

func reviewServerFixture(t *testing.T) (*Server, *state.Run, *reviewTestLauncher) {
	t.Helper()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.EnsureWorkspace(ctx, root, "hosted"); err != nil {
		t.Fatal(err)
	}
	epic, err := db.CreateEpic(ctx, "Review", "body")
	if err != nil {
		t.Fatal(err)
	}
	runRecord, err := db.CreateRun(ctx, epic.ID, "", "codex", "model", "high", "railway", 1, true, "draft", "", "")
	if err != nil {
		t.Fatal(err)
	}
	runRecord.Status = "completed"
	runRecord.PRURL = "https://github.com/acme/demo/pull/7"
	runRecord.PreviewURL = "https://control.example/previews/" + runRecord.ID + "/?cap=test-capability"
	if err := db.UpdateRun(ctx, runRecord); err != nil {
		t.Fatal(err)
	}
	sandboxRecord, err := db.CreateSandbox(ctx, runRecord.ID, "railway", "vessica/review")
	if err != nil {
		t.Fatal(err)
	}
	sandboxRecord.ContainerID = "sandbox-1"
	sandboxRecord.Status = "running"
	if err := db.UpdateSandbox(ctx, sandboxRecord); err != nil {
		t.Fatal(err)
	}
	launcher := &reviewTestLauncher{}
	server := &Server{DB: db, APIToken: "review-secret", Launcher: launcher, Config: config.Config{Hosted: config.HostedConfig{ControlPlaneURL: "https://control.example"}}}
	return server, runRecord, launcher
}

func connectReviewLinear(t *testing.T, server *Server, runRecord *state.Run) {
	t.Helper()
	ctx := context.Background()
	if _, err := server.DB.UpsertTrackerIntegration(ctx, "linear", "connected", map[string]string{"team": "team-1"}, "", "oauth:linear"); err != nil {
		t.Fatal(err)
	}
	if _, err := server.DB.UpsertExternalMapping(ctx, "linear", "epic", runRecord.EpicID, "linear-issue-1", nil, "synced"); err != nil {
		t.Fatal(err)
	}
}
