package id

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// Prefixes for type-prefixed IDs per PRD §12.
const (
	Workspace  = "ws"
	Epic       = "epic"
	PRD        = "prd"
	ADR        = "adr"
	Test       = "test"
	Design     = "design"
	Artifact   = "art"
	ArtifactSet = "aset"
	Ticket     = "tix"
	Wave       = "wave"
	Run        = "run"
	Sandbox    = "sbx"
	Memory     = "mem"
	Receipt    = "rcpt"
	Trace      = "trace"
	Event      = "evt"
	Claim      = "clm"
	Pack       = "pack"
	Agent      = "agent"
)

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// New generates a type-prefixed unique ID: prefix_ + 10 base36 chars.
func New(prefix string) string {
	prefix = strings.TrimSuffix(prefix, "_")
	var b [8]byte
	_, _ = rand.Read(b[:])
	n := uint64(time.Now().UnixNano())
	for i := 0; i < 8; i++ {
		n ^= uint64(b[i]) << (uint(i) * 8)
	}
	return fmt.Sprintf("%s_%s", prefix, encodeBase36(n, 10))
}

func encodeBase36(n uint64, width int) string {
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = alphabet[n%36]
		n /= 36
	}
	return string(buf)
}

// Valid reports whether s looks like a type-prefixed ID.
func Valid(s string) bool {
	i := strings.IndexByte(s, '_')
	if i <= 0 || i >= len(s)-1 {
		return false
	}
	for _, c := range s[i+1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// Prefix returns the type prefix of an ID, or empty if invalid.
func Prefix(s string) string {
	i := strings.IndexByte(s, '_')
	if i <= 0 {
		return ""
	}
	return s[:i]
}
