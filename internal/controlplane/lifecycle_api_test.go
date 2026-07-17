package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type lifecycleTestLauncher struct{ cancelled string }

func (*lifecycleTestLauncher) Launch(context.Context, *state.Run) error             { return nil }
func (*lifecycleTestLauncher) Destroy(context.Context, *state.Sandbox) error        { return nil }
func (l *lifecycleTestLauncher) CancelRun(runID string)                             { l.cancelled = runID }
func (*lifecycleTestLauncher) LaunchFrom(context.Context, *state.Run, string) error { return nil }

func TestHostedResumeAndCancelAreIdempotentAndRetainSandbox(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(ctx, root, "hosted"); err != nil {
		t.Fatal(err)
	}
	epic, _ := db.CreateEpic(ctx, "Repair", "body")
	runRecord, _ := db.CreateRun(ctx, epic.ID, "", "codex", "model", "high", "railway", 1, true, "draft", "", "")
	runRecord.Status, runRecord.CurrentPhase = "failed", "validate"
	if err := db.UpdateRun(ctx, runRecord); err != nil {
		t.Fatal(err)
	}
	sandboxRecord, _ := db.CreateSandbox(ctx, runRecord.ID, "railway", "vessica/repair")
	sandboxRecord.ContainerID, sandboxRecord.Status = "sandbox-retained", "running"
	if err := db.UpdateSandbox(ctx, sandboxRecord); err != nil {
		t.Fatal(err)
	}
	launcher := &lifecycleTestLauncher{}
	handler := (&Server{DB: db, APIToken: "api", Launcher: launcher, Config: config.TeamDefaults()}).Handler()

	resumeBody := []byte(`{"from":"validate"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runRecord.ID+"/resume", bytes.NewReader(resumeBody))
	request.Header.Set("Authorization", "Bearer api")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !bytes.Contains(recorder.Body.Bytes(), []byte("idempotency_required")) {
		t.Fatalf("missing key status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	resume := func() string {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runRecord.ID+"/resume", bytes.NewReader(resumeBody))
		request.Header.Set("Authorization", "Bearer api")
		request.Header.Set("Idempotency-Key", "resume-once")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusAccepted && recorder.Code != http.StatusOK {
			t.Fatalf("resume status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	first, second := resume(), resume()
	var firstResponse, secondResponse runMutationResponse
	_ = json.Unmarshal([]byte(first), &firstResponse)
	_ = json.Unmarshal([]byte(second), &secondResponse)
	if firstResponse.Job == nil || secondResponse.Job == nil || firstResponse.Job.ID != secondResponse.Job.ID {
		t.Fatalf("resume was not replayed: first=%s second=%s", first, second)
	}
	jobs, _ := db.ListJobs(ctx, 20)
	if len(jobs) != 1 || jobs[0].RunID != runRecord.ID {
		t.Fatalf("jobs=%#v", jobs)
	}
	if claimed, err := db.ClaimJob(ctx, "worker-1", time.Hour); err != nil || claimed == nil || claimed.LeaseOwner != "worker-1" {
		t.Fatalf("claim=%#v err=%v", claimed, err)
	}

	cancel := func() {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+runRecord.ID+"/cancel", nil)
		request.Header.Set("Authorization", "Bearer api")
		request.Header.Set("Idempotency-Key", "cancel-once")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("cancel status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	cancel()
	cancel()
	latest, _ := db.GetRun(ctx, runRecord.ID)
	retained, _ := db.GetSandboxForRun(ctx, runRecord.ID)
	jobs, _ = db.ListJobs(ctx, 20)
	if latest.Status != "cancelled" || retained.Status == "destroyed" || jobs[0].Status != "cancelled" || jobs[0].LeaseOwner != "" || launcher.cancelled != runRecord.ID {
		t.Fatalf("run=%#v sandbox=%#v job=%#v cancelled=%q", latest, retained, jobs[0], launcher.cancelled)
	}
}
