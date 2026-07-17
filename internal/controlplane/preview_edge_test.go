package controlplane

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPreviewEdgeAuthenticatesAndRewritesUpstream(t *testing.T) {
	var gotHost, gotToken, gotForwardedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotToken = r.Header.Get(PreviewEdgeHeader)
		gotForwardedHost = r.Header.Get("X-Forwarded-Host")
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer upstream.Close()
	handler, err := NewPreviewEdgeHandler(upstream.URL, "edge-secret")
	if err != nil {
		t.Fatal(err)
	}
	edge := httptest.NewServer(handler)
	defer edge.Close()
	req, _ := http.NewRequest(http.MethodGet, edge.URL+"/app/globals.css", nil)
	req.Host = "preview.example.test"
	req.Header.Set(PreviewEdgeHeader, "spoofed")
	response, err := edge.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	parsed, _ := url.Parse(upstream.URL)
	if string(body) != "/app/globals.css" || gotHost != parsed.Host || gotToken != "edge-secret" || gotForwardedHost != "preview.example.test" {
		t.Fatalf("body=%q host=%q token=%q forwarded_host=%q", body, gotHost, gotToken, gotForwardedHost)
	}
}

func TestPreviewEdgeHealthDoesNotReachUpstream(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { upstreamCalls++ }))
	defer upstream.Close()
	handler, err := NewPreviewEdgeHandler(upstream.URL, "edge-secret")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || upstreamCalls != 0 {
		t.Fatalf("status=%d upstream_calls=%d", recorder.Code, upstreamCalls)
	}
}
