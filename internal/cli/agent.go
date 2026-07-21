package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	generalagent "github.com/vessica-labs/vessica-cli/internal/agent"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/output"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func newAgentCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Build, manage, and invoke persistent cloud agents"}
	cmd.AddCommand(newAgentCreateCmd(app), newAgentListCmd(app), newAgentViewCmd(app), newAgentUpdateCmd(app))
	cmd.AddCommand(newAgentStateCmd(app, "pause"), newAgentStateCmd(app, "resume"), newAgentStateCmd(app, "archive"))
	cmd.AddCommand(newAgentDraftCmd(app), newAgentBudgetCmd(app), newAgentHeartbeatCmd(app), newAgentRegistryCmd(app), newAgentRunCmd(app))
	return cmd
}

func prepareAgentHosted(cmd *cobra.Command, app *App) error {
	if err := app.loadWorkspace(cmd.Context()); err != nil {
		return err
	}
	if app.Config.Hosted.ControlPlaneURL == "" {
		return app.Printer.Fail("hosted_required", "general agents run only in a hosted Vessica workspace", "run ves up and attach this workspace")
	}
	return nil
}
func agentEndpoint(app *App, path string) string {
	return strings.TrimRight(app.Config.Hosted.ControlPlaneURL, "/") + path
}
func agentSecret(app *App) (string, error) {
	v, err := loadRailwaySecrets(app.Root)
	if err != nil {
		return "", err
	}
	return v.APIToken, nil
}
func agentMutationKey(app *App, prefix string) string {
	if app.Flags.IdempotencyKey != "" {
		return app.Flags.IdempotencyKey
	}
	return prefix + "-" + id.New("req")
}

func newAgentCreateCmd(app *App) *cobra.Command {
	var description, file string
	var review bool
	cmd := &cobra.Command{Use: "create", Short: "Create an agent from a description or definition file", RunE: func(cmd *cobra.Command, _ []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		if file != "" {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var definition generalagent.Definition
			if err = json.Unmarshal(raw, &definition); err != nil {
				return fmt.Errorf("parse agent definition: %w", err)
			}
			definition.Defaults(time.Now().Location().String())
			var result state.Agent
			if err = hostedRequestWithKey(cmd.Context(), http.MethodPost, agentEndpoint(app, "/api/v1/agents"), token, agentMutationKey(app, "agent-create"), definition, &result); err != nil {
				return err
			}
			return app.Printer.Success(result)
		}
		if strings.TrimSpace(description) == "" {
			return fmt.Errorf("--description or --file is required")
		}
		return submitAgentBuild(cmd.Context(), app, token, map[string]any{"description": description, "review": review, "timezone": time.Now().Location().String()}, review)
	}}
	cmd.Flags().StringVar(&description, "description", "", "natural-language agent description")
	cmd.Flags().StringVar(&file, "file", "", "vessica.agent/v1 JSON file")
	cmd.Flags().BoolVar(&review, "review", false, "retain a draft for review before activation")
	return cmd
}

func submitAgentBuild(ctx context.Context, app *App, token string, body map[string]any, review bool) error {
	var operation state.AgentBuildOperation
	if err := hostedRequestWithKey(ctx, http.MethodPost, agentEndpoint(app, "/api/v1/agent-builds"), token, agentMutationKey(app, "agent-build"), body, &operation); err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		var result struct {
			Operation state.AgentBuildOperation `json:"operation"`
			Draft     *state.AgentDraft         `json:"draft"`
		}
		if err := hostedRequest(ctx, http.MethodGet, agentEndpoint(app, "/api/v1/agent-builds/"+url.PathEscape(operation.ID)), token, nil, &result); err != nil {
			return err
		}
		switch result.Operation.Status {
		case "completed":
			return app.Printer.Success(result)
		case "draft":
			if !review || app.Flags.JSON || !isTTY() {
				return app.Printer.Success(result)
			}
			raw, _ := json.MarshalIndent(result.Draft, "", "  ")
			app.Printer.Info("%s", raw)
			app.Printer.Info("Activate this draft? [y/N]")
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes") {
				var activated state.Agent
				err := hostedRequestWithKey(ctx, http.MethodPost, agentEndpoint(app, "/api/v1/agent-builds/"+url.PathEscape(result.Draft.ID)+"/activate"), token, agentMutationKey(app, "draft-approve"), map[string]string{"draft_id": result.Draft.ID}, &activated)
				if err != nil {
					return err
				}
				return app.Printer.Success(activated)
			}
			return app.Printer.Success(result)
		case "failed":
			return fmt.Errorf("agent build failed: %s", result.Operation.Error)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("agent build did not finish within 10 minutes")
}

func newAgentListCmd(app *App) *cobra.Command {
	return &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, _ []string) error {
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
		return app.Printer.Success(result.Agents)
	}}
}
func newAgentViewCmd(app *App) *cobra.Command {
	return &cobra.Command{Use: "view <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result map[string]any
		if err = hostedRequest(cmd.Context(), http.MethodGet, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(args[0])), token, nil, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}}
}

