package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/config"
)

func TestVessicaProjectCandidateRequiresHostedTopology(t *testing.T) {
	project := railwayProjectListItem{ID: "project_1", Name: "vessica"}
	project.Workspace.ID = "workspace_1"
	project.Environments.Edges = append(project.Environments.Edges, struct {
		Node struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"node"`
	}{})
	project.Environments.Edges[0].Node.ID = "environment_1"
	project.Environments.Edges[0].Node.Name = "production"
	for _, service := range []struct{ id, name string }{{"service_1", "control-plane"}, {"service_2", "knowledge-server"}} {
		edge := struct {
			Node struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"node"`
		}{}
		edge.Node.ID, edge.Node.Name = service.id, service.name
		project.Services.Edges = append(project.Services.Edges, edge)
	}

	candidate, ok := vessicaProjectCandidate(project)
	if !ok || candidate.ProjectID != "project_1" || candidate.EnvironmentID != "environment_1" || candidate.ServiceID != "service_1" {
		t.Fatalf("candidate=%#v ok=%v", candidate, ok)
	}
	project.Services.Edges = project.Services.Edges[:1]
	if _, ok := vessicaProjectCandidate(project); ok {
		t.Fatal("control plane without a knowledge service must not be treated as an installation")
	}
}

func TestReadRailwayVariablesKeepsValuesInProcess(t *testing.T) {
	bin := t.TempDir()
	path := filepath.Join(bin, "railway")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' '{\"VES_CONTROL_PLANE_API_TOKEN\":\"service-secret\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RAILWAY_TOKEN", "test")
	cfg := config.Config{Hosted: config.HostedConfig{ProjectID: "project", EnvironmentID: "environment"}}
	variables, err := readRailwayVariables(context.Background(), cfg, "service")
	if err != nil || variables["VES_CONTROL_PLANE_API_TOKEN"] != "service-secret" {
		t.Fatalf("variables=%v err=%v", variables, err)
	}
}
