package controlplane

import (
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/tracker"
)

func resolveLinearRepository(issue *tracker.LinearIssue, repositories []state.Repository) (*state.Repository, error) {
	if len(repositories) == 1 {
		return &repositories[0], nil
	}
	var matches []state.Repository
	for i := range repositories {
		aliases := []string{
			"vessica:repo:" + repositories[i].ID,
			"vessica:repo:" + repositories[i].DisplayName,
			"vessica-repo:" + repositories[i].DisplayName,
		}
		matched := false
		for _, label := range issue.Labels.Nodes {
			for _, alias := range aliases {
				if strings.EqualFold(strings.TrimSpace(label.Name), alias) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			matches = append(matches, repositories[i])
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	return nil, fmt.Errorf("ambiguous_repository_route: Linear issue %s must have exactly one Vessica repository label (vessica:repo:<repository-id-or-name>)", issue.Identifier)
}
