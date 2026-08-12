package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vessica-labs/vessica-cli/internal/redaction"
	"github.com/vessica-labs/vessica-cli/internal/state"
)

type outlookScheduledRun struct {
	TaskID string `json:"task_id"`
	RunID  string `json:"run_id"`
}
type outlookSource struct {
	Surface      string              `json:"surface"`
	Connector    string              `json:"connector"`
	ScheduledRun outlookScheduledRun `json:"scheduled_run"`
}
type outlookScanWindow struct {
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}
type outlookWatermark struct {
	Previous  string `json:"previous"`
	Candidate string `json:"candidate"`
}
type outlookWatermarks struct {
	Email    outlookWatermark `json:"email"`
	Calendar outlookWatermark `json:"calendar"`
}
type outlookBatchSummary struct {
	MessagesScanned        int      `json:"messages_scanned"`
	MessagesIncluded       int      `json:"messages_included"`
	CalendarEventsScanned  int      `json:"calendar_events_scanned"`
	CalendarEventsIncluded int      `json:"calendar_events_included"`
	ResponseNeeds          int      `json:"response_needs"`
	ContactUpdates         int      `json:"contact_updates"`
	Warnings               []string `json:"warnings"`
}
type outlookBatchV2 struct {
	Schema         string              `json:"schema"`
	BatchID        string              `json:"batch_id"`
	GeneratedAt    string              `json:"generated_at"`
	Source         outlookSource       `json:"source"`
	ScanWindow     outlookScanWindow   `json:"scan_window"`
	Watermarks     outlookWatermarks   `json:"watermarks"`
	Messages       []map[string]any    `json:"messages"`
	CalendarEvents []map[string]any    `json:"calendar_events"`
	ContactUpdates []map[string]any    `json:"contact_updates"`
	BatchSummary   outlookBatchSummary `json:"batch_summary"`
}
type outlookIngestionInput struct {
	Batch          outlookBatchV2 `json:"batch"`
	IdempotencyKey string         `json:"idempotency_key" jsonschema:"must equal batch.batch_id"`
}
type outlookRejected struct {
	SourceID string `json:"source_id"`
	Reason   string `json:"reason"`
}
type outlookIngestionOutput struct {
	Schema              string            `json:"schema,omitempty"`
	BatchID             string            `json:"batch_id,omitempty"`
	AcceptedIDs         []string          `json:"accepted_ids,omitempty"`
	DeduplicatedIDs     []string          `json:"deduplicated_ids,omitempty"`
	Rejected            []outlookRejected `json:"rejected"`
	CommittedWatermarks outlookWatermarks `json:"committed_watermarks"`
	Error               *MCPToolError     `json:"error,omitempty"`
}

func (o *outlookIngestionOutput) setMCPError(err *MCPToolError) { o.Error = err }

