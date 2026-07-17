package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/run"
	"github.com/vessica-labs/vessica-cli/internal/state"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func newEpicCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "epic", Short: "Manage epics"}
	var title, body, bodyFile, specFile, status string

	add := &cobra.Command{
		Use: "add", Short: "Create an epic",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if specFile != "" {
				spec, err := readEpicSpec(app.Root, specFile)
				if err != nil {
					return app.Printer.Fail("invalid_spec", err.Error(), "fix the spec and retry ves epic draft --spec-file <file> --json")
				}
				if app.Flags.DryRun {
					return app.dryRun("epic.add", spec)
				}
				if app.Flags.JSON && !app.Flags.Yes {
					return app.requireYes("create the epic and tickets")
				}
				if replayed, err := app.idempotencyReplay(cmd.Context()); err != nil || replayed {
					return err
				}
				if config.IsHostedAttachment(app.Config) {
					created, err := app.publishEpic(cmd.Context(), "", spec)
					if err != nil {
						return app.Printer.Fail("hosted_create_failed", err.Error(), "retry ves epic add with the same idempotency key")
					}
					return app.Printer.Success(created)
				}
				created, err := app.DB.CreateEpicFromSpec(cmd.Context(), spec)
				if err != nil {
					return err
				}
				data := map[string]any{"epic": created.Epic, "tickets": created.Tickets, "next_actions": []string{"ves run epic " + created.Epic.ID + " --dry-run --json", "ves run epic " + created.Epic.ID + " --yes --idempotency-key <key> --json"}}
				if app.Config.Hosted.ControlPlaneURL != "" {
					published, publishErr := app.publishEpic(cmd.Context(), created.Epic.ID, spec)
					if publishErr != nil {
						return app.Printer.Fail("hosted_publish_failed", publishErr.Error(), "retry ves epic publish "+created.Epic.ID+" --yes --idempotency-key "+app.Flags.IdempotencyKey+" --json")
					}
					data["hosted"] = published
				}
				app.recordEpicKnowledge(cmd.Context(), created.Epic, created.Tickets)
				if err := app.idempotencyStore(cmd.Context(), data); err != nil {
					return err
				}
				return app.Printer.Success(data)
			}
			b := body
			if bodyFile != "" {
				raw, err := os.ReadFile(bodyFile)
				if err != nil {
					return err
				}
				b = string(raw)
			}
			if b == "" && !isTTY() {
				raw, _ := os.ReadFile("/dev/stdin")
				b = string(raw)
			}
			if title == "" {
				return app.Printer.Fail("missing_title", "--title required", "")
			}
			if app.Flags.DryRun {
				return app.dryRun("epic.add", map[string]any{"title": title, "body": b})
			}
			if app.Flags.JSON && !app.Flags.Yes {
				return app.requireYes("create the epic")
			}
			if replayed, err := app.idempotencyReplay(cmd.Context()); err != nil || replayed {
				return err
			}
			if config.IsHostedAttachment(app.Config) {
				created, err := app.publishEpic(cmd.Context(), "", state.EpicSpec{Title: title, Body: b})
				if err != nil {
					return app.Printer.Fail("hosted_create_failed", err.Error(), "retry ves epic add with the same idempotency key")
				}
				return app.Printer.Success(created)
			}
			e, err := app.DB.CreateEpic(cmd.Context(), title, b)
			if err != nil {
				return err
			}
			app.recordEpicKnowledge(cmd.Context(), e, nil)
			if err := app.idempotencyStore(cmd.Context(), e); err != nil {
				return err
			}
			return app.Printer.Success(e)
		},
	}
	add.Flags().StringVar(&title, "title", "", "epic title")
	add.Flags().StringVar(&body, "body", "", "epic body")
	add.Flags().StringVar(&bodyFile, "body-file", "", "read body from file")
	add.Flags().StringVar(&specFile, "spec-file", "", "create an epic and ticket graph from JSON")
	cmd.AddCommand(add)

	draft := &cobra.Command{
		Use: "draft", Short: "Validate an epic and ticket graph without persisting it",
		RunE: func(cmd *cobra.Command, args []string) error {
			var spec state.EpicSpec
			var err error
			if specFile != "" {
				spec, err = readEpicSpec(app.Root, specFile)
			} else {
				spec = state.EpicSpec{Title: title, Body: body}
				err = state.ValidateEpicSpec(spec)
			}
			if err != nil {
				return app.Printer.Fail("invalid_spec", err.Error(), "provide a valid title and acyclic ticket dependencies")
			}
			return app.Printer.Success(map[string]any{"valid": true, "spec": spec, "ticket_count": len(spec.Tickets), "next_actions": []string{"ves epic add --spec-file <file> --yes --idempotency-key <key> --json"}})
		},
	}
	draft.Flags().StringVar(&title, "title", "", "epic title")
	draft.Flags().StringVar(&body, "body", "", "epic body")
	draft.Flags().StringVar(&specFile, "spec-file", "", "read the proposed epic and tickets from JSON")
	cmd.AddCommand(draft)

	cmd.AddCommand(&cobra.Command{
		Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				list, err := app.listHostedEpics(cmd.Context())
				if err != nil {
					return err
				}
				if !app.Flags.JSON {
					return app.Printer.Success(formatEpics(list))
				}
				return app.Printer.Success(list)
			}
			list, err := app.DB.ListEpics(cmd.Context())
			if err != nil {
				return err
			}
			if !app.Flags.JSON {
				return app.Printer.Success(formatEpics(list))
			}
			return app.Printer.Success(list)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "view <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				e, err := app.getHostedEpic(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return app.Printer.Success(e)
			}
			e, err := app.DB.GetEpic(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return app.Printer.Success(e)
		},
	})
	update := &cobra.Command{
		Use: "update <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if app.Flags.DryRun {
				return app.dryRun("epic.update", map[string]any{"id": args[0], "title": title, "body": body, "status": status})
			}
			if app.Flags.JSON && !app.Flags.Yes {
				return app.requireYes("update the epic")
			}
			if replayed, replayErr := app.idempotencyReplay(cmd.Context()); replayErr != nil || replayed {
				return replayErr
			}
			e, err := app.DB.UpdateEpic(cmd.Context(), args[0], title, body, status)
			if err != nil {
				return err
			}
			if err := app.idempotencyStore(cmd.Context(), e); err != nil {
				return err
			}
			return app.Printer.Success(e)
		},
	}
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	update.Flags().StringVar(&status, "status", "", "status")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use: "delete <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if err := app.requireYes("delete epic"); err != nil {
				return err
			}
			if app.Flags.DryRun {
				return app.dryRun("epic.delete", map[string]any{"id": args[0]})
			}
			if err := app.DB.DeleteEpic(cmd.Context(), args[0]); err != nil {
				return err
			}
			return app.Printer.Success(map[string]string{"deleted": args[0]})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "plan <epic_id>", Args: cobra.ExactArgs(1), Short: "Plan epic through ticketize",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			eng := &run.Engine{DB: app.DB, Root: app.Root, Config: app.Config, Stream: !app.Flags.JSON}
			r, err := eng.RunEpic(cmd.Context(), run.Options{
				EpicID:    args[0],
				StopAfter: "ticketize",
				Stream:    !app.Flags.JSON,
			})
			if err != nil {
				return err
			}
			return app.Printer.Success(r)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "status <epic_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			if config.IsHostedAttachment(app.Config) {
				status, err := app.getHostedEpicStatus(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return app.Printer.Success(status)
			}
			e, err := app.DB.GetEpic(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			tickets, _ := app.DB.ListTickets(cmd.Context(), e.ID)
			ready, _ := app.DB.ReadyTickets(cmd.Context(), e.ID)
			return app.Printer.Success(map[string]any{"epic": e, "tickets": len(tickets), "ready": len(ready)})
		},
	})
	_ = strings.TrimSpace
	return cmd
}

