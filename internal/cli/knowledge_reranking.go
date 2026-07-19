package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

func newKnowledgeRerankingCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "reranking", Short: "Manage optional conditional memory reranking"}
	cmd.AddCommand(&cobra.Command{Use: "status", RunE: func(c *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Knowledge.Mode != "hosted" {
			return app.Printer.Fail("hosted_knowledge_required", "reranking is configured on hosted knowledge", "run ves up first")
		}
		g, err := app.openKnowledge(c.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		status, err := g.Status(c.Context())
		if err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{
			"enabled": status.RerankEnabled,
			"model":   status.RerankModel,
		})
	}})

	var provider, keyEnv, model, baseURL string
	enable := &cobra.Command{Use: "enable", RunE: func(c *cobra.Command, args []string) error {
		if provider != "openai" {
			return app.Printer.Fail("unsupported_rerank_provider", "only the openai reranking provider is supported", "use --provider openai")
		}
		if keyEnv == "" {
			return app.Printer.Fail("rerank_key_reference_required", "--api-key-env is required", "export the provider key locally and pass only its environment-variable name")
		}
		key := os.Getenv(keyEnv)
		if key == "" {
			return app.Printer.Fail("rerank_key_missing", fmt.Sprintf("environment variable %s is empty", keyEnv), "")
		}
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Knowledge.Mode != "hosted" {
			return app.Printer.Fail("hosted_knowledge_required", "run ves up before enabling reranking", "")
		}
		impact := map[string]any{
			"provider":                     provider,
			"model":                        model,
			"key_env":                      keyEnv,
			"conditional":                  true,
			"candidate_limit":              12,
			"timeout_seconds":              2.5,
			"sends_readable_memory_text":   true,
			"fallback":                     "deterministic_hybrid_order",
			"separate_disablement_control": true,
		}
		if app.Flags.DryRun {
			return app.dryRun("knowledge.reranking.enable", impact)
		}
		if err := app.requireYes("send up to 12 authorized readable memory candidates to OpenAI on ambiguous queries, store a separate reranking key in Railway, and redeploy hosted knowledge"); err != nil {
			return err
		}
		cfg := app.Config
		for name, value := range map[string]string{
			"RERANK_API_KEY":  key,
			"RERANK_MODEL":    model,
			"RERANK_BASE_URL": baseURL,
			"RERANK_ENABLED":  "true",
		} {
			if err := setRailwayVariableForService(c.Context(), cfg, cfg.Knowledge.ServiceID, name, value); err != nil {
				return err
			}
		}
		if err := redeployKnowledgeService(c, cfg); err != nil {
			return err
		}
		cfg.Knowledge.RerankProvider = provider
		cfg.Knowledge.RerankModel = model
		cfg.Knowledge.RerankEnabled = true
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
		return app.Printer.Success(map[string]any{"enabled": true, "provider": provider, "model": model, "policy": "conditional"})
	}}
	enable.Flags().StringVar(&provider, "provider", "openai", "reranking provider")
	enable.Flags().StringVar(&keyEnv, "api-key-env", "", "environment variable containing the provider key")
	enable.Flags().StringVar(&model, "model", "gpt-5.6-luna", "reranking model")
	enable.Flags().StringVar(&baseURL, "base-url", "", "optional OpenAI-compatible Responses API endpoint")
	cmd.AddCommand(enable)

	disable := &cobra.Command{Use: "disable", RunE: func(c *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Knowledge.Mode != "hosted" {
			return app.Printer.Fail("hosted_knowledge_required", "reranking is configured on hosted knowledge", "run ves up first")
		}
		if app.Flags.DryRun {
			return app.dryRun("knowledge.reranking.disable", map[string]any{"enabled": false, "deterministic_retrieval_retained": true})
		}
		if err := app.requireYes("remove the separate reranking key from Railway and redeploy hosted knowledge"); err != nil {
			return err
		}
		cfg := app.Config
		for name, value := range map[string]string{"RERANK_API_KEY": "", "RERANK_ENABLED": "false"} {
			if err := setRailwayVariableForService(c.Context(), cfg, cfg.Knowledge.ServiceID, name, value); err != nil {
				return err
			}
		}
		if err := redeployKnowledgeService(c, cfg); err != nil {
			return err
		}
		cfg.Knowledge.RerankProvider = ""
		cfg.Knowledge.RerankModel = ""
		cfg.Knowledge.RerankEnabled = false
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
		return app.Printer.Success(map[string]any{"enabled": false, "retrieval_mode": "deterministic_hybrid"})
	}}
	cmd.AddCommand(disable)
	return cmd
}

func redeployKnowledgeService(c *cobra.Command, cfg config.Config) error {
	previous := ""
	if latest, err := latestRailwayDeploymentForService(c.Context(), cfg, cfg.Knowledge.ServiceID); err == nil {
		previous = latest.ID
	}
	if _, err := runRailway(c.Context(), "", nil, "redeploy", "--project", cfg.Hosted.ProjectID, "-e", cfg.Hosted.EnvironmentID, "-s", cfg.Knowledge.ServiceID, "--yes"); err != nil {
		return err
	}
	return waitForRailwayDeploymentForService(c.Context(), cfg, cfg.Knowledge.ServiceID, previous, 8*time.Minute)
}
