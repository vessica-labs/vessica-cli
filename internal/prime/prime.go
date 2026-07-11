package prime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/config"
	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type Request struct {
	For      string
	EpicID   string
	TicketID string
	Minimal  bool
}

type Response struct {
	Product      string         `json:"product"`
	Workspace    string         `json:"workspace"`
	Harness      map[string]any `json:"harness"`
	Commands     []string       `json:"commands"`
	ReadyTickets []state.Ticket `json:"ready_tickets,omitempty"`
	Epic         *state.Epic    `json:"epic,omitempty"`
	Ticket       *state.Ticket  `json:"ticket,omitempty"`
	Rules        []string       `json:"rules"`
	Text         string         `json:"text,omitempty"`
	Knowledge    any            `json:"knowledge,omitempty"`
}

func Build(ctx context.Context, db *state.DB, root string, req Request) (*Response, error) {
	ws, err := db.GetWorkspace(ctx)
	if err != nil {
		return nil, err
	}
	st, _ := harness.StatusOf(root)
	engineManaged := os.Getenv("VES_ENGINE_MANAGED_RUN") == "1"
	resp := &Response{
		Product:   "vessica",
		Workspace: ws.ID,
		Harness: map[string]any{
			"drift":   st.DriftStatus,
			"missing": st.MissingFiles,
		},
		Commands: primeCommands(engineManaged),
		Rules:    primeRules(engineManaged),
	}
	if req.EpicID != "" {
		epic, err := db.GetEpic(ctx, req.EpicID)
		if err != nil {
			return nil, err
		}
		resp.Epic = epic
		ready, _ := db.ReadyTickets(ctx, req.EpicID)
		resp.ReadyTickets = ready
	} else {
		ready, _ := db.ReadyTickets(ctx, "")
		if len(ready) > 10 {
			ready = ready[:10]
		}
		resp.ReadyTickets = ready
	}
	if req.TicketID != "" {
		t, err := db.GetTicket(ctx, req.TicketID)
		if err != nil {
			return nil, err
		}
		resp.Ticket = t
	}
	if !req.Minimal {
		if b, err := os.ReadFile(filepath.Join(root, "AGENTS.md")); err == nil {
			resp.Text = truncate(string(b), 1500)
		}
	}
	if req.For != "" {
		resp.Rules = append(resp.Rules, fmt.Sprintf("Optimized for runner: %s", req.For))
	}
	_ = config.DirName
	return resp, nil
}

func primeCommands(engineManaged bool) []string {
	if engineManaged {
		return []string{
			"ves prime --json",
			"git status --short",
			"git diff --check",
			"<project test command>",
			"<project build command>",
		}
	}
	return []string{
		"ves prime --json",
		"ves ticket ready --json",
		"ves ticket claim --next --epic <epic_id> --agent <agent_id> --lease 45m --json",
		"ves memory add --stdin --json",
		"ves ticket close <ticket_id> --agent <agent_id> --evidence <receipt_id> --json",
	}
}

func primeRules(engineManaged bool) []string {
	common := []string{
		"Use ves for context.",
		"Do not invent ad hoc TODO files when Vessica state exists.",
		"Follow AGENTS.md and harness docs.",
	}
	if engineManaged {
		return append([]string{
			"Engine-managed run: Vessica already owns claim, close, heartbeat, memory, receipts, commits, and merges.",
			"Do not run ves ticket claim, ves ticket close, ves ticket heartbeat, ves ticket release, or ves memory add.",
			"Implement the change, run local checks, and return a concise evidence summary.",
		}, common...)
	}
	return append([]string{
		"Standalone/manual mode: claim before coding; close only with evidence.",
	}, common...)
}

func FormatHuman(r *Response) string {
	var b strings.Builder
	b.WriteString("Vessica prime\n")
	b.WriteString(fmt.Sprintf("workspace: %s\n", r.Workspace))
	b.WriteString(fmt.Sprintf("harness drift: %v\n", r.Harness["drift"]))
	b.WriteString("\nCommands:\n")
	for _, c := range r.Commands {
		b.WriteString("  " + c + "\n")
	}
	if len(r.ReadyTickets) > 0 {
		b.WriteString("\nReady tickets:\n")
		for _, t := range r.ReadyTickets {
			b.WriteString(fmt.Sprintf("  %s %s\n", t.ID, t.Title))
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
