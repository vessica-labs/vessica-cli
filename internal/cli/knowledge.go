package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vessica-labs/vessica-cli/internal/auth"
	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/knowledgegateway"
	ks "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func newKnowledgeCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "knowledge", Short: "Inspect and manage the durable Vessica knowledge layer"}
	var query, outFile, inFile, endpoint, token string
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
	promote := &cobra.Command{Use: "promote", Short: "Promote local knowledge to an existing hosted service", Hidden: true, RunE: func(cmd *cobra.Command, args []string) error {
		if endpoint == "" || token == "" {
			return app.Printer.Fail("missing_hosted_knowledge", "--endpoint and --token required", "")
		}
		if err := app.loadWorkspace(cmd.Context()); err != nil {
			return err
		}
		defer app.closeDB()
		if app.Config.Knowledge.Mode == "hosted" {
			return app.Printer.Fail("already_hosted", "knowledge authority is already hosted", "")
		}
		lockPath := filepath.Join(app.Root, ".vessica", "state", "knowledge.promote.lock")
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return app.Printer.Fail("promotion_locked", "another knowledge promotion is active", "remove the lock only after confirming no promotion is running")
		}
		_ = lock.Close()
		defer os.Remove(lockPath)
		ws, err := app.DB.GetWorkspace(cmd.Context())
		if err != nil {
			return err
		}
		local, err := knowledgegateway.OpenForPromotion(app.Root, app.Config, ws.ID)
		if err != nil {
			return err
		}
		snap, err := local.Export(cmd.Context())
		local.Close()
		if err != nil {
			return err
		}
		if app.Flags.DryRun {
			return app.dryRun("knowledge.promote", map[string]any{"endpoint": endpoint, "counts": snap.Counts, "checksum": snap.Checksum})
		}
		if err := app.requireYes("promote local knowledge to hosted authority"); err != nil {
			return err
		}
		if err := auth.Login("knowledge", token, "hosted knowledge"); err != nil {
			return err
		}
		if err := auth.Login("knowledge-export", token, "hosted knowledge export"); err != nil {
			return err
		}
		next := app.Config
		next.Knowledge.Mode = "hosted"
		next.Knowledge.Endpoint = endpoint
		next.Knowledge.WorkspaceID = snap.WorkspaceID
		remote, err := knowledgegateway.Open(app.Root, next, snap.WorkspaceID)
		if err != nil {
			return err
		}
		defer remote.Close()
		if err := remote.Import(cmd.Context(), snap); err != nil {
			return err
		}
		check, err := remote.Export(cmd.Context())
		if err != nil {
			return err
		}
		if err := verifyKnowledgePromotion(snap, check); err != nil {
			return app.Printer.Fail("promotion_verification_failed", err.Error(), "SQLite remains authoritative; rerun promotion safely")
		}
		if _, err := remote.Context(cmd.Context(), ks.ContextRequest{Query: "workspace knowledge", ArtifactSelectors: []ks.ArtifactSelector{{Status: "active"}}, TokenBudget: 1000}); err != nil {
			return app.Printer.Fail("promotion_context_verification_failed", err.Error(), "SQLite remains authoritative")
		}
		backup := filepath.Join(app.Root, ".vessica", "state", "knowledge-"+snap.HighWatermark+".readonly.db")
		localPath := app.Config.Knowledge.LocalPath
		if localPath == "" {
			localPath = filepath.Join(".vessica", "state", "knowledge.db")
		}
		if !filepath.IsAbs(localPath) {
			localPath = filepath.Join(app.Root, localPath)
		}
		if err := copyReadOnly(localPath, backup); err != nil {
			return err
		}
		if err := config.Save(app.Root, next); err != nil {
			return err
		}
		return app.Printer.Success(map[string]any{"promoted": true, "endpoint": endpoint, "workspace_id": snap.WorkspaceID, "counts": snap.Counts, "checksum": snap.Checksum, "backup": backup})
	}}
	promote.Flags().StringVar(&endpoint, "endpoint", "", "hosted knowledge API endpoint")
	promote.Flags().StringVar(&token, "token", "", "hosted knowledge bearer token")
	cmd.AddCommand(promote)
	cmd.AddCommand(newKnowledgeEmbeddingsCmd(app))
	return cmd
}

func verifyKnowledgePromotion(want, got ks.Snapshot) error {
	if want.WorkspaceID != got.WorkspaceID || want.HighWatermark != got.HighWatermark {
		return fmt.Errorf("remote workspace or event high-watermark differs from local snapshot")
	}
	for key, count := range want.Counts {
		if got.Counts[key] != count {
			return fmt.Errorf("remote %s count is %d; expected %d", key, got.Counts[key], count)
		}
	}
	hashes := map[string]string{}
	for _, a := range got.Artifacts {
		hashes[fmt.Sprintf("%s:%d", a.ID, a.Version)] = a.ContentHash
	}
	for _, a := range want.Artifacts {
		if hashes[fmt.Sprintf("%s:%d", a.ID, a.Version)] != a.ContentHash {
			return fmt.Errorf("artifact %s version %d hash differs", a.ID, a.Version)
		}
	}
	memories := map[string]string{}
	for _, m := range got.Memories {
		memories[fmt.Sprintf("%s:%d", m.ID, m.Version)] = m.Type + "\x00" + m.Content
	}
	for _, m := range want.Memories {
		if memories[fmt.Sprintf("%s:%d", m.ID, m.Version)] != m.Type+"\x00"+m.Content {
			return fmt.Errorf("memory %s version %d differs", m.ID, m.Version)
		}
	}
	entities := map[string]int{}
	for _, e := range got.Entities {
		entities[e.ID] = e.Version
	}
	for _, e := range want.Entities {
		if entities[e.ID] < e.Version {
			return fmt.Errorf("entity %s version is missing", e.ID)
		}
	}
	relationships := map[string]string{}
	for _, r := range got.Relationships {
		relationships[fmt.Sprintf("%s:%d", r.ID, r.Version)] = r.FromID + "\x00" + r.Predicate + "\x00" + r.ToID
	}
	for _, r := range want.Relationships {
		if relationships[fmt.Sprintf("%s:%d", r.ID, r.Version)] != r.FromID+"\x00"+r.Predicate+"\x00"+r.ToID {
			return fmt.Errorf("relationship %s version %d differs", r.ID, r.Version)
		}
	}
	events := map[string]bool{}
	for _, e := range got.Events {
		events[e.ID] = true
	}
	for _, e := range want.Events {
		if !events[e.ID] {
			return fmt.Errorf("event %s is missing", e.ID)
		}
	}
	return nil
}

func copyReadOnly(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
