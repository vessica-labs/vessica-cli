package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

type linearIntegrationOptions struct {
	Team, Project, TodoState, WIPState, DoneState, BlockedState, TriggerLabel string
}

type hostedLinearStatus struct {
	Integration *struct {
		Status     string `json:"status"`
		ConfigJSON string `json:"config_json"`
	} `json:"integration"`
}

func connectLinearIntegration(ctx context.Context, app *App, opts linearIntegrationOptions) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(app.Root)
	if err != nil {
		return nil, fmt.Errorf("load hosted credentials: %w", err)
	}
	if secrets.WebhookSecret == "" {
		return nil, fmt.Errorf("hosted Linear webhook secret is not configured")
	}
	linearOAuth, _ := auth.MarshalOAuth("linear")
	linearToken, err := auth.Token("linear")
	if err != nil {
		return nil, err
	}
	linear := tracker.NewLinearClient(linearToken)
	discovery, err := linear.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover Linear workspace: %w", err)
	}

	current, connected := hostedLinearConfig(ctx, app, secrets)
	resolveOpts := railwayUpOptions{
		Team:         firstNonEmpty(opts.Team, current.TeamID),
		TodoState:    firstNonEmpty(opts.TodoState, current.TodoStateID),
		WIPState:     firstNonEmpty(opts.WIPState, current.WIPStateID),
		DoneState:    firstNonEmpty(opts.DoneState, current.DoneStateID),
		BlockedState: firstNonEmpty(opts.BlockedState, current.BlockedStateID),
	}
	team, states, err := resolveLinearConfig(discovery, resolveOpts)
	if err != nil {
		return nil, err
	}
	existingProjectID := current.ProjectID
	if opts.Team != "" && team.ID != current.TeamID && opts.Project == "" {
		existingProjectID = ""
	}
	project, err := resolveLinearProject(discovery, team.ID, opts.Project, existingProjectID)
	if err != nil {
		return nil, err
	}
	desired := config.TrackerConfig{
		Provider:       "linear",
		Mode:           "best_efforts",
		TeamID:         team.ID,
		ProjectID:      project.ID,
		TodoStateID:    states["todo"],
		WIPStateID:     states["wip"],
		DoneStateID:    states["done"],
		BlockedStateID: states["blocked"],
		TriggerLabel:   resolvedTriggerLabel(opts.TriggerLabel, current.TriggerLabel),
	}
	app.Config.Tracker = desired
	if connected && sameTrackerConfig(current, desired) && secrets.WebhookID != "" {
		if err := saveHostedClientConfig(app.Config, secrets); err != nil {
			return nil, err
		}
		return linearIntegrationResult(team, project, secrets.WebhookID, true), nil
	}

	if desired.TriggerLabel != "" {
		if _, err := linear.EnsureIssueLabel(ctx, team.ID, desired.TriggerLabel); err != nil {
			return nil, fmt.Errorf("ensure Linear trigger label: %w", err)
		}
	}
	if secrets.WebhookID == "" {
		webhook, err := linear.CreateWebhook(ctx, team.ID, strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/")+"/webhooks/linear", secrets.WebhookSecret)
		if err != nil {
			return nil, fmt.Errorf("create Linear webhook: %w", err)
		}
		secrets.WebhookID = webhook.ID
		if err := saveRailwaySecrets(app.Root, secrets); err != nil {
			return nil, err
		}
	}

	hostedLinearToken := linearToken
	if linearOAuth != "" {
		hostedLinearToken = ""
	}
	variables := map[string]string{
		"VES_TRACKER_PROVIDER":        desired.Provider,
		"VES_LINEAR_TEAM_ID":          desired.TeamID,
		"VES_LINEAR_PROJECT_ID":       desired.ProjectID,
		"VES_LINEAR_TODO_STATE_ID":    desired.TodoStateID,
		"VES_LINEAR_WIP_STATE_ID":     desired.WIPStateID,
		"VES_LINEAR_DONE_STATE_ID":    desired.DoneStateID,
		"VES_LINEAR_BLOCKED_STATE_ID": desired.BlockedStateID,
		"VES_LINEAR_TRIGGER_LABEL":    desired.TriggerLabel,
		"VES_LINEAR_WEBHOOK_SECRET":   secrets.WebhookSecret,
		"VES_LINEAR_WEBHOOK_ID":       secrets.WebhookID,
		"VES_LINEAR_OAUTH_JSON":       linearOAuth,
		"LINEAR_API_KEY":              hostedLinearToken,
	}
	for key, value := range variables {
		if err := setRailwayVariable(ctx, app.Config, key, value); err != nil {
			return nil, err
		}
	}

	previousDeploymentID := ""
	if latest, err := latestRailwayDeployment(ctx, app.Config); err == nil {
		previousDeploymentID = latest.ID
	}
	if _, err := runRailway(ctx, "", nil, "redeploy", "--project", app.Config.Hosted.ProjectID, "-e", app.Config.Hosted.EnvironmentID, "-s", app.Config.Hosted.ServiceID, "--yes"); err != nil {
		return nil, fmt.Errorf("redeploy control plane with Linear configuration: %w", err)
	}
	if err := waitForRailwayDeployment(ctx, app.Config, previousDeploymentID, 8*time.Minute); err != nil {
		return nil, err
	}
	if err := waitForHostedHealth(ctx, strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/")+"/readyz", 8*time.Minute); err != nil {
		return nil, err
	}
	if err := config.Save(app.Root, app.Config); err != nil {
		return nil, err
	}
	if err := saveHostedClientConfig(app.Config, secrets); err != nil {
		return nil, err
	}
	return linearIntegrationResult(team, project, secrets.WebhookID, false), nil
}

func hostedLinearConfig(ctx context.Context, app *App, secrets railwaySecrets) (config.TrackerConfig, bool) {
	current := app.Config.Tracker
	if app.Config.Hosted.ControlPlaneURL == "" || secrets.APIToken == "" {
		return current, false
	}
	var status hostedLinearStatus
	endpoint := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/status"
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &status); err != nil || status.Integration == nil {
		return current, false
	}
	if err := json.Unmarshal([]byte(status.Integration.ConfigJSON), &current); err != nil {
		return app.Config.Tracker, false
	}
	return current, strings.EqualFold(status.Integration.Status, "connected")
}

func sameTrackerConfig(a, b config.TrackerConfig) bool {
	return a.Provider == b.Provider && a.Mode == b.Mode && a.TeamID == b.TeamID && a.ProjectID == b.ProjectID &&
		a.TodoStateID == b.TodoStateID && a.WIPStateID == b.WIPStateID && a.DoneStateID == b.DoneStateID &&
		a.BlockedStateID == b.BlockedStateID && a.TriggerLabel == b.TriggerLabel
}

func linearIntegrationResult(team tracker.LinearTeam, project tracker.LinearProject, webhookID string, unchanged bool) map[string]any {
	result := map[string]any{
		"provider": "linear", "status": "connected", "team_id": team.ID, "team": team.Name,
		"project_id": project.ID, "project": project.Name, "webhook_id": webhookID, "unchanged": unchanged,
	}
	return result
}
