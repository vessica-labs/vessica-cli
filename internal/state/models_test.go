package state

import (
	"testing"
	"time"
)

func TestFormatTimePreservesLexicographicChronology(t *testing.T) {
	earlier := time.Date(2026, 7, 21, 12, 0, 0, 100, time.UTC)
	later := earlier.Add(time.Nanosecond)

	earlierText := FormatTime(earlier)
	laterText := FormatTime(later)
	if len(earlierText) != len(laterText) {
		t.Fatalf("timestamps must have fixed width: %q and %q", earlierText, laterText)
	}
	if earlierText >= laterText {
		t.Fatalf("timestamp text must preserve chronological order: %q >= %q", earlierText, laterText)
	}
}
