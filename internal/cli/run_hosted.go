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
	"github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func (a *App) executeHostedResume(ctx context.Context, runID, from string, mode streaming.Mode) error {
	result, err := a.resumeHostedRun(ctx, runID, from)
	if err != nil {
		return err
	}
	if mode != streaming.ModeOff {
		return a.watchHostedRun(ctx, runID, 0, mode == streaming.ModeJSONL)
	}
	return a.Printer.Success(result)
}

func (a *App) executeHostedCancel(ctx context.Context, runID string) error {
	result, err := a.cancelHostedRun(ctx, runID)
	if err != nil {
		return err
	}
	return a.Printer.Success(result)
}

func (a *App) executeHostedPreview(ctx context.Context, runID string, browser bool) error {
	runRecord, err := a.getHostedRun(ctx, runID)
	if err != nil {
		return err
	}
	if runRecord.PreviewURL == "" {
		return a.Printer.Fail("no_public_preview", "hosted run does not have a ready public preview", "inspect ves run view and hosted preview outcome")
	}
	if browser {
		_ = run.OpenPreview(runRecord.PreviewURL)
	}
	return a.Printer.Success(map[string]string{"url": runRecord.PreviewURL})
}

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

func (a *App) getHostedStatus(ctx context.Context) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var status map[string]any
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/status"
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &status); err != nil {
		return nil, err
	}
	return status, nil
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

func (a *App) listHostedEpics(ctx context.Context) ([]state.Epic, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var epics []state.Epic
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/epics?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &epics); err != nil {
		return nil, err
	}
	return epics, nil
}

func (a *App) getHostedEpic(ctx context.Context, epicID string) (*state.Epic, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var epic state.Epic
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/epics/" + url.PathEscape(epicID) + "?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &epic); err != nil {
		return nil, err
	}
	return &epic, nil
}

func (a *App) approveHostedRun(ctx context.Context, runID string, options any) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs/" + url.PathEscape(runID) + "/approve"
	if err := hostedRequestWithKey(ctx, http.MethodPost, endpoint, secrets.APIToken, firstNonEmpty(a.Flags.IdempotencyKey, "approve-"+runID), options, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) resumeHostedRun(ctx context.Context, runID, from string) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs/" + url.PathEscape(runID) + "/resume?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	key := firstNonEmpty(a.Flags.IdempotencyKey, "resume-"+runID+"-"+firstNonEmpty(from, "current"))
	if err := hostedRequestWithKey(ctx, http.MethodPost, endpoint, secrets.APIToken, key, map[string]string{"from": from}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) cancelHostedRun(ctx context.Context, runID string) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs/" + url.PathEscape(runID) + "/cancel?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	if err := hostedRequestWithKey(ctx, http.MethodPost, endpoint, secrets.APIToken, firstNonEmpty(a.Flags.IdempotencyKey, "cancel-"+runID), nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) getHostedEpicStatus(ctx context.Context, epicID string) (map[string]any, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/epics/" + url.PathEscape(epicID) + "/status?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) listHostedSandboxes(ctx context.Context) ([]state.Sandbox, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var records []state.Sandbox
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/sandboxes?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (a *App) getHostedSandbox(ctx context.Context, sandboxID string) (*state.Sandbox, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var record state.Sandbox
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/sandboxes/" + url.PathEscape(sandboxID) + "?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (a *App) getHostedSandboxLogs(ctx context.Context, sandboxID string) (map[string]string, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var result map[string]string
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/sandboxes/" + url.PathEscape(sandboxID) + "/logs?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *App) getHostedRun(ctx context.Context, runID string) (*state.Run, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var runRecord state.Run
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs/" + url.PathEscape(runID) + "?repository_id=" + url.QueryEscape(a.Config.Attachment.RepositoryID)
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
	endpoint := fmt.Sprintf("%s/api/v1/runs/%s/events?after=%d&repository_id=%s", strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/"), url.PathEscape(runID), after, url.QueryEscape(a.Config.Attachment.RepositoryID))
	if err := hostedRequest(ctx, http.MethodGet, endpoint, secrets.APIToken, nil, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (a *App) printHostedRunLogs(ctx context.Context, runID, detail string, agentOnly, jsonl, raw bool) error {
	if raw {
		return a.Printer.Fail("hosted_raw_log_unavailable", "hosted runs expose sanitized events rather than repository-local raw logs", "use ves run logs <run_id> --json")
	}
	events, err := a.listHostedRunEvents(ctx, runID, 0)
	if err != nil {
		return err
	}
	if detail != "" {
		for i := range events {
			if events[i].ID == detail {
				return a.Printer.Success(events[i])
			}
		}
		return a.Printer.Fail("event_not_found", "event was not found in this hosted run", "")
	}
	if jsonl {
		for i := range events {
			if err := streaming.WriteEvent(os.Stdout, &events[i]); err != nil {
				return err
			}
		}
		runRecord, err := a.getHostedRun(ctx, runID)
		if err != nil {
			return err
		}
		return writeTerminalRunRecord(runRecord)
	}
	if !a.Flags.JSON {
		return a.Printer.Success(formatRunEvents(events, agentOnly))
	}
	return a.Printer.Success(events)
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
