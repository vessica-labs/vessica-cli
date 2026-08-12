package controlplane

import (
	"fmt"
	"strings"
)

func requireOutlookFields(value map[string]any, fields ...string) error {
	for _, field := range fields {
		if _, ok := value[field]; !ok {
			return fmt.Errorf("required field %q is missing", field)
		}
	}
	return nil
}

func validateOutlookObject(value any, allowed, required []string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("must be an object")
	}
	allowedSet := map[string]bool{}
	for _, field := range allowed {
		allowedSet[field] = true
	}
	for field := range object {
		if !allowedSet[field] {
			return fmt.Errorf("field %q is not allowed", field)
		}
	}
	return requireOutlookFields(object, required...)
}

func validateOutlookConfidence(value any) error {
	confidence, ok := value.(float64)
	if !ok || confidence < 0 || confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	return nil
}

func validateOutlookEvidence(value any, included map[string]bool) error {
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return fmt.Errorf("evidence_ids must be a non-empty array")
	}
	seen := map[string]bool{}
	for _, raw := range values {
		identifier, ok := raw.(string)
		if !ok || identifier == "" || seen[identifier] || !included[identifier] {
			return fmt.Errorf("evidence_ids must be unique included source IDs")
		}
		seen[identifier] = true
	}
	return nil
}

func validateOutlookFindings(record map[string]any, evidence map[string]bool) error {
	definitions := map[string]struct{ allowed, required []string }{
		"decisions":      {[]string{"summary", "confidence", "evidence_ids"}, []string{"summary", "confidence", "evidence_ids"}},
		"commitments":    {[]string{"summary", "owner", "due_at", "confidence", "evidence_ids"}, []string{"summary", "owner", "confidence", "evidence_ids"}},
		"response_needs": {[]string{"reason", "due_at", "confidence", "evidence_ids"}, []string{"reason", "confidence", "evidence_ids"}},
	}
	for field, definition := range definitions {
		items, ok := record[field].([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", field)
		}
		for _, item := range items {
			if err := validateOutlookObject(item, definition.allowed, definition.required); err != nil {
				return fmt.Errorf("%s: %w", field, err)
			}
			object := item.(map[string]any)
			if err := validateOutlookConfidence(object["confidence"]); err != nil {
				return err
			}
			if err := validateOutlookEvidence(object["evidence_ids"], evidence); err != nil {
				return err
			}
			if dueAt, ok := object["due_at"].(string); ok && dueAt != "" {
				if _, err := parseOutlookTime(dueAt); err != nil {
					return fmt.Errorf("due_at must be RFC 3339")
				}
			}
		}
	}
	signals, ok := record["signals"].(map[string]any)
	if !ok || len(signals) != 3 {
		return fmt.Errorf("signals must contain clients, projects, and topics")
	}
	for _, field := range []string{"clients", "projects", "topics"} {
		items, ok := signals[field].([]any)
		if !ok {
			return fmt.Errorf("signals.%s must be an array", field)
		}
		for _, item := range items {
			if err := validateOutlookObject(item, []string{"name", "confidence", "evidence_ids"}, []string{"name", "confidence", "evidence_ids"}); err != nil {
				return err
			}
			object := item.(map[string]any)
			if err := validateOutlookConfidence(object["confidence"]); err != nil {
				return err
			}
			if err := validateOutlookEvidence(object["evidence_ids"], evidence); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOutlookTimeField(record map[string]any, field string) error {
	if _, err := parseOutlookTime(stringField(record, field)); err != nil {
		return fmt.Errorf("%s must be RFC 3339", field)
	}
	return nil
}

func validateOutlookRecurrence(value any) error {
	if value == nil {
		return nil
	}
	return validateOutlookObject(value, []string{"series_id", "occurrence_id"}, []string{"series_id", "occurrence_id"})
}

func validateOutlookChange(value any, eventAt string) error {
	if err := validateOutlookObject(value, []string{"kind", "cancelled_at", "previous_start", "new_start"}, []string{"kind"}); err != nil {
		return err
	}
	change := value.(map[string]any)
	kind := stringField(change, "kind")
	if !containsString([]string{"scheduled", "updated", "cancelled", "rescheduled"}, kind) {
		return fmt.Errorf("change.kind is invalid")
	}
	if kind == "cancelled" {
		if validateOutlookTimeField(change, "cancelled_at") != nil || change["previous_start"] != nil || change["new_start"] != nil {
			return fmt.Errorf("cancelled change fields are invalid")
		}
	}
	if kind == "rescheduled" {
		previous, previousErr := parseOutlookTime(stringField(change, "previous_start"))
		newStart, newErr := parseOutlookTime(stringField(change, "new_start"))
		if previousErr != nil || newErr != nil || previous.Equal(newStart) || stringField(change, "new_start") != eventAt {
			return fmt.Errorf("rescheduled change fields are invalid")
		}
	} else if kind != "cancelled" && (change["cancelled_at"] != nil || change["previous_start"] != nil || change["new_start"] != nil) {
		return fmt.Errorf("change contains timestamps not allowed for its kind")
	}
	return nil
}

func validateOutlookContact(update map[string]any, evidence map[string]bool) error {
	allowed := []string{"email", "display_name", "relationship_type", "client", "inferred_importance", "rationale", "confidence", "evidence_ids"}
	required := []string{"email", "display_name", "relationship_type", "inferred_importance", "rationale", "confidence", "evidence_ids"}
	if err := validateOutlookObject(update, allowed, required); err != nil {
		return err
	}
	if err := validateOutlookConfidence(update["confidence"]); err != nil {
		return err
	}
	if err := validateOutlookConfidence(update["inferred_importance"]); err != nil {
		return fmt.Errorf("inferred_importance: %w", err)
	}
	if !validOutlookEmail(stringField(update, "email")) || stringField(update, "display_name") == "" || stringField(update, "rationale") == "" {
		return fmt.Errorf("contact identity fields are invalid")
	}
	if !containsString([]string{"client", "colleague", "external_partner", "unknown"}, stringField(update, "relationship_type")) {
		return fmt.Errorf("relationship_type is invalid")
	}
	if err := validateOutlookEvidence(update["evidence_ids"], evidence); err != nil {
		return err
	}
	return rejectUnsafeOutlookValue(update)
}

func validOutlookEmail(value string) bool {
	return value != "" && value == strings.ToLower(value) && outlookEmailPattern.MatchString(value)
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}
