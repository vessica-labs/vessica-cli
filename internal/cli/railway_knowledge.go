package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func ensureRailwayKnowledge(ctx context.Context, workDir string, app *App, cfg *config.Config, opts railwayUpOptions, token, adminToken, embeddingKey string) error {
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
	if cfg.Knowledge.PostgresServiceID == "" {
		before, err := listRailwayServices(ctx, *cfg)
		if err != nil {
			return err
		}
		if candidate, ok := recoverKnowledgePostgres(before, *cfg); ok {
			cfg.Knowledge.PostgresServiceID = candidate.ID
			cfg.Knowledge.PostgresServiceName = candidate.Name
		} else {
			if _, err := runRailway(ctx, workDir, nil, "add", "--database", "postgres", "--json"); err != nil {
				return err
			}
			after, err := listRailwayServices(ctx, *cfg)
			if err != nil {
				return err
			}
			candidate, err := newlyAddedRailwayService(before, after)
			if err != nil {
				return fmt.Errorf("identify knowledge Postgres service: %w", err)
			}
			cfg.Knowledge.PostgresServiceID = candidate.ID
			cfg.Knowledge.PostgresServiceName = candidate.Name
		}
	}
	if cfg.Knowledge.PostgresServiceName == "" {
		return fmt.Errorf("knowledge Postgres service name is unavailable")
	}
	if err := config.Save(app.Root, *cfg); err != nil {
		return err
	}
	if cfg.Knowledge.Endpoint == "" {
		raw, err := runRailway(ctx, workDir, nil, "domain", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "-p", "8080", "--json")
		if err != nil {
			return err
		}
		domain, err := objectString(raw, "domain")
		if err != nil {
			return err
		}
		cfg.Knowledge.Endpoint = "https://" + strings.TrimPrefix(strings.TrimPrefix(domain, "https://"), "http://")
	}
	ws, err := app.DB.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	if cfg.Knowledge.WorkspaceID == "" {
		cfg.Knowledge.WorkspaceID = ws.ID
	}
	variables := map[string]string{
		"DATABASE_URL":           "$" + "{{" + cfg.Knowledge.PostgresServiceName + ".DATABASE_URL}}",
		"KNOWLEDGE_API_TOKEN":    token,
		"KNOWLEDGE_EXPORT_TOKEN": adminToken,
		"KNOWLEDGE_WORKSPACE_ID": cfg.Knowledge.WorkspaceID,
		"EMBEDDING_API_KEY":      embeddingKey,
		"EMBEDDING_MODEL":        "text-embedding-3-small",
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
	if cfg.Knowledge.Mode != "hosted" {
		if err := promoteKnowledgeAuthority(ctx, app, cfg, token, adminToken); err != nil {
			return err
		}
	}
	return nil
}

func promoteKnowledgeAuthority(ctx context.Context, app *App, cfg *config.Config, token, adminToken string) error {
	lockPath := filepath.Join(app.Root, ".vessica", "state", "knowledge.promote.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("another knowledge promotion is active")
	}
	_ = lock.Close()
	defer os.Remove(lockPath)
	ws, err := app.DB.GetWorkspace(ctx)
	if err != nil {
		return err
	}
	local, err := knowledgegateway.OpenForPromotion(app.Root, app.Config, ws.ID)
	if err != nil {
		return err
	}
	snap, err := local.Export(ctx)
	_ = local.Close()
	if err != nil {
		return err
	}
	if err := auth.Login("knowledge", token, "Railway hosted knowledge"); err != nil {
		return err
	}
	if err := auth.Login("knowledge-export", adminToken, "Railway hosted knowledge export"); err != nil {
		return err
	}
	next := *cfg
	next.Knowledge.Mode = "hosted"
	remote, err := knowledgegateway.Open(app.Root, next, snap.WorkspaceID)
	if err != nil {
		return err
	}
	defer remote.Close()
	if err := remote.Import(ctx, snap); err != nil {
		return err
	}
	check, err := remote.Export(ctx)
	if err != nil {
		return err
	}
	if err := verifyKnowledgePromotion(snap, check); err != nil {
		return err
	}
	if _, err := remote.Context(ctx, ks.ContextRequest{Query: "workspace knowledge", ArtifactSelectors: []ks.ArtifactSelector{{Status: "active"}}, TokenBudget: 1000}); err != nil {
		return err
	}
	localPath := cfg.Knowledge.LocalPath
	if localPath == "" {
		localPath = filepath.Join(".vessica", "state", "knowledge.db")
	}
	if !filepath.IsAbs(localPath) {
		localPath = filepath.Join(app.Root, localPath)
	}
	backup := filepath.Join(app.Root, ".vessica", "state", "knowledge-"+snap.HighWatermark+".readonly.db")
	if err := copyReadOnly(localPath, backup); err != nil {
		return err
	}
	cfg.Knowledge.Mode = "hosted"
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
