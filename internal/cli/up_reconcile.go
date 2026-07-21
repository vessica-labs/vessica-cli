package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

type attachedIntegrationStatus struct {
	Provider   string `json:"provider"`
	Status     string `json:"status"`
	ConfigJSON string `json:"config_json"`
}

func hydrateAttachedTrackerConfig(ctx context.Context, app *App) error {
	secrets, err := loadRailwaySecrets(app.Root)
	if err != nil {
		return err
	}
	var status struct {
		Integration *attachedIntegrationStatus `json:"integration"`
	}
	endpoint := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/status"
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &status); err != nil {
		return err
	}
	trackerConfig, found, err := trackerConfigFromAttachedIntegration(status.Integration)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	app.Config.Tracker = trackerConfig
	return config.Save(app.Root, app.Config)
}

func trackerConfigFromAttachedIntegration(integration *attachedIntegrationStatus) (config.TrackerConfig, bool, error) {
	if integration == nil || !strings.EqualFold(integration.Provider, "linear") || !strings.EqualFold(integration.Status, "connected") {
		return config.TrackerConfig{}, false, nil
	}
	var trackerConfig config.TrackerConfig
	if err := json.Unmarshal([]byte(integration.ConfigJSON), &trackerConfig); err != nil {
		return config.TrackerConfig{}, false, fmt.Errorf("decode hosted tracker configuration: %w", err)
	}
	trackerConfig.Provider = "linear"
	if trackerConfig.Mode == "" {
		trackerConfig.Mode = "best_efforts"
	}
	return trackerConfig, true, nil
}
