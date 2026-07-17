package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

const railwayPreviewEdgeServiceName = "preview-edge"

func ensureRailwayPreviewEdge(ctx context.Context, workDir string, cfg *config.Config, opts railwayUpOptions, secrets railwaySecrets) error {
	if strings.TrimSpace(secrets.PreviewEdgeToken) == "" {
		return fmt.Errorf("preview edge token is required")
	}
	if cfg.Hosted.PreviewServiceID == "" {
		services, err := listRailwayServices(ctx, *cfg)
		if err != nil {
			return err
		}
		for _, service := range services {
			if strings.EqualFold(service.Name, railwayPreviewEdgeServiceName) {
				cfg.Hosted.PreviewServiceID = service.ID
				break
			}
		}
	}
	if cfg.Hosted.PreviewServiceID == "" {
		raw, err := runRailway(ctx, workDir, nil, "add", "--service", railwayPreviewEdgeServiceName, "--json")
		if err != nil {
			return err
		}
		cfg.Hosted.PreviewServiceID, err = railwayCreatedServiceID(raw)
		if err != nil {
			return err
		}
	}
	if cfg.Hosted.PreviewURL == "" {
		raw, err := runRailway(ctx, workDir, nil, "domain", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.PreviewServiceID, "-p", "8080", "--json")
		if err != nil {
			return err
		}
		domain, err := objectString(raw, "domain")
		if err != nil {
			return err
		}
		cfg.Hosted.PreviewURL = "https://" + strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	}
	variables := map[string]string{
		"VES_PREVIEW_UPSTREAM":   "http://control-plane.railway.internal:8080",
		"VES_PREVIEW_EDGE_TOKEN": secrets.PreviewEdgeToken,
	}
	for key, value := range variables {
		if err := setRailwayVariableForService(ctx, *cfg, cfg.Hosted.PreviewServiceID, key, value); err != nil {
			return err
		}
	}
	patch, err := json.Marshal(map[string]any{
		"services": map[string]any{
			cfg.Hosted.PreviewServiceID: map[string]any{
				"deploy": map[string]any{
					"startCommand":            "ves control-plane preview-edge",
					"healthcheckPath":         "/healthz",
					"healthcheckTimeout":      120,
					"restartPolicyType":       "ON_FAILURE",
					"restartPolicyMaxRetries": 5,
				},
			},
		},
	})
	if err != nil {
		return err
	}
	if _, err := runRailway(ctx, "", bytes.NewReader(patch), "environment", "edit",
		"--project", cfg.Hosted.ProjectID, "--environment", cfg.Hosted.EnvironmentID,
		"--message", "Configure Vessica preview edge", "--json"); err != nil {
		return fmt.Errorf("configure Railway preview edge: %w", err)
	}
	previous := ""
	if latest, err := latestRailwayDeploymentForService(ctx, *cfg, cfg.Hosted.PreviewServiceID); err == nil {
		previous = latest.ID
	}
	if opts.Source != "" {
		if _, err := runRailway(ctx, opts.Source, nil, "up", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.PreviewServiceID, "--detach", "--json", "--message", "Vessica preview edge dev"); err != nil {
			return err
		}
	} else {
		if strings.TrimSpace(opts.Image) == "" {
			return fmt.Errorf("preview edge image is required")
		}
		if _, err := runRailway(ctx, "", nil, "service", "source", "connect",
			"--project", cfg.Hosted.ProjectID, "--environment", cfg.Hosted.EnvironmentID,
			"--service", cfg.Hosted.PreviewServiceID, "--image", opts.Image, "--json"); err != nil {
			return fmt.Errorf("configure Railway preview edge image: %w", err)
		}
	}
	if err := waitForRailwayDeploymentForService(ctx, *cfg, cfg.Hosted.PreviewServiceID, previous, 8*time.Minute); err != nil {
		return err
	}
	if err := waitForHostedHealth(ctx, strings.TrimRight(cfg.Hosted.PreviewURL, "/")+"/healthz", 8*time.Minute); err != nil {
		return err
	}
	return nil
}
