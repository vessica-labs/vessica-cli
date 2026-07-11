package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/retention"
	"github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	"github.com/vessica-labs/vessica-cli/internal/streaming"
)

func newSandboxCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "sandbox", Short: "Inspect sandboxes"}
	var browser bool
	var retainFor, olderThan, runID string
	var promptFile, promptModel, promptReasoning, promptStream string
	var promptNoPush, promptNoStream, promptEventsOnly bool

	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			list, err := app.DB.ListSandboxes(context.Background())
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatSandboxes(list))
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <sandbox_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			s, err := app.DB.GetSandbox(context.Background(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(s)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "logs <sandbox_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			s, err := app.DB.GetSandbox(context.Background(), args[0])
			if err != nil {
				return err
			}
			if s.ContainerID == "" || s.ContainerID == "local" {
				return app.Printer.Success(map[string]string{"message": "no docker logs for local sandbox", "id": s.ID})
			}
			_ = retention.Touch(context.Background(), app.DB, s)
			out, err := exec.Command("docker", "logs", "--tail", "200", s.ContainerID).CombinedOutput()
			if err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"logs": string(out)})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "shell <sandbox_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			s, err := app.DB.GetSandbox(context.Background(), args[0])
			if err != nil {
				return err
			}
			if s.ContainerID == "" || s.ContainerID == "local" {
				c := exec.Command("bash")
				c.Dir = app.Root
				c.Stdin = os.Stdin
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				return c.Run()
			}
			_ = retention.Touch(context.Background(), app.DB, s)
			c := exec.Command("docker", "exec", "-it", s.ContainerID, "bash")
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	})
	tunnel := &cobra.Command{
		Use: "tunnel <sandbox_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			s, err := app.DB.GetSandbox(context.Background(), args[0])
			if err != nil {
				return err
			}
			url := s.PreviewURL
			if url == "" {
				if s.RunID != "" {
					eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config}
					r, err := eng.EnsurePreview(context.Background(), s.RunID)
					if err == nil {
						url = r.PreviewURL
					}
				}
				if url == "" {
					port := s.PreviewPort
					if port == 0 {
						port = 3000
					}
					url = "http://127.0.0.1:" + itoa(port)
				}
			}
			if browser {
				_ = run.OpenPreview(url)
			}
			if latest, err := app.DB.GetSandboxForRun(context.Background(), s.RunID); err == nil && latest.PreviewURL == url {
				s = latest
				_ = retention.Touch(context.Background(), app.DB, s)
			}
			return app.Printer.Success(map[string]string{"url": url, "sandbox_id": s.ID, "expires_at": retention.EffectiveExpiry(s).Format(time.RFC3339Nano)})
		},
	}
	tunnel.Flags().BoolVar(&browser, "browser", false, "open browser")
	cmd.AddCommand(tunnel)

	promptCmd := &cobra.Command{
		Use:   "prompt <sandbox_id> [prompt]",
		Short: "Ask Codex to refine a live sandbox directly",
		Args:  cobra.MinimumNArgs(1),
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
				path := promptFile
				if !filepath.IsAbs(path) {
					path = filepath.Join(app.Root, path)
				}
				body, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				prompt = strings.TrimSpace(string(body))
			}
			if prompt == "" {
				return fmt.Errorf("prompt is required as an argument or --file")
			}
			mode, err := resolveStreamMode(promptStream, promptNoStream, promptEventsOnly, app.Flags.JSON)
			if err != nil {
				return err
			}
			opts := run.PromptOptions{Prompt: prompt, Model: promptModel, ReasoningEffort: promptReasoning, Push: !promptNoPush}
			if app.Flags.DryRun {
				return app.dryRun("sandbox.prompt", map[string]any{"sandbox_id": args[0], "model": promptModel, "reasoning_effort": promptReasoning, "push": !promptNoPush, "prompt": prompt})
			}
			streamEnabled := mode != streaming.ModeOff && mode != streaming.ModeUI
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: streamEnabled, EventsOnly: promptEventsOnly, StreamMode: mode}
			var result *run.PromptResult
			if mode == streaming.ModeUI {
				_, err = executeWithUI("Refine Vessica sandbox", app.Root, eng, func() (*state.Run, error) {
					result, err = eng.PromptSandbox(context.Background(), args[0], opts)
					if result == nil {
						return nil, err
					}
					runRecord, getErr := app.DB.GetRun(context.Background(), result.RunID)
					if err != nil {
						return runRecord, err
					}
					return runRecord, getErr
				})
			} else {
				result, err = eng.PromptSandbox(context.Background(), args[0], opts)
			}
			if mode == streaming.ModeJSONL {
				runID := ""
				if result != nil {
					runID = result.RunID
				}
				_ = streaming.WriteRecord(os.Stdout, streaming.ResultRecord(runID, result, err))
			}
			if err != nil {
				return err
			}
			if mode == streaming.ModeJSONL {
				return nil
			}
			return app.Printer.Success(result)
		},
	}
	promptCmd.Flags().StringVar(&promptFile, "file", "", "read the prompt from a file")
	promptCmd.Flags().StringVar(&promptModel, "model", "", "Codex model ID (defaults to the run model)")
	promptCmd.Flags().StringVar(&promptReasoning, "reasoning-effort", "", "Codex reasoning effort: low|medium|high|xhigh")
	promptCmd.Flags().StringVar(&promptStream, "stream", "pretty", "stream mode: pretty|ui|events|jsonl|raw|off")
	promptCmd.Flags().BoolVar(&promptNoStream, "no-stream", false, "disable live Codex output")
	promptCmd.Flags().BoolVar(&promptEventsOnly, "events-only", false, "stream compact events instead of agent output")
	promptCmd.Flags().BoolVar(&promptNoPush, "no-push", false, "commit locally without pushing the integration branch")
	cmd.AddCommand(promptCmd)

	destroy := &cobra.Command{
		Use: "destroy [sandbox_id]", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("sandbox.destroy", map[string]any{"sandbox_id": strings.Join(args, ""), "run_id": runID})
			}
			if err := app.requireYes("destroy sandbox"); err != nil {
				return err
			}
			var targets []state.Sandbox
			var err error
			if runID != "" {
				targets, err = app.DB.ListSandboxesForRun(context.Background(), runID)
				if err != nil {
					return err
				}
			} else {
				if len(args) != 1 {
					return fmt.Errorf("sandbox_id or --run is required")
				}
				s, getErr := app.DB.GetSandbox(context.Background(), args[0])
				if getErr != nil {
					return getErr
				}
				targets = []state.Sandbox{*s}
			}
			var destroyed []string
			for i := range targets {
				s := &targets[i]
				if err := retention.Destroy(context.Background(), app.DB, app.Root, s, "manual"); err != nil {
					return err
				}
				destroyed = append(destroyed, s.ID)
			}
			return app.Printer.Success(map[string]any{"destroyed": destroyed})
		},
	}
	destroy.Flags().StringVar(&runID, "run", "", "destroy every sandbox for a run")
	cmd.AddCommand(destroy)

	retain := &cobra.Command{
		Use: "retain <sandbox_id>", Args: cobra.ExactArgs(1), Short: "Extend a sandbox preview lease", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("sandbox.retain", map[string]any{"sandbox_id": args[0], "for": retainFor})
			}
			d, err := retention.ParseDuration(retainFor)
			if err != nil {
				return err
			}
			s, err := app.DB.GetSandbox(context.Background(), args[0])
			if err != nil {
				return err
			}
			if err := retention.Retain(context.Background(), app.DB, s, d); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"sandbox_id": s.ID, "retained_until": s.RetainedUntil})
		},
	}
	retain.Flags().StringVar(&retainFor, "for", "24h", "retention duration, for example 24h or 7d")
	cmd.AddCommand(retain)

	gc := &cobra.Command{
		Use: "gc", Short: "Remove expired Vessica sandboxes", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspaceWithoutGC(); err != nil {
				return err
			}
			defer app.closeDB()
			var age time.Duration
			var err error
			if olderThan != "" {
				age, err = retention.ParseDuration(olderThan)
				if err != nil {
					return err
				}
			}
			result, err := retention.GC(context.Background(), app.DB, app.Root, retention.GCOptions{DryRun: app.Flags.DryRun, OlderThan: age})
			if err != nil {
				return err
			}
			return app.Printer.Success(result)
		},
	}
	gc.Flags().StringVar(&olderThan, "older-than", "", "also remove sandboxes older than this duration")
	cmd.AddCommand(gc)
	return cmd
}

func itoa(n int) string {
	return fmtInt(n)
}

func fmtInt(n int) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