func newAgentUpdateCmd(app *App) *cobra.Command {
	var description, file string
	var review bool
	cmd := &cobra.Command{Use: "update <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		if file != "" {
			raw, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var d generalagent.Definition
			if err = json.Unmarshal(raw, &d); err != nil {
				return err
			}
			var result any
			if err = hostedRequestWithKey(cmd.Context(), http.MethodPatch, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(args[0])), token, agentMutationKey(app, "agent-update"), d, &result); err != nil {
				return err
			}
			return app.Printer.Success(result)
		}
		if description == "" {
			return fmt.Errorf("--description or --file is required")
		}
		a, err := getHostedAgent(cmd.Context(), app, token, args[0])
		if err != nil {
			return err
		}
		return submitAgentBuild(cmd.Context(), app, token, map[string]any{"description": description, "agent_id": a.ID, "review": review, "timezone": time.Now().Location().String()}, review)
	}}
	cmd.Flags().StringVar(&description, "description", "", "natural-language edit")
	cmd.Flags().StringVar(&file, "file", "", "replacement definition JSON")
	cmd.Flags().BoolVar(&review, "review", false, "retain a draft before activation")
	return cmd
}
func getHostedAgent(ctx context.Context, app *App, token, ref string) (*state.Agent, error) {
	var result struct {
		Agent state.Agent `json:"agent"`
	}
	if err := hostedRequest(ctx, http.MethodGet, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(ref)), token, nil, &result); err != nil {
		return nil, err
	}
	return &result.Agent, nil
}

func newAgentStateCmd(app *App, action string) *cobra.Command {
	return &cobra.Command{Use: action + " <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result state.Agent
		if err = hostedRequestWithKey(cmd.Context(), http.MethodPost, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(args[0])+"/"+action), token, agentMutationKey(app, "agent-"+action), nil, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}}
}

func newAgentDraftCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "draft"}
	cmd.AddCommand(&cobra.Command{Use: "view <draft-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result map[string]any
		if err = hostedRequest(cmd.Context(), http.MethodGet, agentEndpoint(app, "/api/v1/agent-builds/"+url.PathEscape(args[0])), token, nil, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}})
	cmd.AddCommand(&cobra.Command{Use: "approve <draft-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		if err := app.requireYes("activate the agent draft"); err != nil {
			return err
		}
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result state.Agent
		if err = hostedRequestWithKey(cmd.Context(), http.MethodPost, agentEndpoint(app, "/api/v1/agent-builds/"+url.PathEscape(args[0])+"/activate"), token, agentMutationKey(app, "draft-approve"), map[string]string{"draft_id": args[0]}, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}})
	return cmd
}

func newAgentBudgetCmd(app *App) *cobra.Command {
	var daily, timezone string
	set := &cobra.Command{Use: "set <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		if timezone == "" {
			timezone = time.Now().Location().String()
		}
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result map[string]any
		if err = hostedRequestWithKey(cmd.Context(), http.MethodPut, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(args[0])+"/budget"), token, agentMutationKey(app, "agent-budget"), map[string]string{"daily_usd": daily, "timezone": timezone}, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}}
	set.Flags().StringVar(&daily, "daily-usd", "", "daily token-cost limit in USD")
	set.Flags().StringVar(&timezone, "timezone", "", "IANA timezone (defaults to client timezone)")
	_ = set.MarkFlagRequired("daily-usd")
	cmd := &cobra.Command{Use: "budget"}
	cmd.AddCommand(set)
	return cmd
}

