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
	"github.com/vessica-labs/vessica-cli/internal/id"
)

func ensureRailwayKnowledge(ctx context.Context, workDir string, app *App, cfg *config.Config, opts railwayUpOptions, knowledgeDatabaseURL, token, adminToken, embeddingKey string) error {
	image := opts.KnowledgeImage
	if image == "" && opts.KnowledgeSource == "" {
		image = "ghcr.io/vessica-labs/vessica-knowledge-server:v" + knowledgeServerVersion
		var err error
		image, err = resolveGHCRDigest(ctx, image)
		if err != nil {
			return fmt.Errorf("resolve knowledge-server release image: %w", err)
		}
	}
	if cfg.Knowledge.ServiceID == "" {
		args := []string{"add", "--service", "knowledge-server", "--json"}
		if image != "" {
			args = []string{"add", "--image", image, "--service", "knowledge-server", "--json"}
		}
		raw, err := runRailway(ctx, workDir, nil, args...)
		if err != nil {
			return err
		}
		cfg.Knowledge.ServiceID, err = objectID(raw)
		if err != nil {
			return err
		}
		if err := config.Save(app.Root, *cfg); err != nil {
			return err
		}
	}
	if err := config.Save(app.Root, *cfg); err != nil {
		return err
	}
	if cfg.Knowledge.Endpoint == "" {
		domain, err := ensureRailwayServiceDomain(ctx, workDir, *cfg, cfg.Knowledge.ServiceID)
		if err != nil {
			return err
		}
		cfg.Knowledge.Endpoint = "https://" + strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	}
	if cfg.Knowledge.WorkspaceID == "" {
		cfg.Knowledge.WorkspaceID = id.New(id.Workspace)
	}
	variables := map[string]string{
		"VES_KNOWLEDGE_DATABASE_URL": knowledgeDatabaseURL,
		"KNOWLEDGE_API_TOKEN":        token,
		"KNOWLEDGE_EXPORT_TOKEN":     adminToken,
		"KNOWLEDGE_WORKSPACE_ID":     cfg.Knowledge.WorkspaceID,
		"EMBEDDING_API_KEY":          embeddingKey,
		"EMBEDDING_MODEL":            "text-embedding-3-small",
	}
	for key, value := range variables {
		if err := setRailwayVariableForService(ctx, *cfg, cfg.Knowledge.ServiceID, key, value); err != nil {
			return err
		}
	}
	previous := ""
	if latest, err := latestRailwayDeploymentForService(ctx, *cfg, cfg.Knowledge.ServiceID); err == nil {
		previous = latest.ID
	}
	if opts.KnowledgeSource != "" {
		if _, err := runRailway(ctx, opts.KnowledgeSource, nil, "up", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--detach", "--json", "--message", "Vessica knowledge dev"); err != nil {
			return err
		}
	} else {
		if _, err := runRailway(ctx, "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--yes"); err != nil {
			return err
		}
	}
	if err := waitForRailwayDeploymentForService(ctx, *cfg, cfg.Knowledge.ServiceID, previous, 8*time.Minute); err != nil {
		return err
	}
	if err := waitForHostedHealth(ctx, strings.TrimRight(cfg.Knowledge.Endpoint, "/")+"/readyz", 8*time.Minute); err != nil {
		return err
	}
	cfg.Knowledge.Version = knowledgeServerVersion
	cfg.Knowledge.Image = image
	cfg.Knowledge.Mode = "hosted"
	if err := syncHostedKnowledgeCredentials(*cfg, railwaySecrets{KnowledgeToken: token, KnowledgeAdminToken: adminToken}); err != nil {
		return err
	}
	return nil
}

func syncHostedKnowledgeCredentials(cfg config.Config, secrets railwaySecrets) error {
	if strings.TrimSpace(secrets.KnowledgeToken) == "" || strings.TrimSpace(secrets.KnowledgeAdminToken) == "" {
		return fmt.Errorf("hosted knowledge credentials are incomplete")
	}
	account := firstNonEmpty(cfg.Knowledge.Endpoint, cfg.Knowledge.WorkspaceID, "hosted")
	if err := auth.Login("knowledge", secrets.KnowledgeToken, account); err != nil {
		return fmt.Errorf("store hosted knowledge credential: %w", err)
	}
	if err := auth.Login("knowledge-export", secrets.KnowledgeAdminToken, account); err != nil {
		return fmt.Errorf("store hosted knowledge export credential: %w", err)
	}
	return nil
}

func resolveGHCRDigest(ctx context.Context, image string) (string, error) {
	if strings.Contains(image, "@sha256:") {
		return image, nil
	}
	const prefix = "ghcr.io/"
	if !strings.HasPrefix(image, prefix) {
		return "", fmt.Errorf("production image must be ghcr.io or already digest-pinned")
	}
	nameTag := strings.TrimPrefix(image, prefix)
	name, tag, ok := strings.Cut(nameTag, ":")
	if !ok {
		tag = "latest"
	}
	tokenURL := "https://ghcr.io/token?scope=repository:" + name + ":pull"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var authResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", err
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodHead, "https://ghcr.io/v2/"+name+"/manifests/"+tag, nil)
	req.Header.Set("Authorization", "Bearer "+authResp.Token)
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("registry returned HTTP %d", resp.StatusCode)
	}
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("registry response omitted image digest")
	}
	return prefix + name + "@" + digest, nil
}
