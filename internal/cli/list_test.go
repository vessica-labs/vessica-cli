package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestEpicListShowsAllItems(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, dir, "init", "--profile", "solo", "--runner", "codex", "--repo", "github", "--json")
	for _, title := range []string{"One", "Two", "Three"} {
		runCLI(t, dir, "epic", "add", "--title", title, "--body", strings.ToLower(title), "--json")
	}

	human := runCLI(t, dir, "epic", "list")
	for _, title := range []string{"One", "Two", "Three"} {
		if !strings.Contains(human, title) {
			t.Fatalf("human epic list missing %q:\n%s", title, human)
		}
	}
	if got := strings.Count(human, "- epic_"); got != 3 {
		t.Fatalf("human epic list items=%d, want 3:\n%s", got, human)
	}

	raw := runCLI(t, dir, "epic", "list", "--json")
	var envelope struct {
		OK   bool `json:"ok"`
		Data []struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("parse json output: %v\n%s", err, raw)
	}
	if !envelope.OK || len(envelope.Data) != 3 {
		t.Fatalf("json epic list len=%d ok=%v raw=%s", len(envelope.Data), envelope.OK, raw)
	}
}

func runCLI(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := NewRoot()
	cmd.SetArgs(append([]string{"--cwd", cwd}, args...))
	cmd.SetIn(strings.NewReader(""))

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = origStdout
	}()

	err = cmd.Execute()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("ves %s failed: %v\n%s", strings.Join(args, " "), err, buf.String())
	}
	return buf.String()
}
