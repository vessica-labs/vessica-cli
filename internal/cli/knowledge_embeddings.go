package cli

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

func newKnowledgeEmbeddingsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "embeddings", Short: "Manage optional user-funded semantic retrieval"}
	cmd.AddCommand(&cobra.Command{Use: "status", RunE: func(c *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Knowledge.Mode != "hosted" {
			return app.Printer.Fail("hosted_knowledge_required", "embeddings are configured on hosted knowledge", "run ves up first")
		}
		secrets, err := loadRailwaySecrets(app.Root)
		if err != nil {
			return err
		}
		var status any
		if err := hostedRequest(c.Context(), http.MethodGet, strings.TrimRight(app.Config.Knowledge.Endpoint, "/")+"/v1/status", secrets.KnowledgeToken, nil, &status); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"status": status})
	}})
	var provider, keyEnv, model, baseURL string
	enable := &cobra.Command{Use: "enable", RunE: func(c *cobra.Command, args []string) error {
		if keyEnv == "" {
			return app.Printer.Fail("embedding_key_reference_required", "--api-key-env is required", "export the provider key locally and pass only its environment-variable name")
		}
		key := os.Getenv(keyEnv)
		if key == "" {
			return app.Printer.Fail("embedding_key_missing", fmt.Sprintf("environment variable %s is empty", keyEnv), "")
		}
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Knowledge.Mode != "hosted" {
			return app.Printer.Fail("hosted_knowledge_required", "run ves up before enabling embeddings", "")
		}
		mode := "missing"
		if app.Config.Knowledge.EmbeddingModel != "" && (app.Config.Knowledge.EmbeddingModel != model || app.Config.Knowledge.EmbeddingProvider != provider) {
			mode = "all"
		}
		if app.Flags.DryRun {
			return app.dryRun("knowledge.embeddings.enable", map[string]any{"provider": provider, "model": model, "key_env": keyEnv, "backfill_mode": mode})
		}
		if err := app.requireYes("store the embeddings key in Railway and redeploy hosted knowledge"); err != nil {
			return err
		}
		cfg := app.Config
		for name, value := range map[string]string{"EMBEDDING_API_KEY": key, "EMBEDDING_MODEL": model, "EMBEDDING_BASE_URL": baseURL} {
			if err := setRailwayVariableForService(c.Context(), cfg, cfg.Knowledge.ServiceID, name, value); err != nil {
				return err
			}
		}
		previous := ""
		if latest, err := latestRailwayDeploymentForService(c.Context(), cfg, cfg.Knowledge.ServiceID); err == nil {
			previous = latest.ID
		}
		if _, err := runRailway(c.Context(), "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--yes"); err != nil {
			return err
		}
		if err := waitForRailwayDeploymentForService(c.Context(), cfg, cfg.Knowledge.ServiceID, previous, 8*time.Minute); err != nil {
			return err
		}
		secrets, err := loadRailwaySecrets(app.Root)
		if err != nil {
			return err
		}
		var backfill any
		if err := hostedRequest(c.Context(), http.MethodPost, strings.TrimRight(cfg.Knowledge.Endpoint, "/")+"/admin/v1/embeddings/backfill", secrets.KnowledgeAdminToken, map[string]any{"workspace_id": cfg.Knowledge.WorkspaceID, "mode": mode}, &backfill); err != nil {
			return err
		}
		cfg.Knowledge.EmbeddingProvider = provider
		cfg.Knowledge.EmbeddingModel = model
		if err := saveHostedClientConfig(cfg, secrets); err != nil {
			return err
		}
		if err := config.Save(app.Root, cfg); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"retrieval_mode": "semantic_hybrid", "embedding_state": "catching_up", "provider": provider, "model": model, "backfill": backfill})
	}}
	enable.Flags().StringVar(&provider, "provider", "openai", "embedding provider")
	enable.Flags().StringVar(&keyEnv, "api-key-env", "", "environment variable containing the provider key")
	enable.Flags().StringVar(&model, "model", "text-embedding-3-small", "embedding model")
	enable.Flags().StringVar(&baseURL, "base-url", "", "optional OpenAI-compatible embeddings endpoint")
	cmd.AddCommand(enable)
	disable := &cobra.Command{Use: "disable", RunE: func(c *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Flags.DryRun {
			return app.dryRun("knowledge.embeddings.disable", map[string]any{"retrieval_mode": "lexical", "vectors_retained": true})
		}
		if err := app.requireYes("remove the embeddings key from Railway and return to lexical retrieval"); err != nil {
			return err
		}
		cfg := app.Config
		if err := setRailwayVariableForService(c.Context(), cfg, cfg.Knowledge.ServiceID, "EMBEDDING_API_KEY", ""); err != nil {
			return err
		}
		previous := ""
		if latest, err := latestRailwayDeploymentForService(c.Context(), cfg, cfg.Knowledge.ServiceID); err == nil {
			previous = latest.ID
		}
		if _, err := runRailway(c.Context(), "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--yes"); err != nil {
			return err
		}
		if err := waitForRailwayDeploymentForService(c.Context(), cfg, cfg.Knowledge.ServiceID, previous, 8*time.Minute); err != nil {
			return err
		}
		cfg.Knowledge.EmbeddingProvider = ""
		cfg.Knowledge.EmbeddingModel = ""
		secrets, err := loadRailwaySecrets(app.Root)
		if err != nil {
			return err
		}
		if err := saveHostedClientConfig(cfg, secrets); err != nil {
			return err
		}
		if err := config.Save(app.Root, cfg); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"retrieval_mode": "lexical", "embedding_state": "not_configured", "vectors_retained": true})
	}}
	cmd.AddCommand(disable)
	return cmd
}
