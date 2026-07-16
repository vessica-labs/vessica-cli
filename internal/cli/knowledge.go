package cli

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func newKnowledgeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "knowledge", Short: "Inspect and manage the durable Vessica knowledge layer"}
	var query, outFile, inFile string
	var budget int
	cmd.AddCommand(&cobra.Command{Use: "status", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		g, err := app.openKnowledge(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		probe, err := g.Context(cmd.Context(), ks.ContextRequest{Query: "status", TokenBudget: 1})
		if err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"mode": g.Mode(), "workspace_id": g.Workspace(), "endpoint": app.Config.Knowledge.Endpoint, "local_path": app.Config.Knowledge.LocalPath, "retrieval_mode": probe.RetrievalMode, "index_fresh": probe.IndexFresh, "embedding_model": probe.EmbeddingModel})
	}})
	contextCmd := &cobra.Command{Use: "context", RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		g, scope, err := app.knowledgeAndScope(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		result, err := g.Context(cmd.Context(), ks.ContextRequest{Query: query, ScopeIDs: []string{scope.ID}, ArtifactSelectors: []ks.ArtifactSelector{{Status: "active"}}, TokenBudget: budget})
		if err != nil {
			return err
		}
		return app.Printer.Success(result)
	}}
	contextCmd.Flags().StringVar(&query, "query", "", "task or retrieval query")
	contextCmd.Flags().IntVar(&budget, "token-budget", 12000, "maximum assembled context tokens")
	cmd.AddCommand(contextCmd)
	exportCmd := &cobra.Command{Use: "export", Hidden: true, RunE: func(cmd *cobra.Command, args []string) error {
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		g, err := app.openKnowledge(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		snap, err := g.Export(cmd.Context())
		if err != nil {
			return err
		}
		if outFile != "" {
			raw, _ := json.MarshalIndent(snap, "", "  ")
			if err := os.WriteFile(outFile, append(raw, '\n'), 0o600); err != nil {
				return err
			}
			return app.Printer.Success(map[string]any{"file": outFile, "checksum": snap.Checksum, "counts": snap.Counts})
		}
		return app.Printer.Success(snap)
	}}
	exportCmd.Flags().StringVar(&outFile, "file", "", "write snapshot JSON to file")
	cmd.AddCommand(exportCmd)
	importCmd := &cobra.Command{Use: "import", Hidden: true, RunE: func(cmd *cobra.Command, args []string) error {
		if inFile == "" {
			return app.Printer.Fail("missing_file", "--file required", "")
		}
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		raw, err := os.ReadFile(inFile)
		if err != nil {
			return err
		}
		var snap ks.Snapshot
		if err := json.Unmarshal(raw, &snap); err != nil {
			return err
		}
		g, err := app.openKnowledge(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		if app.Flags.DryRun {
			return app.dryRun("knowledge.import", map[string]any{"checksum": snap.Checksum, "counts": snap.Counts})
		}
		if err := app.requireYes("import the knowledge snapshot"); err != nil {
			return err
		}
		if err := g.Import(cmd.Context(), snap); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"imported": true, "checksum": snap.Checksum, "counts": snap.Counts})
	}}
	importCmd.Flags().StringVar(&inFile, "file", "", "snapshot JSON file")
	cmd.AddCommand(importCmd)
	cmd.AddCommand(newKnowledgeEmbeddingsCmd(app))
	return cmd
}

func newEntityCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "entity", Short: "Create and resolve knowledge entities"}
	var typ, name, alias, externalSystem, externalID, externalURL string
	create := &cobra.Command{Use: "create", RunE: func(cmd *cobra.Command, args []string) error {
		if typ == "" || name == "" {
			return app.Printer.Fail("missing_fields", "--type and --name required", "")
		}
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		g, scope, err := app.knowledgeAndScope(cmd.Context())
		if err != nil {
			return err
		}
		defer g.Close()
		v := ks.Entity{ScopeID: scope.ID, Type: typ, DisplayName: name}
		if alias != "" {
			v.Aliases = []string{alias}
		}
		if externalSystem != "" && externalID != "" {
			v.ExternalRefs = []ks.ExternalRef{{System: externalSystem, ID: externalID, URL: externalURL}}
		}
		if app.Flags.DryRun {
			return app.dryRun("entity.create", v)
		}
		if app.Flags.JSON && !app.Flags.Yes {
			return app.requireYes("create the entity")
		}
		got, err := g.CreateEntity(cmd.Context(), app.knowledgeKey("entity.create"), v)
		if err != nil {
			return err
		}
		return app.Printer.Success(got)
	}}
	create.Flags().StringVar(&typ, "type", "", "entity type")
	create.Flags().StringVar(&name, "name", "", "display name")
	create.Flags().StringVar(&alias, "alias", "", "alias")
	create.Flags().StringVar(&externalSystem, "external-system", "", "external system")
	create.Flags().StringVar(&externalID, "external-id", "", "external identifier")
	create.Flags().StringVar(&externalURL, "external-url", "", "external URL")
	cmd.AddCommand(create)
	for _, verb := range []string{"resolve", "search"} {
		v := verb
		cmd.AddCommand(&cobra.Command{Use: v + " <query>", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.loadWorkspace(cmd.Context()); err != nil {
				return err
			}
			defer app.closeDB()
			g, scope, err := app.knowledgeAndScope(cmd.Context())
			if err != nil {
				return err
			}
			defer g.Close()
			items, err := g.ResolveEntities(cmd.Context(), args[0], []string{scope.ID})
			if err != nil {
				return err
			}
			return app.Printer.Success(items)
		}})
	}
	return cmd
}
