package controlplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func (s *Server) repositoryRemote(ctx context.Context, runRecord *state.Run) (string, error) {
	if s == nil || s.DB == nil || runRecord == nil {
		return "", fmt.Errorf("run repository is unavailable")
	}
	if runRecord.RepositoryID != "" {
		repository, err := s.DB.GetRepository(ctx, runRecord.RepositoryID)
		if err != nil {
			return "", fmt.Errorf("resolve run repository: %w", err)
		}
		if strings.TrimSpace(repository.Remote) != "" {
			return repository.Remote, nil
		}
	}
	if strings.TrimSpace(s.Config.Repo.Remote) == "" {
		return "", fmt.Errorf("run repository remote is unavailable")
	}
	return s.Config.Repo.Remote, nil
}
