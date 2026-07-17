package controlplane

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

const PreviewEdgeHeader = "X-Vessica-Preview-Edge"

// RunPreviewEdge serves a dedicated public preview origin and forwards every
// non-health request to the control plane over Railway's private network. The
// shared header is overwritten at the edge so callers cannot choose its value.
func RunPreviewEdge(ctx context.Context, addr, upstream, token string) error {
	handler, err := NewPreviewEdgeHandler(upstream, token)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func NewPreviewEdgeHandler(upstream, token string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimSpace(upstream))
	hostname := ""
	if target != nil {
		hostname = target.Hostname()
	}
	privateHost := strings.HasSuffix(strings.ToLower(hostname), ".railway.internal") || strings.EqualFold(hostname, "localhost")
	if parsedIP := net.ParseIP(hostname); parsedIP != nil && parsedIP.IsLoopback() {
		privateHost = true
	}
	if err != nil || target == nil || target.Scheme != "http" || target.Host == "" || !privateHost {
		return nil, fmt.Errorf("preview edge upstream must be a private HTTP URL")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("preview edge token is required")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalHost := req.Host
		originalProto := req.Header.Get("X-Forwarded-Proto")
		originalDirector(req)
		req.Host = target.Host
		req.Header.Set(PreviewEdgeHeader, token)
		req.Header.Set("X-Forwarded-Host", originalHost)
		if originalProto != "" {
			req.Header.Set("X-Forwarded-Proto", originalProto)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.Handle("/", proxy)
	return mux, nil
}
