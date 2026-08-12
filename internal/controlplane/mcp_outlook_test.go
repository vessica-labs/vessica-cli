package controlplane

import (
	"encoding/json"
	"testing"
)

func TestOutlookIngestionValidationRejectsContractViolations(t *testing.T) {
	valid := validOutlookMessage("message-1")
	validBatch := emptyOutlookBatch()
	validBatch.Messages = []map[string]any{cloneOutlookRecord(valid)}
	validBatch.BatchSummary.MessagesScanned, validBatch.BatchSummary.MessagesIncluded = 1, 1
	if err := validateOutlookBatch(validBatch); err != nil {
		t.Fatalf("valid contract record rejected: %v", err)
	}
	mutations := []func(map[string]any){
		func(record map[string]any) { delete(record, "direction") },
		func(record map[string]any) {
			record["participants"] = []any{map[string]any{"name": "Person", "email": "person@example.com", "role": "sender", "extra": true}}
		},
		func(record map[string]any) { record["evidence_ids"] = []any{"message-2"} },
		func(record map[string]any) { record["confidence"] = 1.1 },
		func(record map[string]any) {
			record["summary"] = "From: one@example.com\nTo: two@example.com\nSubject: copied\nquoted body"
		},
	}
	for i, mutate := range mutations {
		record := cloneOutlookRecord(valid)
		mutate(record)
		batch := emptyOutlookBatch()
		batch.Messages = []map[string]any{record}
		batch.BatchSummary.MessagesScanned, batch.BatchSummary.MessagesIncluded = 1, 1
		if err := validateOutlookBatch(batch); err == nil {
			t.Fatalf("contract violation %d was accepted: %#v", i, record)
		}
	}
}

func emptyOutlookBatch() outlookBatchV2 {
	return outlookBatchV2{
		Schema: "vessica.outlook-ingestion/v2", BatchID: "batch", GeneratedAt: "2026-08-11T08:00:01-07:00",
		Source:     outlookSource{Surface: "chatgpt_work", Connector: "outlook", ScheduledRun: outlookScheduledRun{TaskID: "task", RunID: "run"}},
		ScanWindow: outlookScanWindow{Start: "2026-08-11T07:00:00-07:00", End: "2026-08-11T08:00:00-07:00", Timezone: "America/Los_Angeles"},
		Watermarks: outlookWatermarks{Email: outlookWatermark{Previous: "2026-08-11T07:00:00-07:00", Candidate: "2026-08-11T08:00:00-07:00"}, Calendar: outlookWatermark{Previous: "2026-08-11T07:00:00-07:00", Candidate: "2026-08-11T08:00:00-07:00"}},
		Messages:   []map[string]any{}, CalendarEvents: []map[string]any{}, ContactUpdates: []map[string]any{}, BatchSummary: outlookBatchSummary{Warnings: []string{}},
	}
}

func validOutlookMessage(sourceID string) map[string]any {
	return map[string]any{
		"source_id": sourceID, "message_at": "2026-08-11T07:30:00-07:00", "direction": "inbound", "subject": "Follow up",
		"participants":   []any{map[string]any{"name": "Person", "email": "person@example.com", "role": "from"}},
		"connector_link": "https://outlook.office.com/mail/" + sourceID, "summary": "A concise follow-up.",
		"decisions": []any{}, "commitments": []any{}, "response_needs": []any{},
		"signals": map[string]any{"clients": []any{}, "projects": []any{}, "topics": []any{}}, "confidence": 0.9,
		"evidence_ids": []any{sourceID},
	}
}

func cloneOutlookRecord(record map[string]any) map[string]any {
	raw, _ := json.Marshal(record)
	var clone map[string]any
	_ = json.Unmarshal(raw, &clone)
	return clone
}
