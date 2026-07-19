package cli

import (
	"context"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
)

func newKnowledgeServerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "server", Short: "Operate the hosted knowledge service release"}
	var image string
	upgrade := &cobra.Command{Use: "upgrade", Short: "Upgrade only the hosted knowledge service image", RunE: func(c *cobra.Command, args []string) error {
		if err := app.loadWorkspaceWithoutGC(c.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		cfg := app.Config
		if cfg.Knowledge.Mode != "hosted" || cfg.Knowledge.ServiceID == "" {
			return app.Printer.Fail("hosted_knowledge_required", "a hosted knowledge service is required", "run ves up first")
		}
		resolved, err := resolveGHCRDigest(c.Context(), image)
		if err != nil {
			return err
		}
		impact := map[string]any{
			"service_id":              cfg.Knowledge.ServiceID,
			"current_image":           cfg.Knowledge.Image,
			"target_image":            resolved,
			"embeddings_unchanged":    true,
			"reranking_unchanged":     true,
			"control_plane_unchanged": true,
		}
		if app.Flags.DryRun {
			return app.dryRun("knowledge.server.upgrade", impact)
		}
		if err := app.requireYes("upgrade only the hosted knowledge service to " + resolved); err != nil {
			return err
		}
		if err := upgradeKnowledgeServer(c.Context(), cfg, resolved); err != nil {
			return err
		}
		cfg.Knowledge.Image = resolved
		cfg.Knowledge.Version = knowledgeServerVersion
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
		return app.Printer.Success(map[string]any{"upgraded": true, "version": cfg.Knowledge.Version, "image": resolved, "ready": true})
	}}
	upgrade.Flags().StringVar(&image, "image", "ghcr.io/vessica-labs/vessica-knowledge-server:v"+knowledgeServerVersion, "published knowledge-server image")
	cmd.AddCommand(upgrade)
	return cmd
}

func upgradeKnowledgeServer(ctx context.Context, cfg config.Config, image string) error {
	previous := ""
	if latest, err := latestRailwayDeploymentForService(ctx, cfg, cfg.Knowledge.ServiceID); err == nil {
		previous = latest.ID
	}
	if _, err := runRailway(ctx, "", nil,
		"service", "source", "connect",
		"--project", cfg.Hosted.ProjectID,
		"--environment", cfg.Hosted.EnvironmentID,
		"--service", cfg.Knowledge.ServiceID,
		"--image", image,
		"--json",
	); err != nil {
		return err
	}
	if err := waitForRailwayDeploymentForService(ctx, cfg, cfg.Knowledge.ServiceID, previous, 8*time.Minute); err != nil {
		return err
	}
	return waitForHostedHealth(ctx, strings.TrimRight(cfg.Knowledge.Endpoint, "/")+"/readyz", 8*time.Minute)
}
