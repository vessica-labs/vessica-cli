package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/output"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func newRunCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "run", Short: "Execute and inspect workflow runs"}
	var (
		runnerName, model, reasoningEffort, sandboxName, prMode, startAt, stopAfter, reuse string
		streamMode, fromPhase, logsDetail                                                  string
		approveMethod                                                                      string
		concurrency                                                                        int
		watchAfter                                                                         int64
		preview, openPreview, eventsOnly, noStream, browser                                bool
		approveKeepPreview, approveKeepBranch                                              bool
		jsonl, logsAgentOnly, logsJSONL, logsRaw, watchUI                                  bool
		promptFile, promptModel, promptReasoning                                           string
		promptNoPush                                                                       bool
	)

	epicCmd := &cobra.Command{
		Use:   "epic <epic_id>",
		Args:  optionalStreamModeArgs(&streamMode),
		Short: "Run software epic workflow",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Config.Hosted.ControlPlaneURL != "" {
				hostedID := ""
				if mapping, mapErr := app.DB.GetExternalMapping(cmd.Context(), "vessica-hosted", "epic", args[0]); mapErr == nil {
					hostedID = mapping.ExternalID
				} else if _, localErr := app.DB.GetEpic(cmd.Context(), args[0]); localErr != nil {
					hostedID = args[0]
				}
				if hostedID != "" {
					if app.Flags.DryRun {
						return app.dryRun("run.epic.hosted", map[string]any{"local_epic_id": args[0], "hosted_epic_id": hostedID, "sandbox": "railway"})
					}
					if err := app.requireYes("start the hosted epic run"); err != nil {
						return err
					}
					result, err := app.startHostedEpicRun(cmd.Context(), hostedID)
					if err != nil {
						return err
					}
					return app.Printer.Success(result)
				}
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
				data, replayed, replayErr := app.idempotencyLookup(context.Background())
				if replayErr != nil {
					_ = streaming.WriteRecord(os.Stdout, streaming.ResultRecord("", nil, replayErr))
					return replayErr
				}
				if replayed {
					return streaming.WriteRecord(os.Stdout, streaming.ResultRecord(streamResultRunID(data), data, nil))
				}
			} else if replayed, replayErr := app.idempotencyReplay(context.Background()); replayErr != nil || replayed {
				return replayErr
			}
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: streamEnabled, EventsOnly: eventsOnly, StreamMode: mode}
			if openPreview {
				openPreviewWhenReady(eng)
			}
			var r *state.Run
			if mode == streaming.ModeUI {
				r, err = executeWithUI("Vessica epic run", app.Root, eng, func() (*state.Run, error) {
					return eng.RunEpic(context.Background(), opts)
				})
			} else {
				r, err = eng.RunEpic(context.Background(), opts)
			}
			if err != nil {
				if mode == streaming.ModeJSONL {
					_ = writeRunResult(mode, r, err)
					return err
				}
				_ = app.Printer.Success(r)
				return err
			}
			if err := app.idempotencyStore(context.Background(), r); err != nil {
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
	cmd.AddCommand(epicCmd)

	cmd.AddCommand(&cobra.Command{
		Use: "ticket <ticket_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			t, err := app.DB.GetTicket(context.Background(), args[0])
			if err != nil {
				return err
			}
			mode, err := resolveStreamMode(streamMode, noStream, eventsOnly, app.Flags.JSON)
			if err != nil {
				return err
			}
			streamEnabled := mode != streaming.ModeOff && mode != streaming.ModeUI
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: streamEnabled, EventsOnly: eventsOnly, StreamMode: mode}
			r, err := eng.RunEpic(context.Background(), run.Options{
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
			if err := app.loadWorkspace(); err != nil {
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
			list, err := app.DB.ListRuns(context.Background())
			if err != nil {
				return err
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if _, localErr := app.DB.GetRun(cmd.Context(), args[0]); localErr != nil && app.Config.Hosted.ControlPlaneURL != "" {
				runRecord, err := app.getHostedRun(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return app.Printer.Success(map[string]any{"run": runRecord, "hosted": true})
			}
			r, err := app.DB.GetRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			phases, _ := app.DB.ListPhases(context.Background(), r.ID)
			return app.Printer.Success(map[string]any{"run": r, "phases": phases})
		},
	})
	logsCmd := &cobra.Command{
		Use: "logs <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if logsDetail != "" {
				event, err := app.DB.GetEvent(context.Background(), logsDetail)
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
			evs, err := app.DB.ListEvents(context.Background(), args[0], 0)
			if err != nil {
				return err
			}
			if logsJSONL {
				for _, e := range evs {
					if err := streaming.WriteEvent(os.Stdout, &e); err != nil {
						return err
					}
				}
				r, err := app.DB.GetRun(context.Background(), args[0])
				if err != nil {
					return err
				}
				hydrateRunOutput(app.DB, r)
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
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if _, localErr := app.DB.GetRun(cmd.Context(), args[0]); localErr != nil && app.Config.Hosted.ControlPlaneURL != "" {
				return app.watchHostedRun(cmd.Context(), args[0], watchAfter, jsonl)
			}
			if watchUI && isTTYOutput() {
				eng := &run.Engine{}
				_, err := executeWithUI("Vessica run "+args[0], app.Root, eng, func() (*state.Run, error) {
					after := watchAfter
					for {
						evs, err := app.DB.ListEvents(context.Background(), args[0], after)
						if err != nil {
							return nil, err
						}
						for i := range evs {
							after = evs[i].Seq
							if eng.EventSink != nil {
								eng.EventSink(&evs[i])
							}
						}
						r, err := app.DB.GetRun(context.Background(), args[0])
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
				evs, err := app.DB.ListEvents(context.Background(), args[0], after)
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
				r, err := app.DB.GetRun(context.Background(), args[0])
				if err != nil {
					return err
				}
				if r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled" || r.Status == "stopped" {
					if jsonl {
						hydrateRunOutput(app.DB, r)
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
			if err := app.loadWorkspace(); err != nil {
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
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: streamEnabled, EventsOnly: eventsOnly, StreamMode: mode}
			var r *state.Run
			if mode == streaming.ModeUI {
				r, err = executeWithUI("Resume Vessica run", app.Root, eng, func() (*state.Run, error) {
					return eng.Resume(context.Background(), args[0], fromPhase)
				})
			} else {
				r, err = eng.Resume(context.Background(), args[0], fromPhase)
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
			if err := app.loadWorkspace(); err != nil {
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
			if replayed, replayErr := app.idempotencyReplay(context.Background()); replayErr != nil || replayed {
				return replayErr
			}
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}
			result, err := eng.ApproveRun(context.Background(), args[0], opts)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(context.Background(), result); err != nil {
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
			if err := app.loadWorkspace(); err != nil {
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
			sandboxRecord, err := app.DB.GetSandboxForRun(context.Background(), args[0])
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
			if replayed, replayErr := app.idempotencyReplay(context.Background()); replayErr != nil || replayed {
				return replayErr
			}
			result, err := (&run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}).PromptSandbox(context.Background(), sandboxRecord.ID, opts)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(context.Background(), result); err != nil {
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
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("run.rollback", map[string]any{"run_id": args[0]})
			}
			if err := app.requireYes("roll back the run"); err != nil {
				return err
			}
			if replayed, replayErr := app.idempotencyReplay(context.Background()); replayErr != nil || replayed {
				return replayErr
			}
			result, err := (&run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}).RollbackRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(context.Background(), result); err != nil {
				return err
			}
			return app.Printer.Success(result)
		},
	}
	cmd.AddCommand(rollback)

	cmd.AddCommand(&cobra.Command{
		Use: "cancel <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("run.cancel", map[string]any{"id": args[0]})
			}
			if err := app.requireYes("cancel run"); err != nil {
				return err
			}
			r, err := app.DB.GetRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			r.Status = "cancelled"
			r.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			_ = app.DB.UpdateRun(context.Background(), r)
			if sandboxes, listErr := app.DB.ListSandboxesForRun(context.Background(), r.ID); listErr == nil {
				for i := range sandboxes {
					_ = retention.Destroy(context.Background(), app.DB, app.Root, &sandboxes[i], "cancelled")
				}
			}
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}
			eng.RecordRunKnowledge(cmd.Context(), r, "run.cancelled", "Run was cancelled", "run:"+r.ID+":cancelled")
			return app.Printer.Success(r)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "artifacts <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			r, err := app.DB.GetRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			arts, err := app.DB.ListArtifactsForRun(context.Background(), r.ID)
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatArtifacts(arts))
			}
			return app.Printer.Success(arts)
		},
	})
	previewCmd := &cobra.Command{
		Use: "preview <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			r, err := app.DB.GetRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			if r.PreviewURL == "" {
				eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: false}
				r, err = eng.EnsurePreview(context.Background(), r.ID)
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

	cmd.AddCommand(&cobra.Command{
		Use: "receipt <run_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			r, err := app.DB.GetRun(context.Background(), args[0])
			if err != nil {
				return err
			}
			if r.ReceiptID == "" {
				return app.Printer.Fail("no_receipt", "run has no receipt yet", "")
			}
			rcpt, err := app.DB.GetReceipt(context.Background(), r.ReceiptID)
			if err != nil {
				return err
			}
			var body any
			_ = json.Unmarshal([]byte(rcpt.BodyJSON), &body)
			return app.Printer.Success(map[string]any{"receipt": rcpt, "body": body})
		},
	})
	return cmd
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

func (a *App) listHostedRuns(ctx context.Context) ([]*state.Run, error) {
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	var runs []*state.Run
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/runs"
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
