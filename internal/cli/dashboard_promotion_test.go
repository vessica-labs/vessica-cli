package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vessica-labs/vessica-cli/internal/state"
)

func TestWritePromotionRecoverySnapshotUsesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery", "promotion.json")
	snapshot := &state.WorkplanSnapshot{Schema: "vessica.workplan-snapshot/v1", Checksum: "sha256:test"}
	if err := writePromotionRecoverySnapshot(path, snapshot); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("snapshot mode = %o, want 600", got)
	}
}
