package run

import (
	"reflect"
	"testing"
)

func TestStatusFilesPreservesFirstPathCharacter(t *testing.T) {
	status := " M app/docs/content/cli-reference.mdx\n?? .vessica-preview.pid\n"
	want := []string{"app/docs/content/cli-reference.mdx", ".vessica-preview.pid"}
	if got := statusFiles(status); !reflect.DeepEqual(got, want) {
		t.Fatalf("statusFiles()=%q, want %q", got, want)
	}
}
