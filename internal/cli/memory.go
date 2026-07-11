package cli

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/id"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func (a *App) knowledgeKey(prefix string) string {
	if a.Flags.IdempotencyKey != "" {
		return a.Flags.IdempotencyKey
	}
	return prefix + ":" + id.New("idem")
}
func (a *App) knowledgeAndScope(ctx context.Context) (*knowledgegateway.Gateway, ks.Scope, error) {
	g, err := a.openKnowledge(ctx)
	if err != nil {
		return nil, ks.Scope{}, err
	}
	scope, err := g.EnsureRepositoryScope(ctx, knowledgegateway.CanonicalRepository(a.Config.Repo.Remote, a.Root), a.Config.Repo.Remote)
	if err != nil {
		g.Close()
		return nil, scope, err
	}
	return g, scope, nil
}

func newMemoryCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "Manage durable knowledge memories"}
	var title, body, typ, confidenceSource string
	var importance, confidence float64
	var stdin bool
	cmd.AddCommand(&cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(); err != nil {
			return err
		}
		defer app.closeDB()
		g, scope, err := app.knowledgeAndScope(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		list, err := g.SearchMemories(cmd.Context(), "", []string{scope.ID})
		if err != nil {
			return err
		}
		return app.Printer.Success(list)
	}})
	cmd.AddCommand(&cobra.Command{Use: "view <memory_id>", Aliases: []string{"get"}, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(); err != nil {
			return err
		}
		defer app.closeDB()
		g, err := app.openKnowledge(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		v, err := g.GetMemory(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return app.Printer.Success(v)
	}})
	add := &cobra.Command{Use: "add", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(); err != nil {
			return err
		}
		defer app.closeDB()
		b := body
		if stdin || b == "" {
			raw, _ := io.ReadAll(os.Stdin)
			if len(raw) > 0 {
				b = string(raw)
			}
		}
		if strings.TrimSpace(b) == "" {
			return app.Printer.Fail("missing_body", "body required via --body or --stdin", "")
		}
		if title == "" {
			title = firstLine(b)
		}
		g, scope, err := app.knowledgeAndScope(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		v := ks.Memory{ScopeID: scope.ID, Type: typ, Title: title, Content: b, Importance: importance, Confidence: confidence, ConfidenceSource: confidenceSource}
		if app.Flags.DryRun {
			return app.dryRun("memory.add", v)
		}
		if app.Flags.JSON && !app.Flags.Yes {
			return app.requireYes("create the memory")
		}
		got, err := g.CreateMemory(cmd.Context(), app.knowledgeKey("memory.add"), v)
		if err != nil {
			return err
		}
		return app.Printer.Success(got)
	}}
	add.Flags().StringVar(&title, "title", "", "title")
	add.Flags().StringVar(&body, "body", "", "body")
	add.Flags().StringVar(&typ, "type", "fact", "instruction|fact|decision|episode")
	add.Flags().Float64Var(&importance, "importance", .5, "importance 0..1")
	add.Flags().Float64Var(&confidence, "confidence", .5, "confidence 0..1")
	add.Flags().StringVar(&confidenceSource, "confidence-source", "agent_inferred", "human_confirmed|agent_inferred|imported|external_system|observed")
	add.Flags().BoolVar(&stdin, "stdin", false, "read body from stdin")
	cmd.AddCommand(add)
	update := &cobra.Command{Use: "update <memory_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(); err != nil {
			return err
		}
		defer app.closeDB()
		g, err := app.openKnowledge(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		cur, err := g.GetMemory(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if title != "" {
			cur.Title = title
		}
		if body != "" {
			cur.Content = body
		}
		if app.Flags.DryRun {
			return app.dryRun("memory.update", cur)
		}
		if app.Flags.JSON && !app.Flags.Yes {
			return app.requireYes("version the memory")
		}
		got, err := g.VersionMemory(cmd.Context(), app.knowledgeKey("memory.update"), cur)
		if err != nil {
			return err
		}
		return app.Printer.Success(got)
	}}
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	cmd.AddCommand(update)
	for _, state := range []string{"superseded", "archived"} {
		st := state
		use := map[string]string{"superseded": "supersede", "archived": "archive"}[st]
		cmd.AddCommand(&cobra.Command{Use: use + " <memory_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(); err != nil {
				return err
			}
			defer app.closeDB()
			g, err := app.openKnowledge(cmd.Context())
			if err != nil {
				return err
			}
			defer g.Close()
			if app.Flags.DryRun {
				return app.dryRun("memory."+use, map[string]string{"id": args[0]})
			}
			if err := app.requireYes(use + " memory"); err != nil {
				return err
			}
			got, err := g.SetMemoryState(cmd.Context(), app.knowledgeKey("memory."+use), args[0], st)
			if err != nil {
				return err
			}
			return app.Printer.Success(got)
		}})
	}
	search := &cobra.Command{Use: "search <query>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(); err != nil {
			return err
		}
		defer app.closeDB()
		g, scope, err := app.knowledgeAndScope(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		list, err := g.SearchMemories(cmd.Context(), args[0], []string{scope.ID})
		if err != nil {
			return err
		}
		return app.Printer.Success(list)
	}}
	cmd.AddCommand(search)
	_ = strconv.Itoa
	return cmd
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		return s[:80]
	}
	return s
}
