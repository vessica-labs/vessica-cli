package state

import (
	"context"
	"testing"
)

// Break caught: a retry of the same external run trigger creates a second
// agent run rather than returning the durable first result.
func TestTriggerAgentRunIsIdempotentAndStoresCheckpoint(t *testing.T) {
	db := agentTestDB(t)
	ctx := context.Background()
	agent, err := db.CreateAgent(ctx, "TRIGGER", "test", testDefinition, "{}", 5_000_000, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "trigger-1", Trigger: "mcp", InputJSON: `{"prompt":"hi"}`})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.TriggerAgentRun(ctx, AgentRunTriggerInput{AgentID: agent.ID, IdempotencyKey: "trigger-1", Trigger: "mcp", InputJSON: `{"prompt":"ignored"}`})
	if err != nil {
		t.Fatal(err)
	}
	if first.AgentRunID != second.AgentRunID || first.AgentRunID == "" {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	checkpoint, err := db.UpsertAgentTaskCheckpoint(ctx, AgentTaskCheckpointInput{AgentID: agent.ID, AgentRunID: first.AgentRunID, CheckpointKey: "phase:ingest", StateJSON: `{"cursor":7}`})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := db.UpsertAgentTaskCheckpoint(ctx, AgentTaskCheckpointInput{AgentID: agent.ID, AgentRunID: first.AgentRunID, CheckpointKey: "phase:ingest", StateJSON: `{"cursor":8}`})
	if err != nil || checkpoint.ID != updated.ID || updated.StateJSON != `{"cursor":8}` {
		t.Fatalf("checkpoint=%#v updated=%#v err=%v", checkpoint, updated, err)
	}
}
