package cli

import (
	"fmt"
	"strings"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func formatArtifacts(arts []state.Artifact) string {
	if len(arts) == 0 {
		return "(no artifacts)"
	}
	var b strings.Builder
	for _, a := range arts {
		fmt.Fprintf(&b, "- %s  %s  [%s]  %s\n", a.ID, a.Type, a.Status, a.Title)
		if a.SourceRunID != "" {
			fmt.Fprintf(&b, "  run: %s\n", a.SourceRunID)
		}
		if a.Version > 0 {
			fmt.Fprintf(&b, "  version: %d\n", a.Version)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatEpics(epics []state.Epic) string {
	if len(epics) == 0 {
		return "(no epics)"
	}
	var b strings.Builder
	for _, e := range epics {
		fmt.Fprintf(&b, "- %s  [%s]  %s\n", e.ID, e.Status, e.Title)
		if e.ExternalID != "" {
			fmt.Fprintf(&b, "  external: %s\n", e.ExternalID)
		}
		if e.UpdatedAt != "" {
			fmt.Fprintf(&b, "  updated: %s\n", e.UpdatedAt)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatTickets(tickets []state.Ticket) string {
	if len(tickets) == 0 {
		return "(no tickets)"
	}
	var b strings.Builder
	for _, t := range tickets {
		fmt.Fprintf(&b, "- %s  [%s]  %s\n", t.ID, t.Status, t.Title)
		fmt.Fprintf(&b, "  type: %s\n", t.Type)
		if t.EpicID != "" {
			fmt.Fprintf(&b, "  epic: %s\n", t.EpicID)
		}
		if t.WaveID != "" {
			fmt.Fprintf(&b, "  wave: %s\n", t.WaveID)
		}
		if len(t.DependsOn) > 0 {
			fmt.Fprintf(&b, "  depends on: %s\n", strings.Join(t.DependsOn, ", "))
		}
		if t.EvidenceReceiptID != "" {
			fmt.Fprintf(&b, "  evidence: %s\n", t.EvidenceReceiptID)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatSandboxes(sandboxes []state.Sandbox) string {
	if len(sandboxes) == 0 {
		return "(no sandboxes)"
	}
	var b strings.Builder
	for _, s := range sandboxes {
		fmt.Fprintf(&b, "- %s  [%s]  %s\n", s.ID, s.Status, s.Backend)
		if s.RunID != "" {
			fmt.Fprintf(&b, "  run: %s\n", s.RunID)
		}
		if s.PreviewURL != "" {
			fmt.Fprintf(&b, "  preview: %s\n", s.PreviewURL)
		}
		if s.ExpiresAt != "" {
			fmt.Fprintf(&b, "  expires: %s\n", s.ExpiresAt)
		}
		if s.RetainedUntil != "" {
			fmt.Fprintf(&b, "  retained until: %s\n", s.RetainedUntil)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
