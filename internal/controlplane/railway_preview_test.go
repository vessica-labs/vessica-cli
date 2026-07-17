package controlplane

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidatePublicPreviewChecksBrowserAssetsWithCapabilityCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/previews/") {
			http.SetCookie(w, &http.Cookie{Name: previewCookie, Value: "capability", Path: "/"})
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<link rel="stylesheet" href="/app/globals.css"><link rel="modulepreload" href="/@id/entry-browser">`))
			return
		}
		if _, err := r.Cookie(previewCookie); err != nil {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("dashboard"))
			return
		}
		switch r.URL.Path {
		case "/app/globals.css":
			if r.Header.Get("Sec-Fetch-Dest") != "style" || !strings.Contains(r.Header.Get("Accept"), "text/css") {
				w.Header().Set("Content-Type", "text/javascript")
				return
			}
			w.Header().Set("Content-Type", "text/css")
		case "/@id/entry-browser":
			if r.Header.Get("Sec-Fetch-Dest") != "script" {
				http.Error(w, "missing script destination", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/javascript")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	if err := validatePublicPreviewAttempt(context.Background(), client, server.URL+"/previews/run-1/?cap=test"); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePublicPreviewRejectsDashboardHTMLForStylesheet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if strings.HasPrefix(r.URL.Path, "/previews/") {
			http.SetCookie(w, &http.Cookie{Name: previewCookie, Value: "capability", Path: "/"})
			_, _ = w.Write([]byte(`<link rel="stylesheet" href="/app/globals.css">`))
			return
		}
		_, _ = w.Write([]byte("dashboard"))
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := server.Client()
	client.Jar = jar
	err := validatePublicPreviewAttempt(context.Background(), client, server.URL+"/previews/run-1/?cap=test")
	if err == nil || !strings.Contains(err.Error(), "stylesheet") || !strings.Contains(err.Error(), "text/html") {
		t.Fatalf("error=%v", err)
	}
}

func TestPublicPreviewURLMovesRetainedCapabilityToConfiguredOrigin(t *testing.T) {
	launcher := &RailwayLauncher{PreviewPublicURL: "https://preview.example.com/"}

	got, err := launcher.publicPreviewURL("run_old", "retained-capability")
	if err != nil {
		t.Fatalf("publicPreviewURL: %v", err)
	}
	want := "https://preview.example.com/previews/run_old/?cap=retained-capability"
	if got != want {
		t.Fatalf("publicPreviewURL = %q, want %q", got, want)
	}
}
