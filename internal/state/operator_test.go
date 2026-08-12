package state

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestExpectedBriefingSlotsUseLosAngelesWeekdaysAndGrace(t *testing.T) {
	cases := []struct {
		name string
		now  string
		want []string
	}{
		{"before morning grace", "2026-08-12T13:45:00Z", []string{"cos-briefing:morning:2026-08-11", "cos-briefing:afternoon:2026-08-11"}},
		{"after morning grace", "2026-08-12T14:01:00Z", []string{"cos-briefing:morning:2026-08-12", "cos-briefing:afternoon:2026-08-11"}},
		{"after afternoon grace", "2026-08-13T00:01:00Z", []string{"cos-briefing:morning:2026-08-12", "cos-briefing:afternoon:2026-08-12"}},
		{"weekend uses friday", "2026-08-15T19:00:00Z", []string{"cos-briefing:morning:2026-08-14", "cos-briefing:afternoon:2026-08-14"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expectedBriefingSlots(now)
			if err != nil {
				t.Fatal(err)
			}
			keys := []string{got[0].Key + ":" + got[0].Date, got[1].Key + ":" + got[1].Date}
			if !reflect.DeepEqual(keys, tc.want) {
				t.Fatalf("slots=%v want=%v", keys, tc.want)
			}
		})
	}
}

func TestOperatorSnapshotUsesArtifactFreshnessAndOnlyMCPTransportMetrics(t *testing.T) {
	root := t.TempDir()
	db, err := Open("sqlite", filepath.Join(root, "state.db"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ws, err := db.EnsureWorkspace(context.Background(), root, "hosted")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendActionLedger(context.Background(), ActionLedgerInput{ActorID: "web-user", Source: "dashboard", Tool: "conversation_send", PolicyDecision: "allowed", LatencyMS: 900, IdempotencyKey: "web"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AppendActionLedger(context.Background(), ActionLedgerInput{ActorID: "mcp-user", Source: "mcp", Tool: "knowledge_search", PolicyDecision: "allowed", LatencyMS: 12, IdempotencyKey: "mcp"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 14, 1, 0, 0, time.UTC)
	if _, err = db.Exec(context.Background(), `INSERT INTO canonical_knowledge_artifacts(workspace_id,canonical_key,artifact_id,created_at,updated_at) VALUES(?,?,?,?,?),(?,?,?,?,?)`, ws.ID, "cos-briefing:morning", "art_morning", FormatTime(now.Add(-25*time.Hour)), FormatTime(now.Add(-25*time.Hour)), ws.ID, "cos-briefing:afternoon", "art_afternoon", FormatTime(now.Add(-12*time.Hour)), FormatTime(now.Add(-12*time.Hour))); err != nil {
		t.Fatal(err)
	}
	snapshot, err := db.OperatorSnapshot(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.MCPCalls != 1 || snapshot.MCPLatencyMS != 12 {
		t.Fatalf("MCP metrics include non-MCP actions: %#v", snapshot)
	}
	wantMorning := "cos-briefing:morning:2026-08-12"
	if len(snapshot.MissingBriefings) == 0 || snapshot.MissingBriefings[0] != wantMorning {
		t.Fatalf("stale current slot not detected: %v", snapshot.MissingBriefings)
	}
	for _, missing := range snapshot.MissingBriefings {
		if missing == "cos-briefing:afternoon:2026-08-11" {
			t.Fatalf("fresh prior afternoon was marked missing: %v", snapshot.MissingBriefings)
		}
	}
}
