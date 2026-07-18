package controlplane

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerBinaryHeadReturnsStableDigestWithoutBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ves")
	body := []byte("verified worker")
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	server := &Server{WorkerDownloadToken: "token", BinaryPath: path}
	request := httptest.NewRequest(http.MethodHead, "/internal/worker/ves", nil)
	request.Header.Set("Authorization", "Bearer token")
	recorder := httptest.NewRecorder()
	server.handleWorkerBinary(recorder, request)
	want := fmt.Sprintf("%x", sha256.Sum256(body))
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-Vessica-Worker-SHA256") != want || recorder.Body.Len() != 0 {
		t.Fatalf("code=%d digest=%q body=%q", recorder.Code, recorder.Header().Get("X-Vessica-Worker-SHA256"), recorder.Body.String())
	}
}
