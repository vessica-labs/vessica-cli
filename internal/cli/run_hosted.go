package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/output"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func (a *App) startHostedEpicRun(ctx context.Context, hostedEpicID string) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	key := a.Flags.IdempotencyKey
	if key == "" {
		key = "run-" + hostedEpicID
	}
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/epics/" + hostedEpicID + "/runs"
	var result map[string]any
	if err := hostedRequestWithKey(ctx, http.MethodPost, endpoint, secrets.APIToken, key, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) listHostedRuns(ctx context.Context) ([]*state.Run, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var runs []*state.Run
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs"
	if a.Config.Attachment.RepositoryID != "" {
		endpoint += "?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	}
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func (a *App) getHostedRun(ctx context.Context, runID string) (*state.Run, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var runRecord state.Run
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs/" + runID
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &runRecord); err != nil {
		return nil, err
	}
	return &runRecord, nil
}

func (a *App) listHostedRunEvents(ctx context.Context, runID string, after int64) ([]state.Event, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var events []state.Event
	endpoint := fmt.Sprintf("%s/api/v1/runs/%s/events?after=%d", strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/"), runID, after)
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (a *App) watchHostedRun(ctx context.Context, runID string, after int64, jsonl bool) error {
	started := map[string]bool{}
	for {
		events, err := a.listHostedRunEvents(ctx, runID, after)
		if err != nil {
			return err
		}
		for _, event := range events {
			after = event.Seq
			if jsonl {
				if err := streaming.WriteEvent(os.Stdout, &event); err != nil {
					return err
				}
			} else if a.Flags.JSON {
				_ = output.PrintJSONL(os.Stdout, event)
			} else if line := formatEventLine(event, false, started); line != "" {
				a.Printer.Info("%s", line)
			}
		}
		runRecord, err := a.getHostedRun(ctx, runID)
		if err != nil {
			return err
		}
		if runTerminal(runRecord.Status) {
			if jsonl {
				return writeTerminalRunRecord(runRecord)
			}
			return a.Printer.Success(map[string]any{"status": runRecord.Status, "last_seq": after, "hosted": true, "run": runRecord})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func runTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "stopped":
		return true
	default:
		return false
	}
}

func optionalStreamModeArgs(mode *string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 2 && cmd.Flags().Changed("stream") && *mode == string(streaming.ModePretty) {
			parsed, err := streaming.ParseMode(args[1])
			if err != nil {
				return err
			}
			*mode = string(parsed)
			return nil
		}
		return cobra.ExactArgs(1)(cmd, args)
	}
}

func streamResultRunID(data any) string {
	if values, ok := data.(map[string]any); ok {
		if runID, ok := values["id"].(string); ok {
			return runID
		}
	}
	return ""
}

func formatRunEvents(evs []state.Event, agentOnly bool) string {
	var b strings.Builder
	started := map[string]bool{}
	for _, e := range evs {
		line := formatEventLine(e, agentOnly, started)
		if line == "" {
			continue
		}
		b.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			b.WriteByte('\n')
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		return "(no matching log events)"
	}
	return out
}

func formatEventLine(e state.Event, agentOnly bool, started map[string]bool) string {
	payload := streaming.Payload(&e)
	message, _ := payload["message"].(string)
	if agentOnly {
		if e.Type != "agent.message" && e.Type != "agent.output" {
			return ""
		}
		if strings.TrimSpace(message) == "" || message == "codex completed" {
			return ""
		}
		return message
	}
	if line := streaming.PrettyLine(&e, started); line != "" {
		return fmt.Sprintf("%s  [%s]", line, e.ID)
	}
	if e.Type == "agent.prompt" {
		return fmt.Sprintf("  %-9s %s  [%s]", "Prompt", "prepared (collapsed)", e.ID)
	}
	if strings.HasPrefix(e.Type, "agent.") {
		return ""
	}
	if message == "" {
		message = strings.ReplaceAll(e.Type, ".", " ")
	}
	return fmt.Sprintf("  %-28s %s  [%s]", e.Type, message, e.ID)
}