func (s *Server) registerOutlookIngestionTool(server *mcp.Server) {
	addMCPTool(s, server, &mcp.Tool{
		Name:        "outlook_ingestion_submit",
		Description: "Validate and durably accept one minimized ChatGPT Work Outlook ingestion v2 batch.",
		Annotations: additiveWriteAnnotations("Submit Outlook ingestion", false),
	}, mcpToolOptions{Scope: "knowledge:write", RequiresIdempotency: true}, func() *outlookIngestionOutput { return &outlookIngestionOutput{} }, func(ctx context.Context, principal mcpPrincipal, in outlookIngestionInput) (*outlookIngestionOutput, error) {
		if in.IdempotencyKey != in.Batch.BatchID {
			return &outlookIngestionOutput{}, fmt.Errorf("idempotency_key must equal batch_id")
		}
		if err := validateOutlookBatch(in.Batch); err != nil {
			return &outlookIngestionOutput{}, err
		}
		checkpointJSON := mustMarshalJSON(in.Batch.Watermarks)
		batch, err := s.agentApp().SubmitOutlookIngestion(ctx, state.OutlookIngestionBatchInput{IdempotencyKey: in.Batch.BatchID, SubmittedBy: principal.ActorID, CheckpointJSON: checkpointJSON, WarningsJSON: mustMarshalJSON(in.Batch.BatchSummary.Warnings)})
		if err != nil {
			return &outlookIngestionOutput{}, err
		}
		out := &outlookIngestionOutput{Schema: "vessica.outlook-ingestion-receipt/v2", BatchID: in.Batch.BatchID, Rejected: []outlookRejected{}, CommittedWatermarks: in.Batch.Watermarks}
		records := append(append([]map[string]any{}, in.Batch.Messages...), in.Batch.CalendarEvents...)
		for _, record := range records {
			sourceID, _ := record["source_id"].(string)
			raw, _ := json.Marshal(record)
			item, duplicate, itemErr := s.agentApp().UpsertOutlookIngestionItem(ctx, state.OutlookIngestionItemInput{
				BatchID: batch.ID, SourceID: sourceID, InternetMessageID: stringField(record, "internet_message_id"),
				ConversationID: stringField(record, "conversation_id"), MessageAt: firstStringField(record, "message_at", "event_at"), NormalizedJSON: string(raw),
			})
			if itemErr != nil {
				return &outlookIngestionOutput{}, itemErr
			}
			if duplicate {
				out.DeduplicatedIDs = append(out.DeduplicatedIDs, sourceID)
				continue
			}
			if _, itemErr = s.agentApp().EnqueueOutlookOutbox(ctx, batch.ID, item.ID, in.Batch.BatchID+":"+sourceID); itemErr != nil {
				return &outlookIngestionOutput{}, itemErr
			}
			out.AcceptedIDs = append(out.AcceptedIDs, sourceID)
		}
		if err = s.agentApp().FinalizeOutlookIngestion(ctx, batch.ID, mustMarshalJSON(in.Batch.Watermarks.Email), mustMarshalJSON(in.Batch.Watermarks.Calendar)); err != nil {
			return &outlookIngestionOutput{}, err
		}
		return out, nil
	})
}

func validateOutlookBatch(batch outlookBatchV2) error {
	if batch.Schema != "vessica.outlook-ingestion/v2" || strings.TrimSpace(batch.BatchID) == "" {
		return fmt.Errorf("schema and batch_id are invalid")
	}
	if batch.Source.Surface != "chatgpt_work" || batch.Source.Connector != "outlook" || batch.Source.ScheduledRun.TaskID == "" || batch.Source.ScheduledRun.RunID == "" {
		return fmt.Errorf("scheduled ChatGPT Work Outlook provenance is required")
	}
	if batch.ScanWindow.Timezone != "America/Los_Angeles" {
		return fmt.Errorf("scan_window.timezone must be America/Los_Angeles")
	}
	generated, err := parseOutlookTime(batch.GeneratedAt)
	if err != nil {
		return fmt.Errorf("generated_at: %w", err)
	}
	start, err := parseOutlookTime(batch.ScanWindow.Start)
	if err != nil {
		return fmt.Errorf("scan_window.start: %w", err)
	}
	end, err := parseOutlookTime(batch.ScanWindow.End)
	if err != nil || !end.After(start) || generated.Before(end) {
		return fmt.Errorf("scan window or generated_at is inconsistent")
	}
	for name, watermark := range map[string]outlookWatermark{"email": batch.Watermarks.Email, "calendar": batch.Watermarks.Calendar} {
		previous, previousErr := parseOutlookTime(watermark.Previous)
		candidate, candidateErr := parseOutlookTime(watermark.Candidate)
		if previousErr != nil || candidateErr != nil || previous.After(candidate) || !candidate.Equal(end) {
			return fmt.Errorf("%s watermark is inconsistent", name)
		}
	}
	if batch.BatchSummary.MessagesIncluded != len(batch.Messages) || batch.BatchSummary.CalendarEventsIncluded != len(batch.CalendarEvents) || batch.BatchSummary.ContactUpdates != len(batch.ContactUpdates) {
		return fmt.Errorf("batch summary counts do not match included arrays")
	}
	if batch.BatchSummary.MessagesScanned < len(batch.Messages) || batch.BatchSummary.CalendarEventsScanned < len(batch.CalendarEvents) {
		return fmt.Errorf("scanned counts cannot be smaller than included counts")
	}
	seen := map[string]bool{}
	for _, records := range [][]map[string]any{batch.Messages, batch.CalendarEvents} {
		for _, record := range records {
			sourceID := stringField(record, "source_id")
			if sourceID == "" || seen[sourceID] {
				return fmt.Errorf("source_id is missing or duplicated")
			}
			seen[sourceID] = true
		}
	}
	responseNeeds := 0
	for kind, records := range map[string][]map[string]any{"message": batch.Messages, "calendar event": batch.CalendarEvents} {
		for _, record := range records {
			sourceID := stringField(record, "source_id")
			if err := validateOutlookRecord(record, kind, seen, start, end); err != nil {
				return fmt.Errorf("%s %s: %w", kind, sourceID, err)
			}
			responseNeeds += len(anySlice(record["response_needs"]))
		}
	}
	contactEmails := map[string]bool{}
	for _, update := range batch.ContactUpdates {
		if err := validateOutlookContact(update, seen); err != nil {
			return fmt.Errorf("contact update: %w", err)
		}
		email := stringField(update, "email")
		if contactEmails[email] {
			return fmt.Errorf("contact update email is duplicated")
		}
		contactEmails[email] = true
	}
	if batch.BatchSummary.ResponseNeeds != responseNeeds {
		return fmt.Errorf("batch summary response_needs does not match included findings")
	}
	return nil
}

