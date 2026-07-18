package run

import (
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestRequestedPreviewSandboxSurvivesReceiptWithoutPublicURL(t *testing.T) {
	if shouldDestroySandboxAfterReceipt(&state.Run{Preview: true, PreviewURL: ""}) {
		t.Fatal("requested hosted preview must survive until the launcher publishes it")
	}
	if !shouldDestroySandboxAfterReceipt(&state.Run{Preview: false}) {
		t.Fatal("non-preview run sandbox should be destroyed after receipt")
	}
}