func (app *App) recordEpicKnowledge(ctx context.Context, epic *state.Epic, tickets []*state.Ticket) {
	if epic == nil {
		return
	}
	g, scope, err := app.knowledgeAndScope(ctx)
	if err != nil {
		return
	}
	defer g.Close()
	refs := []ks.ExternalRef{{System: "vessica.epic", ID: epic.ID}}
	if epic.ExternalID != "" {
		refs = append(refs, ks.ExternalRef{System: "linear.issue", ID: epic.ExternalID})
	}
	for _, ticket := range tickets {
		if ticket == nil {
			continue
		}
		refs = append(refs, ks.ExternalRef{System: "vessica.ticket", ID: ticket.ID})
		if ticket.ExternalID != "" {
			refs = append(refs, ks.ExternalRef{System: "linear.issue", ID: ticket.ExternalID})
		}
	}
	_, _ = g.Workflow(ctx, "epic:"+epic.ID+":accepted", ks.WorkflowEvent{ID: "epic:" + epic.ID + ":accepted", RepositoryScopeID: scope.ID, Type: "epic.accepted", Summary: "Epic accepted: " + epic.Title, OccurredAt: time.Now().UTC(), Actor: ks.Actor{ID: "ves-cli", Type: "user"}, References: refs})
}

func readEpicSpec(root, path string) (state.EpicSpec, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return state.EpicSpec{}, err
	}
	var spec state.EpicSpec
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return state.EpicSpec{}, fmt.Errorf("decode epic spec: %w", err)
	}
	if err := state.ValidateEpicSpec(spec); err != nil {
		return state.EpicSpec{}, err
	}
	return spec, nil
}

func (a *App) publishEpic(ctx context.Context, localEpicID string, spec state.EpicSpec) (map[string]any, error) {
	if a.Config.Hosted.ControlPlaneURL == "" {
		return nil, fmt.Errorf("hosted control plane is not configured")
	}
	secrets, err := loadRailwaySecrets(a.Root)
	if err != nil {
		return nil, err
	}
	key := a.Flags.IdempotencyKey
	if key == "" {
		raw, _ := json.Marshal(spec)
		digest := sha256.Sum256(append([]byte(a.Config.Attachment.RepositoryID+":"), raw...))
		key = fmt.Sprintf("epic-%x", digest[:12])
	}
	var result map[string]any
	endpoint := strings.TrimRight(a.Config.Hosted.ControlPlaneURL, "/") + "/api/v1/epics"
	body := map[string]any{"spec": spec, "repository_id": a.Config.Attachment.RepositoryID}
	if localEpicID != "" {
		body["source_epic_id"] = localEpicID
	}
	if err := hostedRequestWithKey(ctx, http.MethodPost, endpoint, secrets.APIToken, key, body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