func validateOutlookRecord(record map[string]any, kind string, evidence map[string]bool, windowStart, windowEnd time.Time) error {
	allowedMessage := map[string]bool{"source_id": true, "message_at": true, "direction": true, "subject": true, "participants": true, "connector_link": true, "summary": true, "decisions": true, "commitments": true, "response_needs": true, "signals": true, "confidence": true, "evidence_ids": true}
	allowedEvent := map[string]bool{"source_id": true, "event_at": true, "ends_at": true, "subject": true, "participants": true, "connector_link": true, "summary": true, "recurrence": true, "change": true, "decisions": true, "commitments": true, "response_needs": true, "signals": true, "confidence": true, "evidence_ids": true}
	allowed := allowedMessage
	if kind == "calendar event" {
		allowed = allowedEvent
	}
	for key := range record {
		if !allowed[key] {
			return fmt.Errorf("field %q is not allowed", key)
		}
	}
	required := []string{"source_id", "subject", "participants", "connector_link", "summary", "decisions", "commitments", "response_needs", "signals", "confidence", "evidence_ids"}
	if kind == "message" {
		required = append(required, "message_at", "direction")
	} else {
		required = append(required, "event_at", "ends_at", "recurrence", "change")
	}
	if err := requireOutlookFields(record, required...); err != nil {
		return err
	}
	link, err := url.Parse(stringField(record, "connector_link"))
	if err != nil || link.Scheme != "https" || !outlookHost(link.Hostname()) {
		return fmt.Errorf("connector_link is not a recognized Outlook HTTPS link")
	}
	timeField := map[string]string{"message": "message_at", "calendar event": "event_at"}[kind]
	if err = validateOutlookTimeField(record, timeField); err != nil {
		return err
	}
	recordAt, _ := parseOutlookTime(stringField(record, timeField))
	if kind == "message" && (recordAt.Before(windowStart) || recordAt.After(windowEnd)) {
		return fmt.Errorf("message_at must be inside the scan window")
	}
	if kind == "calendar event" {
		if err = validateOutlookTimeField(record, "ends_at"); err != nil {
			return err
		}
		endsAt, _ := parseOutlookTime(stringField(record, "ends_at"))
		if !endsAt.After(recordAt) {
			return fmt.Errorf("ends_at must be after event_at")
		}
		if err = validateOutlookRecurrence(record["recurrence"]); err != nil {
			return err
		}
		if err = validateOutlookChange(record["change"], stringField(record, "event_at")); err != nil {
			return err
		}
	}
	participants, ok := record["participants"].([]any)
	if !ok || len(participants) == 0 {
		return fmt.Errorf("participants must be a non-empty array")
	}
	requiredRole := "from"
	allowedRoles := []string{"from", "to", "cc"}
	if kind == "calendar event" {
		requiredRole, allowedRoles = "organizer", []string{"organizer", "required", "optional", "resource"}
	}
	requiredRoleCount := 0
	for _, participant := range participants {
		if err = validateOutlookObject(participant, []string{"name", "email", "role"}, []string{"name", "email", "role"}); err != nil {
			return fmt.Errorf("participant: %w", err)
		}
		object := participant.(map[string]any)
		if !validOutlookEmail(stringField(object, "email")) || !containsString(allowedRoles, stringField(object, "role")) {
			return fmt.Errorf("participant email or role is invalid")
		}
		if stringField(object, "role") == requiredRole {
			requiredRoleCount++
		}
	}
	if requiredRoleCount != 1 {
		return fmt.Errorf("participants must contain exactly one %s", requiredRole)
	}
	if stringField(record, "subject") == "" || stringField(record, "summary") == "" {
		return fmt.Errorf("subject and summary must be non-empty")
	}
	if kind == "message" && !containsString([]string{"inbound", "outbound", "internal"}, stringField(record, "direction")) {
		return fmt.Errorf("message direction is invalid")
	}
	if err = validateOutlookConfidence(record["confidence"]); err != nil {
		return err
	}
	if err = validateOutlookEvidence(record["evidence_ids"], evidence); err != nil {
		return err
	}
	if err = validateOutlookFindings(record, evidence); err != nil {
		return err
	}
	return rejectUnsafeOutlookValue(record)
}

