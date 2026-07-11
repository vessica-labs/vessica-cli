package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestFailPrintsStructuredErrorOnce(t *testing.T) {
	var out, errOut bytes.Buffer
	p := &Printer{JSON: true, Out: &out, Err: &errOut}
	err := p.Fail("missing", "thing missing", "try again")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsPrinted(err) {
		t.Fatal("expected printed error")
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	got := strings.TrimSpace(out.String())
	want := `{"schema":"vessica.cli/v1","ok":false,"error":{"code":"missing","message":"thing missing","hint":"try again"}}`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
