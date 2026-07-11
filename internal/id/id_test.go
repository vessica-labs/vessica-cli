package id

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	a := New(Epic)
	b := New(Epic)
	if a == b {
		t.Fatalf("expected unique IDs, got %s twice", a)
	}
	if !strings.HasPrefix(a, "epic_") {
		t.Fatalf("prefix: %s", a)
	}
	if !Valid(a) {
		t.Fatalf("invalid: %s", a)
	}
	if Prefix(a) != Epic {
		t.Fatalf("Prefix: %s", Prefix(a))
	}
}

func TestValid(t *testing.T) {
	if Valid("") || Valid("epic") || Valid("_x") || Valid("epic_") {
		t.Fatal("expected invalid")
	}
	if !Valid("tix_3p8x1q9d") {
		t.Fatal("expected valid")
	}
}
