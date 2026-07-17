package onboarding

import "testing"

func TestCompletedOperationClearsRetainedFailure(t *testing.T) {
	op := New("onb-test", "github.com/acme/repo")
	op.ErrorCode = "railway_provision_failed"
	op.Error = "transient deployment failure"
	op.Set("resource_provision", "failed", op.Error)
	op.Set("completed", "succeeded", "hosted Vessica is ready")
	if op.Status != "completed" || op.ErrorCode != "" || op.Error != "" {
		t.Fatalf("completed operation retained failure: %#v", op)
	}
}
