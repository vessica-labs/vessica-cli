package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/controlplane"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func newControlPlaneRailwaySessionCmd() *cobra.Command {
	var workspaceID string
	root := &cobra.Command{Use: "railway-session", Hidden: true}
	root.PersistentFlags().StringVar(&workspaceID, "workspace-id", "", "Railway workspace that owns the forwarding key")

	authorize := &cobra.Command{
		Use:   "authorize",
		Short: "Authorize the official Railway CLI inside the control plane",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, session, err := openHostedRailwaySession(cmd.Context(), workspaceID)
			if err != nil {
				return err
			}
			defer db.Close()
			_, _ = session.Restore(cmd.Context())
			if err := session.Authorize(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"ok": true, "authorized": true})
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Validate the persisted Railway CLI forwarding session",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, session, err := openHostedRailwaySession(cmd.Context(), workspaceID)
			if err != nil {
				return err
			}
			defer db.Close()
			restored, err := session.Restore(cmd.Context())
			if err != nil {
				return err
			}
			valid := false
			if restored || session.Ready() {
				valid = session.Validate(cmd.Context()) == nil
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"ok": true, "authorized": session.Ready(), "valid": valid})
		},
	}
	repairKey := &cobra.Command{
		Use:   "repair-key",
		Short: "Rotate and register the Railway forwarding key for the CLI session",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, db, session, err := openHostedRailwaySession(cmd.Context(), workspaceID)
			if err != nil {
				return err
			}
			defer db.Close()
			if _, err := session.Restore(cmd.Context()); err != nil {
				return err
			}
			if err := session.Validate(cmd.Context()); err != nil {
				return err
			}
			if err := session.RotateForwardingKey(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"ok": true, "repaired": true})
		},
	}
	smoke := &cobra.Command{
		Use:   "smoke",
		Short: "Verify Railway service to relay to sandbox forwarding",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, db, session, err := openHostedRailwaySession(cmd.Context(), workspaceID)
			if err != nil {
				return err
			}
			defer db.Close()
			if _, err := session.Restore(cmd.Context()); err != nil {
				return err
			}
			if err := session.Validate(cmd.Context()); err != nil {
				return err
			}
			if err := controlplane.RunRailwayForwardSmoke(cmd.Context(), railwayPath(), cfg.Hosted.ProjectID, cfg.Hosted.EnvironmentID, cfg.Hosted.WorkerCheckpoint, session); err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"ok": true, "forwarding": true, "path": "control-plane->railway-relay->sandbox"})
		},
	}
	root.AddCommand(authorize, status, repairKey, smoke)
	return root
}

func openHostedRailwaySession(ctx context.Context, workspaceID string) (config.Config, *state.DB, *controlplane.RailwayCLISession, error) {
	cfg := config.TeamDefaults()
	config.ApplyEnv(&cfg)
	if cfg.State.DBURL == "" {
		cfg.State.DBURL = os.Getenv("VES_CONTROL_DATABASE_URL")
	}
	if cfg.State.DBURL == "" {
		return cfg, nil, nil, fmt.Errorf("VES_CONTROL_DATABASE_URL is required")
	}
	db, err := state.OpenWithOptions("postgres-url", cfg.State.DBURL, "/var/lib/vessica", state.OpenOptions{})
	if err != nil {
		return cfg, nil, nil, err
	}
	manager, err := hostedCredentialManager(ctx, db)
	if err != nil {
		_ = db.Close()
		return cfg, nil, nil, err
	}
	if manager == nil {
		_ = db.Close()
		return cfg, nil, nil, fmt.Errorf("VES_CREDENTIAL_ENCRYPTION_KEY is required")
	}
	workspaceID = firstNonEmpty(workspaceID, os.Getenv("VES_RAILWAY_WORKSPACE_ID"), cfg.Hosted.WorkspaceID)
	session := controlplane.NewRailwayCLISession(manager, railwayPath(), filepath.Join("/var/lib/vessica", ".vessica", "railway-cli"), strings.TrimSpace(workspaceID))
	return cfg, db, session, nil
}
