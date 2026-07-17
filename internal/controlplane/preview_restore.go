package controlplane

import "context"

// RestoreHostedPreviews reconnects retained preview tunnels and refreshes the
// external completion projection with the same persisted public URL.
func (s *Server) RestoreHostedPreviews(ctx context.Context) {
	restorer, ok := s.Launcher.(interface{ RestorePreviews(context.Context) })
	if !ok {
		return
	}
	restorer.RestorePreviews(ctx)
	runs, err := s.DB.ListRuns(ctx)
	if err != nil {
		return
	}
	for i := range runs {
		if runs[i].Status == "completed" && s.projectedPreviewURL(&runs[i]) != "" {
			_ = s.SyncRunToLinear(ctx, runs[i].ID)
		}
	}
}
