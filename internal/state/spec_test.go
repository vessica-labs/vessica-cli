package state

import (
	"context"
	"testing"
)

func TestCreateEpicFromSpecIsAtomicAndResolvesDependencyKeys(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open("sqlite", "", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.EnsureWorkspace(ctx, dir, "solo"); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateEpicFromSpec(ctx, EpicSpec{Title: "Ship agent API", Tickets: []TicketSpec{
		{Key: "contract", Title: "Stabilize contract"},
		{Key: "plugin", Title: "Build plugin", DependsOn: []string{"contract"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Tickets) != 2 || len(created.Tickets[1].DependsOn) != 1 || created.Tickets[1].DependsOn[0] != created.Tickets[0].ID {
		t.Fatalf("created=%#v", created)
	}
	before, _ := db.ListEpics(ctx)
	if _, err := db.CreateEpicFromSpec(ctx, EpicSpec{Title: "Invalid", Tickets: []TicketSpec{{Key: "one", Title: "One", DependsOn: []string{"missing"}}}}); err == nil {
		t.Fatal("expected validation failure")
	}
	after, _ := db.ListEpics(ctx)
	if len(after) != len(before) {
		t.Fatalf("invalid spec persisted: before=%d after=%d", len(before), len(after))
	}
}

func TestValidateEpicSpecRejectsCycles(t *testing.T) {
	err := ValidateEpicSpec(EpicSpec{Title: "Cycle", Tickets: []TicketSpec{{Key: "a", Title: "A", DependsOn: []string{"b"}}, {Key: "b", Title: "B", DependsOn: []string{"a"}}}})
	if err == nil {
		t.Fatal("expected cycle error")
	}
}
