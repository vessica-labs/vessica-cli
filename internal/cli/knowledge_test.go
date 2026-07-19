package cli

import (
	"encoding/json"
	"strings"
	"testing"

	knowledge "github.com/vessica-labs/vessica-knowledge-server/knowledge"
)

func TestSoloKnowledgeCLIAndEpicEpisode(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "dev", "up", "--profile", "solo", "--json")
	runCLI(t, dir, "memory", "add", "--type", "decision", "--subject", "Storage layer", "--predicate", "uses", "--object", "SQLite", "--title", "Storage", "--body", "Use SQLite without an embedding key", "--yes", "--idempotency-key", "memory-1", "--json")
	raw := runCLI(t, dir, "knowledge", "context", "--query", "SQLite embedding", "--token-budget", "1000", "--json")
	var env struct {
		Data knowledge.ContextResponse `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.RetrievalMode != "lexical" || len(env.Data.Decisions) != 1 || env.Data.Decisions[0].Memory.EmbeddingState != "not_configured" {
		t.Fatalf("context=%s", raw)
	}
	if got := env.Data.Decisions[0].Memory; got.Subject != "Storage layer" || got.Predicate != "uses" || got.Object != "SQLite" {
		t.Fatalf("structured memory=%#v", got)
	}
	retrievalRaw := runCLI(t, dir, "memory", "retrieve", "database without vectors", "--limit", "5", "--rerank", "never", "--json")
	var retrievalEnv struct {
		Data knowledge.MemoryRetrievalResponse `json:"data"`
	}
	if err := json.Unmarshal([]byte(retrievalRaw), &retrievalEnv); err != nil {
		t.Fatal(err)
	}
	if retrievalEnv.Data.Ranking.Version != "v2" || len(retrievalEnv.Data.Results) != 1 || retrievalEnv.Data.Rerank.Applied {
		t.Fatalf("retrieval=%s", retrievalRaw)
	}
	runCLI(t, dir, "epic", "add", "--title", "Knowledge history", "--body", "Record this accepted epic", "--yes", "--idempotency-key", "epic-1", "--json")
	history := runCLI(t, dir, "memory", "search", "Knowledge history", "--json")
	if !strings.Contains(history, `"type":"episode"`) || !strings.Contains(history, `"workflow_event_type":"epic.accepted"`) {
		t.Fatalf("history=%s", history)
	}
}

func TestMemoryReadersReturnEmptyArrays(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "dev", "up", "--profile", "solo", "--json")
	for _, args := range [][]string{{"memory", "list", "--json"}, {"memory", "search", "absent canary", "--json"}} {
		raw := runCLI(t, dir, args...)
		if !strings.Contains(raw, `"data":[]`) {
			t.Fatalf("expected empty array: %s", raw)
		}
	}
}
