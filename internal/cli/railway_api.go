package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func listRailwayServices(ctx context.Context, cfg config.Config) ([]railwayServiceRef, error) {
	raw, err := runRailway(ctx, "", nil, "service", "list", "--project", cfg.Hosted.ProjectID, "--environment", cfg.Hosted.EnvironmentID, "--json")
	if err != nil {
		return nil, err
	}
	var services []railwayServiceRef
	if err := json.Unmarshal(raw, &services); err != nil {
		return nil, fmt.Errorf("parse Railway service list: %w", err)
	}
	return services, nil
}

func objectString(raw []byte, wanted string) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("parse Railway JSON: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	var find func(any) string
	find = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			if result, ok := x[wanted].(string); ok && result != "" {
				return result
			}
			for _, child := range x {
				if result := find(child); result != "" {
					return result
				}
			}
		case []any:
			for _, child := range x {
				if result := find(child); result != "" {
					return result
				}
			}
		}
		return ""
	}
	if result := find(value); result != "" {
		return result, nil
	}
	return "", fmt.Errorf("Railway response did not include %s", wanted)
}

func railwayCreatedServiceID(raw []byte) (string, error) {
	if id, err := objectString(raw, "serviceId"); err == nil {
		return id, nil
	}
	return objectID(raw)
}

func waitForHostedHealth(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("control plane did not become healthy: %w", lastErr)
}

type railwayDeploymentStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func latestRailwayDeployment(ctx context.Context, cfg config.Config) (railwayDeploymentStatus, error) {
	return latestRailwayDeploymentForService(ctx, cfg, cfg.Hosted.ServiceID)
}

func latestRailwayDeploymentForService(ctx context.Context, cfg config.Config, serviceID string) (railwayDeploymentStatus, error) {
	raw, err := runRailway(ctx, "", nil, "deployment", "list", "-p", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", serviceID, "--limit", "1", "--json")
	if err != nil {
		return railwayDeploymentStatus{}, err
	}
	return parseLatestRailwayDeployment(raw)
}

func parseLatestRailwayDeployment(raw []byte) (railwayDeploymentStatus, error) {
	var deployments []railwayDeploymentStatus
	if err := json.Unmarshal(raw, &deployments); err != nil {
		return railwayDeploymentStatus{}, fmt.Errorf("parse Railway deployments: %w", err)
	}
	if len(deployments) == 0 || deployments[0].ID == "" {
		return railwayDeploymentStatus{}, fmt.Errorf("Railway did not return a deployment")
	}
	return deployments[0], nil
}

func waitForRailwayDeployment(ctx context.Context, cfg config.Config, previousID string, timeout time.Duration) error {
	return waitForRailwayDeploymentForService(ctx, cfg, cfg.Hosted.ServiceID, previousID, timeout)
}

func waitForRailwayDeploymentForService(ctx context.Context, cfg config.Config, serviceID, previousID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var latest railwayDeploymentStatus
	for time.Now().Before(deadline) {
		deployment, err := latestRailwayDeploymentForService(ctx, cfg, serviceID)
		if err == nil {
			latest = deployment
			if deployment.ID != previousID {
				switch strings.ToUpper(deployment.Status) {
				case "SUCCESS":
					return nil
				case "FAILED", "CRASHED", "REMOVED":
					return fmt.Errorf("Railway deployment %s finished with status %s; inspect `ves railway logs`", deployment.ID, deployment.Status)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("Railway deployment did not become successful within %s (latest id=%s status=%s)", timeout, latest.ID, latest.Status)
}

func hostedRequest(ctx context.Context, method, endpoint, token string, body any, target any) error {
	return hostedRequestWithKey(ctx, method, endpoint, token, "", body, target)
}

func hostedRequestWithKey(ctx context.Context, method, endpoint, token, idempotencyKey string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error.Code != "" {
			return fmt.Errorf("hosted API %s (%d): %s", envelope.Error.Code, resp.StatusCode, envelope.Error.Message)
		}
		return fmt.Errorf("hosted API failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if target != nil {
		return json.Unmarshal(data, target)
	}
	return nil
}
