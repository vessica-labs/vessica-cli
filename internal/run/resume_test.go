package run

import (
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestExplicitResumeRerunsCompletedRequestedPhase(t *testing.T) {
	phases := []state.RunPhase{{Phase: "validate", Status: "completed"}}
	if got := resumeStartPhase("validate", true, phases); got != "validate" {
		t.Fatalf("explicit resume advanced to %q", got)
	}
	if got := resumeStartPhase("validate", false, phases); got != "preview" {
		t.Fatalf("implicit resume did not advance: %q", got)
	}
}
