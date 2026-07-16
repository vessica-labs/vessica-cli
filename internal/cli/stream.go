package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
	runtui "github.com/vessica-labs/vessica-cli/internal/tui"
)

func resolveStreamMode(raw string, noStream, eventsOnly, jsonMode bool) (streaming.Mode, error) {
	if noStream {
		return streaming.ModeOff, nil
	}
	if eventsOnly {
		return streaming.ModeEvents, nil
	}
	mode, err := streaming.ParseMode(raw)
	if err != nil {
		return "", err
	}
	if jsonMode && mode != streaming.ModeJSONL {
		return streaming.ModeOff, nil
	}
	if mode == streaming.ModeUI && !isatty.IsTerminal(os.Stdout.Fd()) {
		return streaming.ModePretty, nil
	}
	return mode, nil
}

func writeRunResult(mode streaming.Mode, runResult *state.Run, runErr error) error {
	if mode != streaming.ModeJSONL {
		return nil
	}
	runID := ""
	if runResult != nil {
		runID = runResult.ID
	}
	return streaming.WriteRecord(os.Stdout, streaming.ResultRecord(runID, safeRunResult(runResult), runErr))
}

func writeTerminalRunRecord(runResult *state.Run) error {
	if runResult == nil {
		return streaming.WriteRecord(os.Stdout, streaming.ResultRecord("", nil, fmt.Errorf("run not found")))
	}
	var runErr error
	if runResult.Status == "failed" {
		message := strings.TrimSpace(runResult.Error)
		if message == "" {
			message = "run failed"
		}
		runErr = fmt.Errorf("%s", message)
	}
	return streaming.WriteRecord(os.Stdout, streaming.ResultRecord(runResult.ID, safeRunResult(runResult), runErr))
}

func hydrateRunOutput(ctx context.Context, db *state.DB, runResult *state.Run) {
	if db == nil || runResult == nil {
		return
	}
	if sandboxRecord, err := db.GetSandboxForRun(ctx, runResult.ID); err == nil {
		runResult.SandboxID = sandboxRecord.ID
		runResult.SandboxExpiresAt = sandboxRecord.ExpiresAt
		if runResult.PreviewURL == "" {
			runResult.PreviewURL = sandboxRecord.PreviewURL
		}
	}
}

func safeRunResult(runResult *state.Run) *state.Run {
	if runResult == nil {
		return nil
	}
	copy := *runResult
	copy.Error = redaction.Redact(copy.Error)
	return &copy
}

func isTTYOutput() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

type runResult struct {
	run *state.Run
	err error
}

func executeWithUI(title, root string, engine *run.Engine, execute func() (*state.Run, error)) (*state.Run, error) {
	model := runtui.NewModel(title, func(event *state.Event) string {
		return eventDetail(root, event)
	})
	program := tea.NewProgram(model, tea.WithAltScreen())
	previousSink := engine.EventSink
	engine.EventSink = func(event *state.Event) {
		if event != nil {
			if previousSink != nil {
				previousSink(event)
			}
			program.Send(runtui.EventMsg{Event: *event})
		}
	}
	resultCh := make(chan runResult, 1)
	go func() {
		r, err := execute()
		status := "completed"
		if r != nil && r.Status != "" {
			status = r.Status
		} else if err != nil {
			status = "failed"
		}
		program.Send(runtui.DoneMsg{Status: status})
		resultCh <- runResult{run: r, err: err}
	}()
	_, uiErr := program.Run()
	result := <-resultCh
	if result.err != nil {
		return result.run, result.err
	}
	if uiErr != nil {
		return result.run, uiErr
	}
	return result.run, nil
}

func openPreviewWhenReady(engine *run.Engine) {
	previousSink := engine.EventSink
	var once sync.Once
	engine.EventSink = func(event *state.Event) {
		if previousSink != nil {
			previousSink(event)
		}
		if event == nil || event.Type != "preview.ready" {
			return
		}
		payload := streaming.Payload(event)
		url, _ := payload["url"].(string)
		if strings.TrimSpace(url) != "" {
			once.Do(func() { _ = run.OpenPreview(url) })
		}
	}
}

func eventDetail(root string, event *state.Event) string {
	if event == nil {
		return ""
	}
	detail := streaming.EventDetail(event)
	payload := streaming.Payload(event)
	pathValue, _ := payload["raw_log_path"].(string)
	offset, offsetOK := numberAsInt64(payload["raw_log_offset"])
	length, lengthOK := numberAsInt64(payload["raw_log_length"])
	if pathValue == "" || !offsetOK || !lengthOK || length <= 0 {
		return detail
	}
	fullPath := filepath.Join(root, filepath.FromSlash(pathValue))
	runsRoot := filepath.Join(root, ".vessica", "runs") + string(os.PathSeparator)
	abs, err := filepath.Abs(fullPath)
	if err != nil || !strings.HasPrefix(abs, runsRoot) {
		return detail
	}
	f, err := os.Open(abs)
	if err != nil {
		return detail
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return detail
	}
	data := make([]byte, length)
	n, err := io.ReadFull(f, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return detail
	}
	raw := strings.TrimSpace(string(data[:n]))
	if event.Type == "agent.prompt" {
		var prompt map[string]any
		if json.Unmarshal([]byte(raw), &prompt) == nil {
			b, _ := json.MarshalIndent(prompt, "", "  ")
			return string(b)
		}
	}
	if detail != "" && detail != "{}" {
		return detail + "\n\nRaw event:\n" + raw
	}
	return raw
}

func rawLog(root, runID string) (string, error) {
	path := filepath.Join(root, ".vessica", "runs", runID, "agent.jsonl")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("raw agent log not found for run %s", runID)
	}
	return string(b), err
}

func numberAsInt64(value any) (int64, bool) {
	switch n := value.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}
