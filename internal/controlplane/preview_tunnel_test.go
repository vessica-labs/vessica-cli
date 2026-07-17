package controlplane

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSandboxInitiatedPreviewTunnelPublishesCapabilityURL(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "tunneled "+r.URL.Path)
	}))
	defer target.Close()
	broker := NewPreviewBroker()
	if err := broker.RegisterTunnel("run-tunnel"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer((&Server{PreviewBroker: broker, WorkerDownloadToken: "worker"}).Handler())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = RunPreviewTunnel(ctx, server.URL, "run-tunnel", target.URL, "worker") }()
	capability, _ := broker.Issue("run-tunnel", time.Minute)
	response, err := server.Client().Get(server.URL + "/previews/run-tunnel/example?cap=" + url.QueryEscape(capability))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "tunneled /example") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
}
