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

func TestPreviewBrokerPreservesRootRelativeRequests(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer target.Close()
	broker := NewPreviewBroker()
	if err := broker.Register("run-1", target.URL, func() {}); err != nil {
		t.Fatal(err)
	}
	capability, err := broker.Issue("run-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(broker)
	defer server.Close()
	client := server.Client()
	first, err := client.Get(server.URL + "/previews/run-1/?cap=" + url.QueryEscape(capability))
	if err != nil {
		t.Fatal(err)
	}
	cookies := first.Cookies()
	body, _ := io.ReadAll(first.Body)
	_ = first.Body.Close()
	if string(body) != "/" || len(cookies) == 0 {
		t.Fatalf("body=%q cookies=%#v", body, cookies)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/assets/app.js", nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	second, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(second.Body)
	_ = second.Body.Close()
	if strings.TrimSpace(string(body)) != "/assets/app.js" {
		t.Fatalf("root-relative request was not proxied: %q", body)
	}
}

func TestPreviewBrokerPresentsUpstreamHost(t *testing.T) {
	var targetHost string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHost = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	broker := NewPreviewBroker()
	if err := broker.Register("run-1", target.URL, func() {}); err != nil {
		t.Fatal(err)
	}
	capability, err := broker.Issue("run-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(broker)
	defer server.Close()
	if _, err := server.Client().Get(server.URL + "/previews/run-1/?cap=" + url.QueryEscape(capability)); err != nil {
		t.Fatal(err)
	}
	if targetHost != targetURL.Host {
		t.Fatalf("upstream host=%q, want %q", targetHost, targetURL.Host)
	}
}

func TestPreviewBrokerInjectsReviewOverlayIntoHTML(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><body><main>Preview</main></body></html>")
	}))
	defer target.Close()
	broker := NewPreviewBroker()
	broker.SetOverlayProvider(func(runID string) string { return `<iframe data-run="` + runID + `"></iframe>` })
	if err := broker.Register("run-1", target.URL, func() {}); err != nil {
		t.Fatal(err)
	}
	capability, err := broker.Issue("run-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(broker)
	defer server.Close()
	resp, err := server.Client().Get(server.URL + "/previews/run-1/?cap=" + url.QueryEscape(capability))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	got := string(body)
	if !strings.Contains(got, `<iframe data-run="run-1"></iframe></body>`) {
		t.Fatalf("overlay was not injected before body close: %s", got)
	}
}

func TestPreviewEdgeHeaderRoutesRootAssetsAheadOfDashboard(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = io.WriteString(w, "preview:"+r.URL.Path)
	}))
	defer target.Close()
	broker := NewPreviewBroker()
	if err := broker.Register("run-1", target.URL, func() {}); err != nil {
		t.Fatal(err)
	}
	capability, err := broker.Issue("run-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	dashboard := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "dashboard") })
	handler := (&Server{PreviewBroker: broker, PreviewEdgeToken: "edge-secret", Dashboard: dashboard}).Handler()
	request := httptest.NewRequest(http.MethodGet, "/previews/run-1/?cap="+url.QueryEscape(capability), nil)
	request.Header.Set(PreviewEdgeHeader, "edge-secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("preview capability cookie was not set")
	}
	assetRequest := httptest.NewRequest(http.MethodGet, "/app/globals.css", nil)
	assetRequest.Header.Set(PreviewEdgeHeader, "edge-secret")
	assetRequest.AddCookie(cookies[0])
	assetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(assetRecorder, assetRequest)
	if got := assetRecorder.Body.String(); got != "preview:/app/globals.css" {
		t.Fatalf("root asset was not routed to preview: %q", got)
	}
	assetRequest = httptest.NewRequest(http.MethodGet, "/app/globals.css", nil)
	assetRequest.AddCookie(cookies[0])
	assetRecorder = httptest.NewRecorder()
	handler.ServeHTTP(assetRecorder, assetRequest)
	if got := assetRecorder.Body.String(); got != "dashboard" {
		t.Fatalf("untrusted edge request bypassed dashboard: %q", got)
	}
}
