package controlplane

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
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
	tunnels         map[string]*previewTunnel
	capabilities    map[string]previewCapability
	overlayProvider func(string) string
}
type previewCapability struct {
	RunID     string
	ExpiresAt time.Time
}

func (b *PreviewBroker) SetOverlayProvider(provider func(string) string) {
	b.mu.Lock()
	b.overlayProvider = provider
	b.mu.Unlock()
}

func NewPreviewBroker() *PreviewBroker {
	return &PreviewBroker{targets: map[string]previewTarget{}, tunnels: map[string]*previewTunnel{}, capabilities: map[string]previewCapability{}}
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
	delete(b.tunnels, token)
	b.mu.Unlock()
	if ok && target.Cancel != nil {
		target.Cancel()
	}
}

func (b *PreviewBroker) Issue(runID string, ttl time.Duration) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, targetOK := b.targets[runID]
	_, tunnelOK := b.tunnels[runID]
	if !targetOK && !tunnelOK {
		return "", fmt.Errorf("preview is not available")
	}
	if ttl <= 0 || ttl > 7*24*time.Hour {
		ttl = 24 * time.Hour
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	capability := base64.RawURLEncoding.EncodeToString(raw)
	b.capabilities[capability] = previewCapability{RunID: runID, ExpiresAt: time.Now().Add(ttl)}
	return capability, nil
}

func (b *PreviewBroker) RestoreCapability(token, runID string) error {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(runID) == "" {
		return fmt.Errorf("preview capability and run id are required")
	}
	b.mu.Lock()
	b.capabilities[token] = previewCapability{RunID: runID, ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
	b.mu.Unlock()
	return nil
}
func (b *PreviewBroker) ResolveCapability(token string) (string, bool) {
	b.mu.RLock()
	capability, ok := b.capabilities[token]
	b.mu.RUnlock()
	return capability.RunID, ok && time.Now().Before(capability.ExpiresAt)
}

func (b *PreviewBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	capabilityToken, runID := "", ""
	if strings.HasPrefix(r.URL.Path, "/previews/") {
		rest := strings.TrimPrefix(r.URL.Path, "/previews/")
		parts := strings.SplitN(rest, "/", 2)
		runID = parts[0]
		capabilityToken = r.URL.Query().Get("cap")
		resolved, valid := b.ResolveCapability(capabilityToken)
		if !valid || resolved != runID {
			http.Error(w, "preview authorization is invalid or expired", http.StatusUnauthorized)
			return
		}
		if len(parts) == 1 || parts[1] == "" {
			r.URL.Path = "/"
		} else {
			r.URL.Path = "/" + parts[1]
		}
		query := r.URL.Query()
		query.Del("cap")
		r.URL.RawQuery = query.Encode()
		secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
		sameSite := http.SameSiteLaxMode
		if secure {
			sameSite = http.SameSiteNoneMode
		}
		http.SetCookie(w, &http.Cookie{Name: previewCookie, Value: capabilityToken, Path: "/", HttpOnly: true, Secure: secure, SameSite: sameSite, MaxAge: 900})
	} else if cookie, err := r.Cookie(previewCookie); err == nil {
		capabilityToken = cookie.Value
		runID, _ = b.ResolveCapability(capabilityToken)
	}
	if runID == "" {
		http.NotFound(w, r)
		return
	}
	b.mu.RLock()
	target, ok := b.targets[runID]
	tunnel := b.tunnels[runID]
	overlayProvider := b.overlayProvider
	b.mu.RUnlock()
	if !ok && tunnel == nil {
		http.Error(w, "preview is no longer available", http.StatusGone)
		return
	}
	if tunnel != nil {
		b.serveTunnelRequest(w, r, tunnel)
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
			overlay := []byte(overlayProvider(runID))
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
