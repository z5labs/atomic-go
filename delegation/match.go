package delegation

import (
	"strings"
	"unicode/utf8"
)

// Match reports whether value matches the glob pattern, as a scope's
// workspace, project and view patterns are matched
// (docs/remote/auth.md#scope). A '*' matches any run of characters,
// including none, a '?' matches exactly one character, and every other
// character matches only itself, case-sensitively. The whole value must
// match, so "acme/*" matches "acme/api" and "acme/" but not "acme".
//
// Characters are runes, not bytes, so '?' matches one multi-byte character.
// There is no escape and no character class: '\\' and '[' are ordinary
// characters.
func Match(pattern, value string) bool {
	p, v := 0, 0
	// star is the index in pattern just past the last '*' seen, or -1, and
	// next is where in value that '*' resumes matching on a mismatch.
	star, next := -1, 0
	for v < len(value) {
		if p < len(pattern) {
			switch c := pattern[p]; {
			case c == '*':
				p++
				star, next = p, v
				continue
			case c == '?':
				p++
				v += runeLen(value[v:])
				continue
			default:
				pw, vw := runeLen(pattern[p:]), runeLen(value[v:])
				if pattern[p:p+pw] == value[v:v+vw] {
					p, v = p+pw, v+vw
					continue
				}
			}
		}
		if star < 0 {
			return false
		}
		// Let the last '*' swallow one more rune and try again from there.
		next += runeLen(value[next:])
		p, v = star, next
	}
	return strings.Trim(pattern[p:], "*") == ""
}

// runeLen returns the length in bytes of the first rune of s, which is not
// empty. An invalid byte counts as one rune of its own.
func runeLen(s string) int {
	_, n := utf8.DecodeRuneInString(s)
	return n
}

// NormalizeServer returns the form in which a scope compares server URLs:
// s with surrounding white space trimmed, every trailing '/' removed, and
// lower-cased. So "https://Atomic.Storage/" and "https://atomic.storage"
// are the same server. Server URLs are compared exactly after this, never
// as globs.
func NormalizeServer(s string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(s), "/"))
}

// ResourceRef is the resource an authorization decision is about
// (docs/remote/auth.md#resourceref). It never goes on the wire: a server
// builds it from the request path.
//
// A field left "" is not constrained, and [Scope.Allows] does not check the
// scope's list for it. That is Atomic's absent field; Atomic can also check
// an empty value against a list, which a ResourceRef cannot express.
type ResourceRef struct {
	// Server is the server's URL, such as "https://atomic.storage". It is
	// compared after [NormalizeServer].
	Server string
	// Workspace is the workspace slug.
	Workspace string
	// Project is the project path, such as "acme/api".
	Project string
	// View is the view name.
	View string
}
