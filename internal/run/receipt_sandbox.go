package run

import "github.com/vessica-labs/vessica-cli/internal/state"

func shouldDestroySandboxAfterReceipt(r *state.Run) bool {
	// Hosted workers keep the validation URL private until the launcher
	// publishes the retained sandbox through the preview edge. A requested
	// preview must survive receipt finalization while its public URL is empty.
	return r == nil || !r.Preview
}
