package controlplane

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
)

const previewCookie = "ves_preview"

type previewTarget struct {
	URL    *url.URL
	Cancel context.CancelFunc
}

// PreviewBroker makes Railway's localhost-only sandbox forwards available
// through the control-plane domain. A cookie preserves root-relative assets
// and websocket requests after the initial tokenized URL.
type PreviewBroker struct {
	mu              sync.RWMutex
	targets         map[string]previewTarget
	overlayProvider func(string) string
}

func (b *PreviewBroker) SetOverlayProvider(provider func(string) string) {
	b.mu.Lock()
	b.overlayProvider = provider
	b.mu.Unlock()
}

func NewPreviewBroker() *PreviewBroker {
	return &PreviewBroker{targets: map[string]previewTarget{}}
}

func (b *PreviewBroker) Register(token, target string, cancel context.CancelFunc) error {
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	b.mu.Lock()
	if old, ok := b.targets[token]; ok && old.Cancel != nil {
		old.Cancel()
	}
	b.targets[token] = previewTarget{URL: u, Cancel: cancel}
	b.mu.Unlock()
	return nil
}

func (b *PreviewBroker) Remove(token string) {
	b.mu.Lock()
	target, ok := b.targets[token]
	delete(b.targets, token)
	b.mu.Unlock()
	if ok && target.Cancel != nil {
		target.Cancel()
	}
}

func (b *PreviewBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := ""
	if strings.HasPrefix(r.URL.Path, "/previews/") {
		rest := strings.TrimPrefix(r.URL.Path, "/previews/")
		parts := strings.SplitN(rest, "/", 2)
		token = parts[0]
		if len(parts) == 1 || parts[1] == "" {
			r.URL.Path = "/"
		} else {
			r.URL.Path = "/" + parts[1]
		}
		http.SetCookie(w, &http.Cookie{Name: previewCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	} else if cookie, err := r.Cookie(previewCookie); err == nil {
		token = cookie.Value
	}
	if token == "" {
		http.NotFound(w, r)
		return
	}
	b.mu.RLock()
	target, ok := b.targets[token]
	overlayProvider := b.overlayProvider
	b.mu.RUnlock()
	if !ok {
		http.Error(w, "preview is no longer available", http.StatusGone)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target.URL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Header.Del("Accept-Encoding")
	}
	if overlayProvider != nil {
		proxy.ModifyResponse = func(resp *http.Response) error {
			if resp.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") || resp.Header.Get("Content-Encoding") != "" {
				return nil
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			overlay := []byte(overlayProvider(token))
			lower := bytes.ToLower(body)
			if index := bytes.LastIndex(lower, []byte("</body>")); index >= 0 {
				body = append(append(append([]byte{}, body[:index]...), overlay...), body[index:]...)
			} else {
				body = append(body, overlay...)
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
			resp.Header.Del("ETag")
			return nil
		}
	}
	proxy.ServeHTTP(w, r)
}
