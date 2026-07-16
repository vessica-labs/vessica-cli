package controlplane

import (
	"strings"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func TestResolveLinearRepository(t *testing.T) {
	repositories := []state.Repository{
		{ID: "repo_web", DisplayName: "web"},
		{ID: "repo_api", DisplayName: "api"},
	}
	t.Run("single repository needs no route label", func(t *testing.T) {
		issue := &tracker.LinearIssue{Identifier: "ENG-1"}
		got, err := resolveLinearRepository(issue, repositories[:1])
		if err != nil || got.ID != "repo_web" {
			t.Fatalf("repository=%#v err=%v", got, err)
		}
	})
	t.Run("repository id label selects an explicit route", func(t *testing.T) {
		issue := &tracker.LinearIssue{Identifier: "ENG-2"}
		issue.Labels.Nodes = []tracker.LinearLabel{{Name: "vessica:repo:repo_api"}}
		got, err := resolveLinearRepository(issue, repositories)
		if err != nil || got.ID != "repo_api" {
			t.Fatalf("repository=%#v err=%v", got, err)
		}
	})
	t.Run("ambiguous issue remains unclaimed", func(t *testing.T) {
		issue := &tracker.LinearIssue{Identifier: "ENG-3"}
		if _, err := resolveLinearRepository(issue, repositories); err == nil || !strings.Contains(err.Error(), "ambiguous_repository_route") {
			t.Fatalf("expected typed ambiguity, got %v", err)
		}
	})
}
