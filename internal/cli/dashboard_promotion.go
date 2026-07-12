package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/version"
)

func dashboardPromotion(app *App) func(context.Context, *state.HostingOperation) error {
	return func(ctx context.Context, operation *state.HostingOperation) error {
		stage := func(name, status, message string, detail any) {
			_, _ = app.DB.AppendHostingOperationEvent(ctx, operation.ID, name, status, message, detail)
		}
		fail := func(name string, err error) error {
			stage(name, "failed", err.Error(), nil)
			_ = app.DB.UpdateHostingOperation(ctx, operation.ID, "failed", name, nil, err)
			return err
		}
		if operation.Status != "running" {
			if err := app.DB.BeginHostingOperation(ctx, operation.ID, "prerequisites"); err != nil {
				return err
			}
		}
		stage("prerequisites", "running", "Checking local state and provider credentials", nil)
		snapshot, err := app.DB.ExportWorkplanSnapshot(ctx)
		if err != nil {
			return fail("snapshot", err)
		}
		recoveryPath := filepath.Join(app.Root, ".vessica", "state", "recovery", "promotion-"+operation.ID+".json")
		if err = writePromotionRecoverySnapshot(recoveryPath, &snapshot); err != nil {
			return fail("snapshot", err)
		}
		stage("snapshot", "completed", "Created verified local recovery snapshot", map[string]any{"checksum": snapshot.Checksum, "path": recoveryPath})
		var input struct {
			Name          string `json:"name"`
			PreviewOrigin string `json:"preview_origin"`
		}
		_ = json.Unmarshal([]byte(operation.InputJSON), &input)
		if input.Name == "" {
			input.Name = "vessica-control-plane"
		}
		original := app.Config
		opts := railwayUpOptions{Name: input.Name, Image: "ghcr.io/vessica-labs/vessica-cli:" + version.Version, KnowledgeImage: firstNonEmpty(app.Config.Knowledge.Image, "ghcr.io/vessica-labs/vessica-knowledge-server:v0.3.1"), EmbeddingAPIKeyEnv: "EMBEDDING_API_KEY", PreviewOrigin: input.PreviewOrigin}
		stage("provision", "running", "Provisioning Railway services and pinned images", map[string]any{"control_plane_image": opts.Image, "knowledge_image": opts.KnowledgeImage})
		result, err := railwayUp(ctx, app, opts)
		if err != nil {
			_ = config.Save(app.Root, original)
			app.Config = original
			return fail("provision", err)
		}
		if status, _ := result["status"].(string); status != "running" {
			err = fmt.Errorf("promotion requires credentials: %v", result["missing"])
			_ = config.Save(app.Root, original)
			app.Config = original
			return fail("prerequisites", err)
		}
		stage("migrate", "running", "Importing workplan state into hosted Postgres", map[string]any{"checksum": snapshot.Checksum})
		secrets, err := loadRailwaySecrets(app.Root)
		if err != nil {
			return fail("migrate", err)
		}
		base := strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/")
		var imported any
		if err = hostedRequestWithKey(ctx, http.MethodPost, base+"/api/v1/migrations/workplan", secrets.APIToken, "workplan:"+snapshot.Checksum, snapshot, &imported); err != nil {
			_ = config.Save(app.Root, original)
			app.Config = original
			return fail("migrate", err)
		}
		stage("verify", "running", "Verifying hosted dashboard, events, and knowledge", nil)
		var system any
		if err = hostedDashboardRequest(ctx, http.MethodGet, base+"/api/v1/system", secrets.APIToken, "", nil, &system); err != nil {
			_ = config.Save(app.Root, original)
			app.Config = original
			return fail("verify", err)
		}
		var claim struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		if err = hostedDashboardRequest(ctx, http.MethodPost, base+"/api/v1/access/owner-claims", secrets.APIToken, "owner-claim:"+operation.ID, map[string]any{}, &claim); err != nil {
			return fail("owner_claim", err)
		}
		claimURL := base + "/?owner_claim=" + claim.Data.Token
		result["owner_claim_url"] = claimURL
		result["workplan_checksum"] = snapshot.Checksum
		result["recovery_snapshot"] = recoveryPath
		if input.PreviewOrigin != "" {
			result["preview_origin"] = input.PreviewOrigin
		}
		stage("complete", "completed", "Hosted workspace verified and ready", map[string]any{"dashboard_url": claimURL})
		_ = app.DB.UpdateHostingOperation(ctx, operation.ID, "completed", "complete", result, nil)
		return nil
	}
}

func writePromotionRecoverySnapshot(path string, snapshot *state.WorkplanSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func hostedDashboardRequest(ctx context.Context, method, endpoint, token, key string, body, target any) error {
	raw := []byte(nil)
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.vessica.dashboard+json")
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("hosted dashboard request failed: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}
