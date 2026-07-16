package cli

import (
	"os"

	"github.com/spf13/cobra"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func newArtifactCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "artifact", Short: "Manage authoritative knowledge artifacts"}
	var typ, title, body, bodyFile, status string
	list := &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		g, scope, err := app.knowledgeAndScope(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		items, err := g.ListArtifacts(cmd.Context(), typ, status, []string{scope.ID})
		if err != nil {
			return err
		}
		return app.Printer.Success(items)
	}}
	list.Flags().StringVar(&typ, "type", "", "artifact type")
	list.Flags().StringVar(&status, "status", "", "draft|active|superseded|archived")
	cmd.AddCommand(list)
	cmd.AddCommand(&cobra.Command{Use: "view <artifact_id>", Aliases: []string{"get"}, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		g, err := app.openKnowledge(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		v, err := g.GetArtifact(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return app.Printer.Success(v)
	}})
	add := &cobra.Command{Use: "add", Aliases: []string{"create"}, RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if typ == "" || title == "" {
			return app.Printer.Fail("missing_fields", "--type and --title required", "")
		}
		b := body
		if bodyFile != "" {
			raw, err := os.ReadFile(bodyFile)
			if err != nil {
				return err
			}
			b = string(raw)
		}
		g, scope, err := app.knowledgeAndScope(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		v := ks.Artifact{ScopeID: scope.ID, Type: typ, Title: title, Status: "draft", Content: b}
		if app.Flags.DryRun {
			return app.dryRun("artifact.add", v)
		}
		if app.Flags.JSON && !app.Flags.Yes {
			return app.requireYes("create the artifact")
		}
		got, err := g.CreateArtifact(cmd.Context(), app.knowledgeKey("artifact.add"), v)
		if err != nil {
			return err
		}
		return app.Printer.Success(got)
	}}
	add.Flags().StringVar(&typ, "type", "", "artifact type")
	add.Flags().StringVar(&title, "title", "", "title")
	add.Flags().StringVar(&body, "body", "", "body")
	add.Flags().StringVar(&bodyFile, "body-file", "", "body file")
	cmd.AddCommand(add)
	update := &cobra.Command{Use: "update <artifact_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		g, err := app.openKnowledge(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		cur, err := g.GetArtifact(cmd.Context(), args[0])
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
			return app.dryRun("artifact.update", cur)
		}
		if app.Flags.JSON && !app.Flags.Yes {
			return app.requireYes("version the artifact")
		}
		got, err := g.VersionArtifact(cmd.Context(), app.knowledgeKey("artifact.update"), cur)
		if err != nil {
			return err
		}
		return app.Printer.Success(got)
	}}
	update.Flags().StringVar(&title, "title", "", "title")
	update.Flags().StringVar(&body, "body", "", "body")
	cmd.AddCommand(update)
	for _, st := range []string{"active", "superseded"} {
		state := st
		use := map[string]string{"active": "activate", "superseded": "supersede"}[state]
		cmd.AddCommand(&cobra.Command{Use: use + " <artifact_id>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			g, err := app.openKnowledge(cmd.Context())
			if err != nil {
				return err
			}
			defer g.Close()
			if app.Flags.DryRun {
				return app.dryRun("artifact."+use, map[string]string{"id": args[0]})
			}
			if err := app.requireYes(use + " artifact"); err != nil {
				return err
			}
			got, err := g.SetArtifactStatus(cmd.Context(), app.knowledgeKey("artifact."+use), args[0], state)
			if err != nil {
				return err
			}
			return app.Printer.Success(got)
		}})
	}
	return cmd
}