func newAgentHeartbeatCmd(app *App) *cobra.Command {
	var expression, timezone string
	set := &cobra.Command{Use: "set <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		if timezone == "" {
			timezone = time.Now().Location().String()
		}
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result any
		if err = hostedRequestWithKey(cmd.Context(), http.MethodPut, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(args[0])+"/heartbeat"), token, agentMutationKey(app, "heartbeat-set"), map[string]any{"enabled": true, "cron": expression, "timezone": timezone}, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}}
	set.Flags().StringVar(&expression, "cron", "", "five-field cron expression")
	set.Flags().StringVar(&timezone, "timezone", "", "IANA timezone")
	_ = set.MarkFlagRequired("cron")
	disable := &cobra.Command{Use: "disable <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var result any
		if err = hostedRequestWithKey(cmd.Context(), http.MethodDelete, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(args[0])+"/heartbeat"), token, agentMutationKey(app, "heartbeat-disable"), nil, &result); err != nil {
			return err
		}
		return app.Printer.Success(result)
	}}
	cmd := &cobra.Command{Use: "heartbeat"}
	cmd.AddCommand(set, disable)
	return cmd
}
func newAgentRunCmd(app *App) *cobra.Command {
	var prompt, streamMode string
	cmd := &cobra.Command{Use: "run <name-or-id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := prepareAgentHosted(cmd, app); err != nil {
			return err
		}
		defer app.closeDB()
		if prompt == "" {
			return fmt.Errorf("--prompt is required")
		}
		token, err := agentSecret(app)
		if err != nil {
			return err
		}
		var run state.AgentRun
		body := map[string]string{"prompt": prompt, "repository_id": app.Config.Attachment.RepositoryID}
		if err = hostedRequestWithKey(cmd.Context(), http.MethodPost, agentEndpoint(app, "/api/v1/agents/"+url.PathEscape(args[0])+"/runs"), token, agentMutationKey(app, "agent-run"), body, &run); err != nil {
			return err
		}
		if streamMode == "" || streamMode == "off" {
			return app.Printer.Success(run)
		}
		if streamMode != "jsonl" {
			return fmt.Errorf("--stream must be jsonl or off")
		}
		return watchAgentRun(cmd.Context(), app, token, run.ID, 0, true)
	}}
	cmd.Flags().StringVar(&prompt, "prompt", "", "input for the agent")
	cmd.Flags().StringVar(&streamMode, "stream", "off", "stream format: jsonl or off")
	return cmd
}
func viewHostedAgentRun(ctx context.Context, app *App, token, runID string) error {
	var detail map[string]any
	if err := hostedRequest(ctx, http.MethodGet, agentEndpoint(app, "/api/v1/agent-runs/"+url.PathEscape(runID)), token, nil, &detail); err != nil {
		return err
	}
	return app.Printer.Success(detail)
}
func viewHostedAgentRunFromCLI(ctx context.Context, app *App, runID string) error {
	token, err := agentSecret(app)
	if err != nil {
		return err
	}
	return viewHostedAgentRun(ctx, app, token, runID)
}
func cancelHostedAgentRun(ctx context.Context, app *App, runID string) error {
	token, err := agentSecret(app)
	if err != nil {
		return err
	}
	var result map[string]any
	if err = hostedRequestWithKey(ctx, http.MethodPost, agentEndpoint(app, "/api/v1/agent-runs/"+url.PathEscape(runID)+"/cancel"), token, agentMutationKey(app, "agent-run-cancel"), nil, &result); err != nil {
		return err
	}
	return app.Printer.Success(result)
}
func listHostedAgentRunEvents(ctx context.Context, app *App, token, runID string, jsonl bool) error {
	var result struct {
		Events []state.AgentRunEvent `json:"events"`
	}
	if err := hostedRequest(ctx, http.MethodGet, agentEndpoint(app, "/api/v1/agent-runs/"+url.PathEscape(runID)+"/events"), token, nil, &result); err != nil {
		return err
	}
	for _, e := range result.Events {
		if jsonl {
			if err := writeAgentEvent(e); err != nil {
				return err
			}
		} else {
			app.Printer.Info("%d %s %s", e.Seq, e.Type, e.PayloadJSON)
		}
	}
	return nil
}
func listHostedAgentRunEventsFromCLI(ctx context.Context, app *App, runID string, jsonl bool) error {
	token, err := agentSecret(app)
	if err != nil {
		return err
	}
	return listHostedAgentRunEvents(ctx, app, token, runID, jsonl)
}
func writeAgentEvent(e state.AgentRunEvent) error {
	var payload any
	_ = json.Unmarshal([]byte(e.PayloadJSON), &payload)
	record := streaming.ProtocolRecord{Schema: streaming.ProtocolSchema, Kind: "event", RunID: e.RunID, Seq: e.Seq, Timestamp: e.CreatedAt, Event: &streaming.ProtocolEvent{ID: e.ID, RunID: e.RunID, Seq: e.Seq, Type: e.Type, Payload: payload, CreatedAt: e.CreatedAt}}
	return output.PrintJSONL(os.Stdout, record)
}
func watchAgentRun(ctx context.Context, app *App, token, runID string, after int64, jsonl bool) error {
	for {
		var eventResult struct {
			Events []state.AgentRunEvent `json:"events"`
		}
		path := fmt.Sprintf("/api/v1/agent-runs/%s/events?after=%d", url.PathEscape(runID), after)
		if err := hostedRequest(ctx, http.MethodGet, agentEndpoint(app, path), token, nil, &eventResult); err != nil {
			return err
		}
		for _, e := range eventResult.Events {
			after = e.Seq
			var payload any
			_ = json.Unmarshal([]byte(e.PayloadJSON), &payload)
			record := streaming.ProtocolRecord{Schema: streaming.ProtocolSchema, Kind: "event", RunID: e.RunID, Seq: e.Seq, Timestamp: e.CreatedAt, Event: &streaming.ProtocolEvent{ID: e.ID, RunID: e.RunID, Seq: e.Seq, Type: e.Type, Payload: payload, CreatedAt: e.CreatedAt}}
			if jsonl {
				_ = output.PrintJSONL(os.Stdout, record)
			} else {
				app.Printer.Info("%d %s", e.Seq, e.Type)
			}
		}
		var detail struct {
			Run state.AgentRun `json:"run"`
		}
		if err := hostedRequest(ctx, http.MethodGet, agentEndpoint(app, "/api/v1/agent-runs/"+url.PathEscape(runID)), token, nil, &detail); err != nil {
			return err
		}
		if runTerminal(detail.Run.Status) {
			return streaming.WriteRecord(os.Stdout, streaming.ResultRecord(runID, detail.Run, map[bool]error{true: fmt.Errorf("%s", detail.Run.TerminalError), false: nil}[detail.Run.Status == "failed"]))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
