package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

const railwayControlPlaneProjectName = "vessica-control-plane"

func createRailwayResources(ctx context.Context, workDir, root string, cfg *config.Config, opts railwayUpOptions) error {
	if cfg.Hosted.ProjectID == "" {
		args := []string{"init", "--name", railwayControlPlaneProjectName, "--json"}
		if workspace := firstNonEmpty(opts.WorkspaceName, opts.Workspace); workspace != "" {
			args = append(args, "--workspace", workspace)
		}
		raw, err := runRailway(ctx, workDir, nil, args...)
		if err != nil {
			return err
		}
		cfg.Hosted.ProjectID, err = objectID(raw)
		if err != nil {
			return err
		}
		cfg.Hosted.EnvironmentID = "production"
		if err := config.Save(root, *cfg); err != nil {
			return err
		}
	}
	if cfg.Hosted.EnvironmentID == "" {
		cfg.Hosted.EnvironmentID = "production"
	}
	if err := linkRailwayWorkDir(ctx, workDir, *cfg); err != nil {
		return err
	}
	if cfg.Hosted.ServiceID == "" || cfg.Hosted.PostgresServiceID == "" {
		services, err := listRailwayServices(ctx, *cfg)
		if err != nil {
			return err
		}
		for _, service := range services {
			name := strings.ToLower(service.Name)
			switch {
			case name == "control-plane":
				cfg.Hosted.ServiceID = service.ID
			case name == "postgres" || strings.HasPrefix(name, "postgres-"):
				if cfg.Hosted.PostgresServiceID != "" && cfg.Hosted.PostgresServiceID != service.ID {
					return multipleRailwayPostgresError()
				}
				cfg.Hosted.PostgresServiceID = service.ID
			}
		}
		if err := config.Save(root, *cfg); err != nil {
			return err
		}
	}
	if cfg.Hosted.ServiceID == "" {
		// Keep the service source-less until its database variables and migration
		// command are configured. Attaching the image here causes Railway to start
		// the control plane against an unmigrated database during first install.
		raw, err := runRailway(ctx, workDir, nil, "add", "--service", "control-plane", "--json")
		if err != nil {
			return err
		}
		cfg.Hosted.ServiceID, err = railwayCreatedServiceID(raw)
		if err != nil {
			return err
		}
		if err := config.Save(root, *cfg); err != nil {
			return err
		}
	}
	if cfg.Hosted.PostgresServiceID == "" {
		raw, err := runRailway(ctx, workDir, nil, "add", "--database", "postgres", "--json")
		if err != nil {
			return err
		}
		cfg.Hosted.PostgresServiceID, err = railwayCreatedServiceID(raw)
		if err != nil {
			return err
		}
		if err := config.Save(root, *cfg); err != nil {
			return err
		}
	}
	return nil
}

func reconcileRailwayResourceIDs(ctx context.Context, cfg *config.Config) error {
	raw, err := runRailway(ctx, "", nil, "status", "--project", cfg.Hosted.ProjectID, "--environment", firstNonEmpty(cfg.Hosted.EnvironmentID, "production"), "--json")
	if err != nil {
		return err
	}
	var project struct {
		WorkspaceID  string `json:"workspaceId"`
		Environments struct {
			Edges []struct {
				Node struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"environments"`
		Services struct {
			Edges []struct {
				Node struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"services"`
	}
	if err := json.Unmarshal(raw, &project); err != nil {
		return err
	}
	cfg.Hosted.WorkspaceID = project.WorkspaceID
	for _, edge := range project.Environments.Edges {
		if edge.Node.Name == "production" || edge.Node.ID == cfg.Hosted.EnvironmentID {
			cfg.Hosted.EnvironmentID = edge.Node.ID
			break
		}
	}
	for _, edge := range project.Services.Edges {
		name := strings.ToLower(edge.Node.Name)
		switch {
		case name == "control-plane":
			cfg.Hosted.ServiceID = edge.Node.ID
		case name == railwayPreviewEdgeServiceName:
			cfg.Hosted.PreviewServiceID = edge.Node.ID
		case name == "postgres" || strings.HasPrefix(name, "postgres-"):
			if cfg.Hosted.PostgresServiceID != "" && cfg.Hosted.PostgresServiceID != edge.Node.ID {
				return multipleRailwayPostgresError()
			}
			cfg.Hosted.PostgresServiceID = edge.Node.ID
		case name == "knowledge-server":
			cfg.Knowledge.ServiceID = edge.Node.ID
		}
	}
	if cfg.Hosted.EnvironmentID == "" || cfg.Hosted.ServiceID == "" || cfg.Hosted.PostgresServiceID == "" {
		return fmt.Errorf("Railway project is missing production, control-plane, or Postgres resources")
	}
	return nil
}

func multipleRailwayPostgresError() error {
	return fmt.Errorf("Railway project contains multiple Postgres services; recreate the installation from an empty project")
}
