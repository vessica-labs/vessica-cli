package cli

import (
	"net/http"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

func newAgentRegistryCmd(app *App) *cobra.Command {
	return &cobra.Command{Use: "registry", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result struct {
			Agents []state.Agent `json:"agents"`
		}
		if err = hostedRequest(cmd.Context(), http.MethodGet, agentEndpoint(app, "/api/v1/agents"), token, nil, &result); err != nil {
			return err
		}
		active := make([]state.Agent, 0, len(result.Agents))
		for _, agent := range result.Agents {
			if agent.State == "active" {
				active = append(active, agent)
			}
		}
		return app.Printer.Success(active)
	}}
}
