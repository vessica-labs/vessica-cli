package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	appservice "github.com/vessica-labs/vessica-cli/internal/app"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/output"
	"github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func newRunCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "run", Short: "Execute and inspect workflow runs"}
	var (
		streamMode, fromPhase, logsDetail                 string
		approveMethod                                     string
		watchAfter                                        int64
		eventsOnly, noStream, browser                     bool
		approveKeepPreview, approveKeepBranch             bool
		jsonl, logsAgentOnly, logsJSONL, logsRaw, watchUI bool
		promptFile, promptModel, promptReasoning          string
		promptNoPush                                      bool
	)
	cmd.AddCommand(newRunEpicCmd(app))
	cmd.AddCommand(&cobra.Command{
		Use: "ticket <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			t, err := app.DB.GetTicket(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			mode, err := resolveStreamMode(streamMode, noStream, eventsOnly, app.Flags.JSON)
			if err != nil {
				return err
			}
			streamEnabled := mode != streaming.ModeOff && mode != streaming.ModeUI
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: streamEnabled, EventsOnly: eventsOnly, StreamMode: mode}
			r, err := eng.RunEpic(cmd.Context(), run.Options{
				EpicID:      t.EpicID,
				TicketID:    t.ID,
				StartAt:     "code",
				StopAfter:   "code",
				Concurrency: 1,
				Stream:      streamEnabled,
				EventsOnly:  eventsOnly,
				StreamMode:  mode,
			})
			if err != nil {
				return err
			}
			return app.Printer.Success(r)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Config.Hosted.ControlPlaneURL != "" {
				list, err := app.listHostedRuns(cmd.Context())
				if err != nil {
					return err
				}
				return app.Printer.Success(list)
			}
			list, err := app.DB.ListRuns(cmd.Context())
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				runRecord, err := app.getHostedRun(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return app.Printer.Success(map[string]any{"run": runRecord, "hosted": true})
			}
			r, err := app.DB.GetRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			phases, _ := app.DB.ListPhases(cmd.Context(), r.ID)
			return app.Printer.Success(map[string]any{"run": r, "phases": phases})
		},
	})
	logsCmd := &cobra.Command{
		Use: "logs <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				return app.printHostedRunLogs(cmd.Context(), args[0], logsDetail, logsAgentOnly, logsJSONL, logsRaw)
			}
			if logsDetail != "" {
				event, err := app.DB.GetEvent(cmd.Context(), logsDetail)
				if err != nil {
					return err
				}
				if event.RunID != args[0] {
					return fmt.Errorf("event %s does not belong to run %s", event.ID, args[0])
				}
				return app.Printer.Success(eventDetail(app.Root, event))
			}
			if logsRaw {
				raw, err := rawLog(app.Root, args[0])
				if err != nil {
					return err
				}
				return app.Printer.Success(strings.TrimRight(raw, "\n"))
			}
			evs, err := app.DB.ListEvents(cmd.Context(), args[0], 0)
			if err != nil {
				return err
			}
			if logsJSONL {
				for _, e := range evs {
					if err := streaming.WriteEvent(os.Stdout, &e); err != nil {
						return err
					}
				}
				r, err := app.DB.GetRun(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				hydrateRunOutput(cmd.Context(), app.DB, r)
				return writeTerminalRunRecord(r)
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatRunEvents(evs, logsAgentOnly))
			}
			return app.Printer.Success(evs)
		},
	}
	logsCmd.Flags().BoolVar(&logsAgentOnly, "agent-output", false, "show only agent text output")
	logsCmd.Flags().BoolVar(&logsJSONL, "jsonl", false, "emit the versioned JSONL stream")
	logsCmd.Flags().StringVar(&logsDetail, "detail", "", "show expanded detail for one event id")
	logsCmd.Flags().BoolVar(&logsRaw, "raw", false, "replay the complete raw Codex JSONL log")
	cmd.AddCommand(logsCmd)
	watch := &cobra.Command{
		Use: "watch <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				return app.watchHostedRun(cmd.Context(), args[0], watchAfter, jsonl)
			}
			if watchUI && isTTYOutput() {
				eng := &run.Engine{}
				_, err := executeWithUI("Vessica run "+args[0], app.Root, eng, func() (*state.Run, error) {
					after := watchAfter
					for {
						evs, err := app.DB.ListEvents(cmd.Context(), args[0], after)
						if err != nil {
							return nil, err
						}
						for i := range evs {
							after = evs[i].Seq
							if eng.EventSink != nil {
								eng.EventSink(&evs[i])
							}
						}
						r, err := app.DB.GetRun(cmd.Context(), args[0])
						if err != nil {
							return nil, err
						}
						if r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled" || r.Status == "stopped" {
							return r, nil
						}
						time.Sleep(500 * time.Millisecond)
					}
				})
				return err
			}
			after := watchAfter
			started := map[string]bool{}
			for {
				evs, err := app.DB.ListEvents(cmd.Context(), args[0], after)
				if err != nil {
					return err
				}
				for _, e := range evs {
					after = e.Seq
					if jsonl {
						if err := streaming.WriteEvent(os.Stdout, &e); err != nil {
							return err
						}
					} else if app.Flags.JSON {
						_ = output.PrintJSONL(os.Stdout, e)
					} else {
						if line := formatEventLine(e, false, started); line != "" {
							app.Printer.Info("%s", line)
						}
					}
				}
				r, err := app.DB.GetRun(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				if r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled" || r.Status == "stopped" {
					if jsonl {
						hydrateRunOutput(cmd.Context(), app.DB, r)
						return writeTerminalRunRecord(r)
					}
					return app.Printer.Success(map[string]any{"status": r.Status, "last_seq": after})
				}
				time.Sleep(500 * time.Millisecond)
			}
		},
	}
	watch.Flags().BoolVar(&jsonl, "jsonl", false, "emit the versioned JSONL stream")
	watch.Flags().Int64Var(&watchAfter, "after-seq", 0, "emit events after this sequence number")
	watch.Flags().BoolVar(&watchUI, "ui", false, "watch with expandable interactive UI")
	cmd.AddCommand(watch)

	resume := &cobra.Command{
		Use: "resume <run_id>", Args: optionalStreamModeArgs(&streamMode), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("run.resume", map[string]any{"run_id": args[0], "from": fromPhase})
			}
			if err := app.requireYes("resume the run"); err != nil {
				return err
			}
			mode, err := resolveStreamMode(streamMode, noStream, eventsOnly, app.Flags.JSON)
			if err != nil {
				return err
			}
			streamEnabled := mode != streaming.ModeOff && mode != streaming.ModeUI
			if config.IsHostedAttachment(app.Config) {
				return app.executeHostedResume(cmd.Context(), args[0], fromPhase, mode)
			}
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: streamEnabled, EventsOnly: eventsOnly, StreamMode: mode}
			var r *state.Run
			if mode == streaming.ModeUI {
				r, err = executeWithUI("Resume Vessica run", app.Root, eng, func() (*state.Run, error) {
					return eng.Resume(cmd.Context(), args[0], fromPhase)
				})
			} else {
				r, err = eng.Resume(cmd.Context(), args[0], fromPhase)
			}
			if err != nil {
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
	resume.Flags().StringVar(&fromPhase, "from", "", "phase to resume from")
	resume.Flags().StringVar(&streamMode, "stream", "pretty", "stream mode: pretty|ui|events|jsonl|raw|off")
	resume.Flags().Lookup("stream").NoOptDefVal = "pretty"
	resume.Flags().BoolVar(&noStream, "no-stream", false, "disable live run stream")
	resume.Flags().BoolVar(&eventsOnly, "events-only", false, "compatibility alias for --stream events")
	cmd.AddCommand(resume)

	approve := &cobra.Command{
		Use:   "approve <run_id>",
		Short: "Approve the preview and merge its pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			opts := run.ApproveOptions{MergeMethod: approveMethod, KeepPreview: approveKeepPreview, KeepBranch: approveKeepBranch}
			if app.Flags.DryRun {
				return app.dryRun("run.approve", map[string]any{"run_id": args[0], "merge_method": approveMethod, "keep_preview": approveKeepPreview, "keep_branch": approveKeepBranch})
			}
			if err := app.requireYes("approve and merge the run pull request"); err != nil {
				return err
			}
			if replayed, replayErr := app.idempotencyReplay(cmd.Context()); replayErr != nil || replayed {
				return replayErr
			}
			if config.IsHostedAttachment(app.Config) {
				result, err := app.approveHostedRun(cmd.Context(), args[0], opts)
				if err != nil {
					return err
				}
				return app.Printer.Success(result)
			}
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}
			result, err := eng.ApproveRun(cmd.Context(), args[0], opts)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(cmd.Context(), result); err != nil {
				return err
			}
			return app.Printer.Success(result)
		},
	}
	approve.Flags().StringVar(&approveMethod, "merge-method", "squash", "merge method: squash|merge|rebase")
	approve.Flags().BoolVar(&approveKeepPreview, "keep-preview", false, "keep the preview sandbox running after merge")
	approve.Flags().BoolVar(&approveKeepBranch, "keep-branch", false, "keep the remote integration branch after merge")
	cmd.AddCommand(approve)

	promptCmd := &cobra.Command{
		Use: "prompt <run_id> [prompt]", Short: "Refine the retained sandbox attached to a run", Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			prompt := strings.TrimSpace(strings.Join(args[1:], " "))
			if promptFile != "" {
				if prompt != "" {
					return fmt.Errorf("provide a positional prompt or --file, not both")
				}
				body, err := os.ReadFile(promptFile)
				if err != nil {
					return err
				}
				prompt = strings.TrimSpace(string(body))
			}
			if prompt == "" {
				return app.Printer.Fail("missing_prompt", "prompt is required", "provide text or --file")
			}
			if config.IsHostedAttachment(app.Config) {
				if promptModel != "" || promptReasoning != "" || promptNoPush {
					return app.Printer.Fail("hosted_prompt_option_unsupported", "hosted refinements use the retained run model and always push preview updates", "remove --model, --reasoning-effort, and --no-push")
				}
				if app.Flags.DryRun {
					return app.dryRun("run.prompt", map[string]any{"run_id": args[0]})
				}
				if app.Flags.JSON && !app.Flags.Yes {
					return app.requireYes("refine the retained run sandbox")
				}
				result, err := app.promptHostedRun(cmd.Context(), args[0], prompt)
				if err != nil {
					return err
				}
				return app.Printer.Success(result)
			}
			sandboxRecord, err := app.DB.GetSandboxForRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			opts := run.PromptOptions{Prompt: prompt, Model: promptModel, ReasoningEffort: promptReasoning, Push: !promptNoPush}
			if app.Flags.DryRun {
				return app.dryRun("run.prompt", map[string]any{"run_id": args[0], "sandbox_id": sandboxRecord.ID, "model": promptModel, "push": !promptNoPush})
			}
			if app.Flags.JSON && !app.Flags.Yes {
				return app.requireYes("refine the retained run sandbox")
			}
			if replayed, replayErr := app.idempotencyReplay(cmd.Context()); replayErr != nil || replayed {
				return replayErr
			}
			result, err := (&run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}).PromptSandbox(cmd.Context(), sandboxRecord.ID, opts)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(cmd.Context(), result); err != nil {
				return err
			}
			return app.Printer.Success(result)
		},
	}
	promptCmd.Flags().StringVar(&promptFile, "file", "", "read prompt from file")
	promptCmd.Flags().StringVar(&promptModel, "model", "", "Codex model ID")
	promptCmd.Flags().StringVar(&promptReasoning, "reasoning-effort", "", "Codex reasoning effort")
	promptCmd.Flags().BoolVar(&promptNoPush, "no-push", false, "commit without pushing")
	cmd.AddCommand(promptCmd)

	rollback := &cobra.Command{
		Use: "rollback <run_id>", Short: "Close the run pull request and destroy its preview", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("run.rollback", map[string]any{"run_id": args[0]})
			}
			if err := app.requireYes("roll back the run"); err != nil {
				return err
			}
			if replayed, replayErr := app.idempotencyReplay(cmd.Context()); replayErr != nil || replayed {
				return replayErr
			}
			if config.IsHostedAttachment(app.Config) {
				result, err := app.rollbackHostedRun(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return app.Printer.Success(result)
			}
			result, err := (&run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}).RollbackRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(cmd.Context(), result); err != nil {
				return err
			}
			return app.Printer.Success(result)
		},
	}
	cmd.AddCommand(rollback)

	cmd.AddCommand(&cobra.Command{
		Use: "cancel <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("run.cancel", map[string]any{"id": args[0]})
			}
			if err := app.requireYes("cancel run"); err != nil {
				return err
			}
			if config.IsHostedAttachment(app.Config) {
				return app.executeHostedCancel(cmd.Context(), args[0])
			}
			r, err := appservice.NewRunLifecycle(app.DB, app.Root, app.Config, nil).Cancel(cmd.Context(), args[0], "cli")
			if err != nil {
				return err
			}
			return app.Printer.Success(r)
		},
	})
	cmd.AddCommand(newRunArtifactsCmd(app))
	previewCmd := &cobra.Command{
		Use: "preview <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				return app.executeHostedPreview(cmd.Context(), args[0], browser)
			}
			r, err := app.DB.GetRun(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if r.PreviewURL == "" {
				eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: false}
				r, err = eng.EnsurePreview(cmd.Context(), r.ID)
				if err != nil {
					return app.Printer.Fail("no_preview", err.Error(), "run with --preview or configure .vessica/harness.yaml preview")
				}
			}
			if browser {
				_ = run.OpenPreview(r.PreviewURL)
			}
			return app.Printer.Success(map[string]string{"preview_url": r.PreviewURL})
		},
	}
	previewCmd.Flags().BoolVar(&browser, "browser", false, "open browser")
	cmd.AddCommand(previewCmd)

	cmd.AddCommand(newRunReceiptCmd(app))
	return cmd
}
