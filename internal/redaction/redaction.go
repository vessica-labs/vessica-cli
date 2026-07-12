package redaction

import (
	"regexp"
	"strings"
)

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)["']?[a-z0-9_-]*(?:api[_-]?key|token|secret|password|authorization|oauth[_-]?json|codex[_-]?auth[_-]?b64|credential[_-]?encryption[_-]?key)["']?\s*:\s*["'][^"']*["']`),
	regexp.MustCompile(`(?i)([a-z0-9_-]*(?:oauth[_-]?json|codex[_-]?auth[_-]?b64|credential[_-]?encryption[_-]?key))\s*[:=]\s*['"]?[^\s'"]+`),
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|authorization)\s*[:=]\s*['"]?[^\s'"]+`),
	regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]+`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`gho_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`(?i)postgres(?:ql)?://[^\s]+`),
	regexp.MustCompile(`(?i)mysql://[^\s]+`),
	regexp.MustCompile(`(?i)mongodb(?:\+srv)?://[^\s]+`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
}

const redacted = "[REDACTED]"

// Redact replaces likely secrets in s.
func Redact(s string) string {
	out := s
	for _, re := range patterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			// Keep key name when present
			if i := strings.IndexAny(m, ":="); i > 0 && !strings.HasPrefix(strings.ToLower(m), "bearer") {
				return strings.TrimSpace(m[:i+1]) + " " + redacted
			}
			return redacted
		})
	}
	return out
}