var (
	htmlTagPattern      = regexp.MustCompile(`(?i)<\s*[a-z][^>]*>`)
	mailHeaderPattern   = regexp.MustCompile(`(?im)^\s*(from|to|cc|bcc|subject|date|sent|reply-to|message-id):`)
	jwtPattern          = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b`)
	outlookEmailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

func rejectUnsafeOutlookValue(value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			for _, prohibited := range []string{"raw_body", "body", "html", "mime", "attachment", "credential", "token", "cookie", "password", "private_key"} {
				if strings.Contains(lower, prohibited) {
					return fmt.Errorf("prohibited field %q", key)
				}
			}
			if err := rejectUnsafeOutlookValue(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := rejectUnsafeOutlookValue(child); err != nil {
				return err
			}
		}
	case string:
		lower := strings.ToLower(typed)
		if htmlTagPattern.MatchString(typed) || len(mailHeaderPattern.FindAllString(typed, -1)) >= 3 || jwtPattern.MatchString(typed) || redaction.Redact(typed) != typed || strings.Contains(lower, "ignore previous instructions") || strings.Contains(lower, "ignore all prior instructions") || strings.Contains(lower, "system prompt") || strings.Contains(lower, "-----begin") || strings.Contains(lower, "mime-version:") || strings.Contains(lower, "content-type:") || strings.Contains(lower, "cookie:") || strings.Contains(lower, "set-cookie:") {
			return fmt.Errorf("prohibited sensitive or instruction-like string")
		}
	}
	return nil
}

func parseOutlookTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("RFC 3339 timestamp is required")
	}
	return time.Parse(time.RFC3339, value)
}

func outlookHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, allowed := range []string{"outlook.office.com", "outlook.office365.com", "outlook.live.com", "outlook.office365.us", "outlook.cloud.microsoft"} {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func stringField(record map[string]any, key string) string {
	value, _ := record[key].(string)
	return strings.TrimSpace(value)
}

func firstStringField(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringField(record, key); value != "" {
			return value
		}
	}
	return ""
}
