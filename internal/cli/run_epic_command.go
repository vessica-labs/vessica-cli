package cli

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/output"
	"github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func newRunEpicCmd(app *App) *cobra.Command {
	var runnerName, model, reasoningEffort, sandboxName, prMode, startAt, stopAfter, reuse string
	var streamMode string
	var concurrency int
	var preview, openPreview, eventsOnly, noStream bool

	epicCmd := &cobra.Command{
		Use:   "epic <epic_id>",
		Args:  optionalStreamModeArgs(&streamMode),
		Short: "Run software epic workflow",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Config.Hosted.ControlPlaneURL != "" {
				if app.Flags.DryRun {
					return app.dryRun("run.epic.hosted", map[string]any{"epic_id": args[0], "sandbox": "railway"})
				}
				if err := app.requireYes("start the hosted epic run"); err != nil {
					return err
				}
				result, err := app.startHostedEpicRun(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return app.Printer.Success(result)
			}
			mode, err := resolveStreamMode(streamMode, noStream, eventsOnly, app.Flags.JSON)
			if err != nil {
				return err
			}
			streamEnabled := mode != streaming.ModeOff && mode != streaming.ModeUI
			opts := run.Options{
				EpicID:          args[0],
				Runner:          runnerName,
				Model:           model,
				ReasoningEffort: reasoningEffort,
				Sandbox:         sandboxName,
				Concurrency:     concurrency,
				Preview:         preview || openPreview,
				PRMode:          prMode,
				StartAt:         startAt,
				StopAfter:       stopAfter,
				ReuseArtifacts:  reuse,
				Stream:          streamEnabled,
				EventsOnly:      eventsOnly,
				StreamMode:      mode,
			}
			if app.Flags.DryRun {
				if mode == streaming.ModeJSONL {
					return streaming.WriteRecord(os.Stdout, streaming.ResultRecord("", map[string]any{"dry_run": true, "action": "run.epic", "would": opts}, nil))
				}
				return app.dryRun("run.epic", opts)
			}
			if app.Flags.JSON && mode != streaming.ModeJSONL && !app.Flags.Yes {
				return app.requireYes("start the epic run")
			}
			if mode == streaming.ModeJSONL && !app.Flags.Yes {
				err := &output.Error{Body: output.ErrorBody{Code: "confirmation_required", Message: "confirmation required to start the epic run", Hint: "review the action, then repeat with --yes"}, Printed: true}
				record := streaming.ResultRecord("", nil, err)
				record.Error.Code = "confirmation_required"
				_ = streaming.WriteRecord(os.Stdout, record)
				return err
			}
			if mode == streaming.ModeJSONL {
				data, replayed, replayErr := app.idempotencyLookup(cmd.Context())
				if replayErr != nil {
					_ = streaming.WriteRecord(os.Stdout, streaming.ResultRecord("", nil, replayErr))
					return replayErr
				}
				if replayed {
					return streaming.WriteRecord(os.Stdout, streaming.ResultRecord(streamResultRunID(data), data, nil))
				}
			} else if replayed, replayErr := app.idempotencyReplay(cmd.Context()); replayErr != nil || replayed {
				return replayErr
			}
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: streamEnabled, EventsOnly: eventsOnly, StreamMode: mode}
			if openPreview {
				openPreviewWhenReady(eng)
			}
			var r *state.Run
			if mode == streaming.ModeUI {
				r, err = executeWithUI("Vessica epic run", app.Root, eng, func() (*state.Run, error) {
					return eng.RunEpic(cmd.Context(), opts)
				})
			} else {
				r, err = eng.RunEpic(cmd.Context(), opts)
			}
			if err != nil {
				if mode == streaming.ModeJSONL {
					_ = writeRunResult(mode, r, err)
					return err
				}
				runID := "<run-id>"
				if r != nil && r.ID != "" {
					runID = r.ID
				}
				return app.Printer.Fail("run_failed", err.Error(), "inspect the run with `ves run view "+runID+"` or replay its events with `ves run logs "+runID+"`")
			}
			if err := app.idempotencyStore(cmd.Context(), r); err != nil {
				if mode == streaming.ModeJSONL {
					_ = writeRunResult(mode, r, err)
				}
				return err
			}
			if mode == streaming.ModeJSONL {
				return writeRunResult(mode, r, nil)
			}
			return app.Printer.Success(r)
		},
	}
	epicCmd.Flags().StringVar(&runnerName, "runner", "", "codex|claude|cursor|pi")
	epicCmd.Flags().StringVar(&model, "model", "", "Codex model ID (for example gpt-5.6-terra, gpt-5.6-sol, or gpt-5.6-luna)")
	epicCmd.Flags().StringVar(&reasoningEffort, "reasoning-effort", "", "Codex reasoning effort: low|medium|high|xhigh")
	epicCmd.Flags().StringVar(&sandboxName, "sandbox", "", "docker")
	epicCmd.Flags().IntVar(&concurrency, "concurrency", 3, "coding workers")
	epicCmd.Flags().BoolVar(&preview, "preview", false, "enable preview phase")
	epicCmd.Flags().BoolVar(&openPreview, "open-preview", false, "enable preview and open it when ready")
	epicCmd.Flags().StringVar(&prMode, "pr", "none", "draft|ready|none")
	epicCmd.Flags().StringVar(&streamMode, "stream", "pretty", "stream mode: pretty|ui|events|jsonl|raw|off")
	epicCmd.Flags().Lookup("stream").NoOptDefVal = "pretty"
	epicCmd.Flags().BoolVar(&noStream, "no-stream", false, "disable live run stream")
	epicCmd.Flags().BoolVar(&eventsOnly, "events-only", false, "stream compact run events instead of agent output")
	epicCmd.Flags().StringVar(&startAt, "start-at", "", "start phase")
	epicCmd.Flags().StringVar(&stopAfter, "stop-after", "", "stop after phase")
	epicCmd.Flags().StringVar(&reuse, "reuse-artifacts", "", "approved")
	return epicCmd
}
