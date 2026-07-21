package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

func defaultAgentRuntimeImage() string {
	return "ghcr.io/vessica-labs/vessica-agent-runtime:" + version.Version
}

func ensureRailwayAgentRuntime(ctx context.Context, workDir string, app *App, cfg *config.Config, opts railwayUpOptions, runtimeToken, serviceToken, openAIKey string) error {
	image := firstNonEmpty(opts.AgentRuntimeImage, defaultAgentRuntimeImage())
	if opts.AgentRuntimeSource == "" {
		resolved, err := resolveGHCRDigest(ctx, image)
		if err != nil {
			return fmt.Errorf("resolve agent-runtime release image: %w", err)
		}
		image = resolved
	}
	if cfg.Hosted.AgentRuntimeServiceID == "" {
		services, err := listRailwayServices(ctx, *cfg)
		if err != nil {
			return err
		}
		for _, service := range services {
			if strings.EqualFold(service.Name, "agent-runtime") {
				cfg.Hosted.AgentRuntimeServiceID = service.ID
				break
			}
		}
	}
	if cfg.Hosted.AgentRuntimeServiceID == "" {
		raw, err := runRailway(ctx, workDir, nil, "add", "--service", "agent-runtime", "--json")
		if err != nil {
			return err
		}
		cfg.Hosted.AgentRuntimeServiceID, err = railwayCreatedServiceID(raw)
		if err != nil {
			return err
		}
		if err = config.Save(app.Root, *cfg); err != nil {
			return err
		}
	}
	variables := map[string]string{"VES_CONTROL_PLANE_INTERNAL_URL": "http://control-plane.railway.internal:8080", "VES_AGENT_RUNTIME_TOKEN": runtimeToken, "VES_AGENT_RUNTIME_CONCURRENCY": "4", "VES_AGENT_RUNTIME_PROTOCOL": "vessica.agent-runtime/v1", "RUNTIME_VERSION": version.Version, "OPENAI_API_KEY": openAIKey}
	for key, value := range variables {
		if err := setRailwayVariableForService(ctx, *cfg, cfg.Hosted.AgentRuntimeServiceID, key, value); err != nil {
			return err
		}
	}
	previous := ""
	if latest, err := latestRailwayDeploymentForService(ctx, *cfg, cfg.Hosted.AgentRuntimeServiceID); err == nil {
		previous = latest.ID
	}
	if opts.AgentRuntimeSource != "" {
		if _, err := runRailway(ctx, opts.AgentRuntimeSource, nil, "up", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.AgentRuntimeServiceID, "--detach", "--json", "--message", "Vessica agent runtime dev"); err != nil {
			return err
		}
	} else if cfg.Hosted.AgentRuntimeImage != image {
		if _, err := runRailway(ctx, "", nil, "service", "source", "connect", "--project", cfg.Hosted.ProjectID, "--environment", cfg.Hosted.EnvironmentID, "--service", cfg.Hosted.AgentRuntimeServiceID, "--image", image, "--json"); err != nil {
			return fmt.Errorf("configure agent-runtime image: %w", err)
		}
	} else {
		if _, err := runRailway(ctx, "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Hosted.AgentRuntimeServiceID, "--yes"); err != nil {
			return err
		}
	}
	if err := waitForRailwayDeploymentForService(ctx, *cfg, cfg.Hosted.AgentRuntimeServiceID, previous, 8*time.Minute); err != nil {
		return err
	}
	if err := waitForAgentRuntimeCapabilities(ctx, cfg.Hosted.ControlPlaneURL, serviceToken, openAIKey != "", 2*time.Minute); err != nil {
		return err
	}
	cfg.Hosted.AgentRuntimeImage = image
	cfg.Hosted.AgentRuntimeVersion = version.Version
	if err := config.Save(app.Root, *cfg); err != nil {
		return err
	}
	app.Config = *cfg
	return nil
}

func waitForAgentRuntimeCapabilities(ctx context.Context, controlPlaneURL, token string, requireReady bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	endpoint := strings.TrimRight(controlPlaneURL, "/") + "/api/v1/status"
	for time.Now().Before(deadline) {
		var status struct {
			AgentRuntime struct {
				Connected        bool `json:"connected"`
				CredentialsReady bool `json:"credentials_ready"`
				Accepted         bool `json:"accepted"`
			} `json:"agent_runtime"`
		}
		if err := hostedRequest(ctx, http.MethodGet, endpoint, token, nil, &status); err == nil && status.AgentRuntime.Connected {
			if !requireReady || (status.AgentRuntime.CredentialsReady && status.AgentRuntime.Accepted) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if requireReady {
		return fmt.Errorf("agent runtime did not report compatible ready capabilities")
	}
	return fmt.Errorf("agent runtime did not connect to the control plane")
}
