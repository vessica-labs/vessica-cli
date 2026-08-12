package controlplane

import "github.com/vessica-labs/vessica-cli/internal/state"

// runtimeAgentRunPayload exposes internal durable fields only on the private,
// authenticated runtime protocol. Public API serialization remains redacted.
func runtimeAgentRunPayload(run *state.AgentRun) map[string]any {
	return map[string]any{
		"id": run.ID, "agent_id": run.AgentID, "input_json": run.InputJSON, "trigger": run.Trigger,
		"originating_repository_id": run.OriginatingRepositoryID, "rate_snapshot_json": run.RateSnapshotJSON,
		"resolved_knowledge_json": run.ResolvedKnowledgeJSON,
	}
}
