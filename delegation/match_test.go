package delegation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/delegation"
)

func TestMatch(t *testing.T) {
	tests := []struct {
		pattern, value string
		want           bool
	}{
		{"acme/*", "acme/api", true},
		{"acme/*", "acme/", true},
		{"acme/*", "acme", false},
		{"acme/*", "acme/api/v2", true},
		{"acme/*", "other/api", false},
		{"*", "", true},
		{"*", "anything", true},
		{"**", "x", true},
		{"", "", true},
		{"", "x", false},
		{"x", "", false},
		{"?", "", false},
		{"?", "a", true},
		{"?", "ab", false},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		// '?' is one rune, not one byte.
		{"?", "é", true},
		{"caf?", "café", true},
		{"??", "é", false},
		{"?", "日", true},
		{"日*", "日本", true},
		// Case-sensitive, and the whole value.
		{"main", "Main", false},
		{"main", "main", true},
		{"main", "mainline", false},
		{"*main", "domain", true},
		{"*main", "domains", false},
		// Backtracking across stars.
		{"*a*b", "xaxxb", true},
		{"*a*b", "xaxxbx", false},
		{"a*b*c", "abcbc", true},
		{"a*a*a", "aaa", true},
		{"a*a*a", "aa", false},
		{"*?", "", false},
		{"*?", "é", true},
		// No escapes or classes.
		{`\*`, `\x`, true},
		{"[ab]", "a", false},
		{"[ab]", "[ab]", true},
	}
	for _, tc := range tests {
		require.Equal(t, tc.want, delegation.Match(tc.pattern, tc.value), "Match(%q, %q)", tc.pattern, tc.value)
	}
}

func TestNormalizeServer(t *testing.T) {
	for in, want := range map[string]string{
		"https://atomic.storage":       "https://atomic.storage",
		"https://Atomic.Storage/":      "https://atomic.storage",
		"  https://ATOMIC.storage//  ": "https://atomic.storage",
		"https://atomic.storage/api/":  "https://atomic.storage/api",
		"":                             "",
	} {
		require.Equal(t, want, delegation.NormalizeServer(in), in)
	}
}
