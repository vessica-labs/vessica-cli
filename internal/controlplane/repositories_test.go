package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/reposnapshot"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/toolchain"
)

func TestRepositoryAPIIsAuthenticatedAndIdempotent(t *testing.T) {
	root := t.TempDir()
	db, err := state.Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(context.Background(), "hosted://project", "hosted"); err != nil {
		t.Fatal(err)
	}
	server := (&Server{DB: db, APIToken: "secret"}).Handler()
	body := []byte(`{"remote":"git@github.com:acme/service.git"}`)
	attach := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/repositories", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Idempotency-Key", "attach-acme-service")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec
	}
	first, second := attach(), attach()
	if first.Code != http.StatusCreated || second.Code != http.StatusOK {
		t.Fatalf("attach statuses=%d,%d bodies=%s %s", first.Code, second.Code, first.Body, second.Body)
	}
	var one, two state.Repository
	if err := json.Unmarshal(first.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &two); err != nil {
		t.Fatal(err)
	}
	if one.ID == "" || one.ID != two.ID || one.CanonicalRemote != "github.com/acme/service" {
		t.Fatalf("repositories=%#v %#v", one, two)
	}
	checkpoint := reposnapshot.Checkpoint{SchemaVersion: reposnapshot.SchemaVersion, Name: "vessica-repo-test", Status: "ready", ToolchainFingerprint: toolchain.Fingerprint(), VerifiedAt: "2026-07-18T00:00:00Z"}
	checkpointBody, _ := json.Marshal(checkpoint)
	checkpointReq := httptest.NewRequest(http.MethodPut, "/api/v1/repositories/"+one.ID+"/checkpoint", bytes.NewReader(checkpointBody))
	checkpointReq.Header.Set("Authorization", "Bearer secret")
	checkpointRec := httptest.NewRecorder()
	server.ServeHTTP(checkpointRec, checkpointReq)
	if checkpointRec.Code != http.StatusOK {
		t.Fatalf("checkpoint status=%d body=%s", checkpointRec.Code, checkpointRec.Body)
	}
	var checkpointRepository state.Repository
	if err := json.Unmarshal(checkpointRec.Body.Bytes(), &checkpointRepository); err != nil {
		t.Fatal(err)
	}
	stored, ok := reposnapshot.Parse(checkpointRepository.MetadataJSON)
	if !ok || stored.Name != checkpoint.Name {
		t.Fatalf("stored checkpoint=%#v metadata=%s", stored, checkpointRepository.MetadataJSON)
	}
	onboardingBody := []byte(`{"id":"onb_test","repository_id":"` + one.ID + `","status":"running","current_stage":"repository_mapping","document":{"id":"onb_test","status":"running"}}`)
	onboardingReq := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/operations", bytes.NewReader(onboardingBody))
	onboardingReq.Header.Set("Authorization", "Bearer secret")
	onboardingReq.Header.Set("Idempotency-Key", "onboarding:onb_test")
	onboardingRec := httptest.NewRecorder()
	server.ServeHTTP(onboardingRec, onboardingReq)
	if onboardingRec.Code != http.StatusOK {
		t.Fatalf("onboarding status=%d body=%s", onboardingRec.Code, onboardingRec.Body)
	}
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/onboarding/operations/onb_test", nil)
	statusReq.Header.Set("Authorization", "Bearer secret")
	statusRec := httptest.NewRecorder()
	server.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK || !bytes.Contains(statusRec.Body.Bytes(), []byte(`"id":"onb_test"`)) {
		t.Fatalf("onboarding read=%d body=%s", statusRec.Code, statusRec.Body)
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/repositories", nil)
	listReq.Header.Set("Authorization", "Bearer secret")
	listRec := httptest.NewRecorder()
	server.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body)
	}
}
