package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/id"
)

type previewTunnelRequest struct {
	ID       string      `json:"id"`
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	RawQuery string      `json:"raw_query,omitempty"`
	Header   http.Header `json:"header,omitempty"`
	Body     []byte      `json:"body,omitempty"`
	result   chan previewTunnelResponse
}

type previewTunnelResponse struct {
	ID     string      `json:"id"`
	Status int         `json:"status"`
	Header http.Header `json:"header,omitempty"`
	Body   []byte      `json:"body,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type previewTunnel struct {
	requests chan *previewTunnelRequest
	mu       sync.Mutex
	pending  map[string]*previewTunnelRequest
}

func (b *PreviewBroker) RegisterTunnel(runID string) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("preview run id is required")
	}
	b.mu.Lock()
	if b.tunnels[runID] == nil {
		b.tunnels[runID] = &previewTunnel{requests: make(chan *previewTunnelRequest, 64), pending: map[string]*previewTunnelRequest{}}
	}
	b.mu.Unlock()
	return nil
}

func (b *PreviewBroker) serveTunnelRequest(w http.ResponseWriter, r *http.Request, tunnel *previewTunnel) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20))
	if err != nil {
		http.Error(w, "preview request is too large", http.StatusRequestEntityTooLarge)
		return
	}
	request := &previewTunnelRequest{
		ID: id.New("preview_request"), Method: r.Method, Path: r.URL.Path,
		RawQuery: r.URL.RawQuery, Header: r.Header.Clone(), Body: body,
		result: make(chan previewTunnelResponse, 1),
	}
	request.Header.Del("Authorization")
	request.Header.Del("Cookie")
	select {
	case tunnel.requests <- request:
	case <-r.Context().Done():
		return
	case <-time.After(10 * time.Second):
		http.Error(w, "preview tunnel is unavailable", http.StatusBadGateway)
		return
	}
	select {
	case response := <-request.result:
		if response.Error != "" {
			http.Error(w, "preview tunnel request failed", http.StatusBadGateway)
			return
		}
		copyPreviewHeaders(w.Header(), response.Header)
		status := response.Status
		if status < 100 || status > 599 {
			status = http.StatusBadGateway
		}
		w.WriteHeader(status)
		_, _ = w.Write(response.Body)
	case <-r.Context().Done():
	case <-time.After(45 * time.Second):
		http.Error(w, "preview tunnel timed out", http.StatusGatewayTimeout)
	}
}

func copyPreviewHeaders(destination, source http.Header) {
	for name, values := range source {
		switch strings.ToLower(name) {
		case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailers", "transfer-encoding", "upgrade", "set-cookie":
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func (b *PreviewBroker) handleTunnelPoll(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	b.mu.RLock()
	tunnel := b.tunnels[runID]
	b.mu.RUnlock()
	if tunnel == nil {
		writeAPIError(w, http.StatusNotFound, "preview_tunnel_not_found", "preview tunnel is not registered")
		return
	}
	select {
	case request := <-tunnel.requests:
		tunnel.mu.Lock()
		tunnel.pending[request.ID] = request
		tunnel.mu.Unlock()
		writeJSON(w, http.StatusOK, request)
	case <-r.Context().Done():
	case <-time.After(25 * time.Second):
		w.WriteHeader(http.StatusNoContent)
	}
}

func (b *PreviewBroker) handleTunnelResponse(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	var response previewTunnelResponse
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 10<<20)).Decode(&response); err != nil || response.ID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_tunnel_response", "preview tunnel response is invalid")
		return
	}
	b.mu.RLock()
	tunnel := b.tunnels[runID]
	b.mu.RUnlock()
	if tunnel == nil {
		writeAPIError(w, http.StatusNotFound, "preview_tunnel_not_found", "preview tunnel is not registered")
		return
	}
	tunnel.mu.Lock()
	request := tunnel.pending[response.ID]
	delete(tunnel.pending, response.ID)
	tunnel.mu.Unlock()
	if request == nil {
		writeAPIError(w, http.StatusNotFound, "preview_request_not_found", "preview request is no longer pending")
		return
	}
	select {
	case request.result <- response:
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunPreviewTunnel keeps a sandbox-initiated authenticated connection to the
// control plane and relays requests to the engine-owned loopback server.
func RunPreviewTunnel(ctx context.Context, controlURL, runID, localURL, token string) error {
	base, err := url.Parse(strings.TrimRight(controlURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("invalid control-plane URL")
	}
	local, err := url.Parse(strings.TrimRight(localURL, "/"))
	if err != nil || local.Scheme != "http" || local.Host == "" {
		return fmt.Errorf("invalid local preview URL")
	}
	client := &http.Client{Timeout: 40 * time.Second}
	errors := make(chan error, 8)
	for worker := 0; worker < cap(errors); worker++ {
		go func() { errors <- runPreviewTunnelWorker(ctx, client, base, local, runID, token) }()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errors:
		return err
	}
}

func runPreviewTunnelWorker(ctx context.Context, client *http.Client, base, local *url.URL, runID, token string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pollURL := base.String() + "/internal/preview-tunnels/" + url.PathEscape(runID) + "/poll"
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, pollURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if resp.StatusCode == http.StatusNoContent {
			_ = resp.Body.Close()
			continue
		}
		var work previewTunnelRequest
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&work)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || decodeErr != nil {
			time.Sleep(time.Second)
			continue
		}
		result := executeTunnelRequest(ctx, client, local, &work)
		encoded, _ := json.Marshal(result)
		responseURL := base.String() + "/internal/preview-tunnels/" + url.PathEscape(runID) + "/responses"
		responseReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, responseURL, bytes.NewReader(encoded))
		responseReq.Header.Set("Authorization", "Bearer "+token)
		responseReq.Header.Set("Content-Type", "application/json")
		if response, responseErr := client.Do(responseReq); responseErr == nil {
			_ = response.Body.Close()
		}
	}
}

func executeTunnelRequest(ctx context.Context, client *http.Client, local *url.URL, work *previewTunnelRequest) previewTunnelResponse {
	target := *local
	target.Path = work.Path
	target.RawQuery = work.RawQuery
	req, err := http.NewRequestWithContext(ctx, work.Method, target.String(), bytes.NewReader(work.Body))
	if err != nil {
		return previewTunnelResponse{ID: work.ID, Error: "invalid local request"}
	}
	copyPreviewHeaders(req.Header, work.Header)
	resp, err := client.Do(req)
	if err != nil {
		return previewTunnelResponse{ID: work.ID, Error: "local preview unavailable"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return previewTunnelResponse{ID: work.ID, Error: "read local preview response"}
	}
	return previewTunnelResponse{ID: work.ID, Status: resp.StatusCode, Header: resp.Header.Clone(), Body: body}
}
