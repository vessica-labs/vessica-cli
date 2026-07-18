package run

import (
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestDeterministicXSPlanningBundleRequiresExplicitLocalizedScope(t *testing.T) {
	bundle, ok := deterministicXSPlanningBundle(&state.Epic{
		Title: "[XS] Add benchmark marker",
		Body:  "Complexity: XS. This is intentionally XS: make exactly one localized documentation copy change in README.md.",
	})
	if !ok {
		t.Fatal("expected deterministic XS planning fast path")
	}
	if bundle.Complexity != "xs" || bundle.Ticket == nil || bundle.Ticket.EstimatedComplexity != "xs" {
		t.Fatalf("bundle=%#v", bundle)
	}
}

func TestDeterministicXSPlanningBundleRejectsAmbiguousOrCrossModuleWork(t *testing.T) {
	for _, epic := range []*state.Epic{
		{Title: "Update docs", Body: "Change the documentation."},
		{Title: "[XS] Migrate", Body: "Complexity: XS. Perform a database schema migration across-module."},
	} {
		if _, ok := deterministicXSPlanningBundle(epic); ok {
			t.Fatalf("unexpected fast path for %#v", epic)
		}
	}
}
