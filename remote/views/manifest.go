package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/z5labs/atomic-go/ident"
)

// Manifest is a view's manifest: its name, scope, parent and Merkle state,
// and its change log in order. It is the text body of get_view_manifest and
// put_view_manifest, and what a [Snapshot] is minted from.
//
// The format is documented in docs/remote/endpoints/project.md, under
// ViewManifest.
type Manifest struct {
	// Name is the view name.
	Name string

	// Scope is the view's scope. [ParseManifest] sets it to
	// [ident.ScopeShared] or [ident.ScopeDraft].
	Scope ident.Scope

	// Parent is the parent view's name, or "" for a root view.
	Parent string

	// State is the declared Merkle state of the log, or nil for an empty
	// view. Parsing does not check it against the log; [Manifest.Verify]
	// does.
	State *ident.Hash

	// Changes is the change log, in log order.
	Changes []ident.Hash
}

// ManifestErrorKind is the variant of atomic-repository's manifest error
// that a [ManifestError] corresponds to.
type ManifestErrorKind int

const (
	// ManifestEmpty: the text holds no header, only blank lines or
	// nothing.
	ManifestEmpty ManifestErrorKind = iota + 1

	// ManifestMalformedHeader: the header has fewer than four
	// tab-separated fields, or an empty name or scope. Text is the header
	// line.
	ManifestMalformedHeader

	// ManifestUnknownScope: the header's scope is neither shared nor
	// draft, in any ASCII case. Text is the scope.
	ManifestUnknownScope

	// ManifestInvalidState: the header's state is neither empty, '-' nor
	// a base32 hash. Text is the state.
	ManifestInvalidState

	// ManifestInvalidHash: a log line is not a base32 hash. Text is the
	// line, trimmed.
	ManifestInvalidHash

	// ManifestStateMismatch: [Manifest.Verify] found the declared state
	// differs from the fold of the log. Text is "declared {state},
	// computed {state}", each state in base32 or '-' for none.
	ManifestStateMismatch
)

var manifestErrorKindNames = [...]string{
	ManifestEmpty:           "Empty",
	ManifestMalformedHeader: "MalformedHeader",
	ManifestUnknownScope:    "UnknownScope",
	ManifestInvalidState:    "InvalidState",
	ManifestInvalidHash:     "InvalidHash",
	ManifestStateMismatch:   "StateMismatch",
}

// String returns the name of the Rust variant k corresponds to, such as
// "InvalidHash".
func (k ManifestErrorKind) String() string {
	if k > 0 && int(k) < len(manifestErrorKindNames) {
		return manifestErrorKindNames[k]
	}
	return "ManifestErrorKind(" + strconv.Itoa(int(k)) + ")"
}

// ManifestError reports manifest text that [ParseManifest] cannot read, or
// a manifest that [Manifest.Verify] or [Snapshot.Manifest] refuses.
type ManifestError struct {
	// Kind says what is wrong.
	Kind ManifestErrorKind

	// Line is the 1-based line the fault was found on, the header being
	// line 1 and every line after it counted, blank ones included. It is 0
	// for [ManifestEmpty] and [ManifestStateMismatch], which belong to no
	// line.
	Line int

	// Text is the offending text; each Kind says which.
	Text string
}

// Error implements the error interface.
func (e *ManifestError) Error() string {
	var reason string
	switch e.Kind {
	case ManifestEmpty:
		return "views: manifest is empty"
	case ManifestMalformedHeader:
		reason = fmt.Sprintf("malformed header %q", e.Text)
	case ManifestUnknownScope:
		reason = fmt.Sprintf("unknown scope %q", e.Text)
	case ManifestInvalidState:
		reason = fmt.Sprintf("invalid state %q", e.Text)
	case ManifestInvalidHash:
		reason = fmt.Sprintf("invalid change hash %q", e.Text)
	case ManifestStateMismatch:
		return "views: manifest state mismatch: " + e.Text
	default:
		reason = fmt.Sprintf("%v %q", e.Kind, e.Text)
	}
	return "views: manifest line " + strconv.Itoa(e.Line) + ": " + reason
}

// headerFields is the number of header fields a manifest is read from.
const headerFields = 4

// ParseManifest parses manifest text as atomic-repository does. It checks
// the structure only; call [Manifest.Verify] to check the state against
// the log.
//
// Lines are separated by "\n", with a trailing "\r" removed. Blank lines
// before the header are skipped, and the header is counted as line 1. The
// header is split on tab and each field trimmed; it needs at least four
// fields with a non-empty name and scope, and fields after the fourth are
// ignored. The scope is matched in any ASCII case and stored in lower case.
// A parent or state that is empty or '-' reads as none. Every later line is
// trimmed, blank ones are skipped, and the rest must be base32 hashes.
//
// A failure is a *[ManifestError].
func ParseManifest(text string) (Manifest, error) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return Manifest{}, &ManifestError{Kind: ManifestEmpty}
	}

	header := lines[0]
	fields := strings.Split(header, "\t")
	if len(fields) < headerFields {
		return Manifest{}, &ManifestError{Kind: ManifestMalformedHeader, Line: 1, Text: header}
	}
	fields = fields[:headerFields]
	for i, f := range fields {
		fields[i] = strings.TrimSpace(f)
	}
	if fields[0] == "" || fields[1] == "" {
		return Manifest{}, &ManifestError{Kind: ManifestMalformedHeader, Line: 1, Text: header}
	}

	m := Manifest{Name: fields[0]}
	switch {
	case equalFoldASCII(fields[1], string(ident.ScopeShared)):
		m.Scope = ident.ScopeShared
	case equalFoldASCII(fields[1], string(ident.ScopeDraft)):
		m.Scope = ident.ScopeDraft
	default:
		return Manifest{}, &ManifestError{Kind: ManifestUnknownScope, Line: 1, Text: fields[1]}
	}
	m.Parent = optional(fields[2])
	if s := optional(fields[3]); s != "" {
		h, err := ident.ParseHash(s)
		if err != nil {
			return Manifest{}, &ManifestError{Kind: ManifestInvalidState, Line: 1, Text: fields[3]}
		}
		m.State = &h
	}

	for i, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		h, err := ident.ParseHash(line)
		if err != nil {
			return Manifest{}, &ManifestError{Kind: ManifestInvalidHash, Line: i + 2, Text: line}
		}
		m.Changes = append(m.Changes, h)
	}
	return m, nil
}

// AppendText appends m to b as manifest text: the header, then one base32
// hash per line, every line ending in "\n". The header is the name, the
// scope in lower case, the parent or '-', and the state or '-', separated
// by tabs.
//
// It refuses, with an error wrapping [ErrUnwritable], a Manifest that would
// not read back as itself: an empty Name; a Scope that is not shared or
// draft in some ASCII case; a Name or Parent holding a tab, CR or LF or
// leading or trailing white space; and a Parent of "-", which reads back as
// none.
func (m Manifest) AppendText(b []byte) ([]byte, error) {
	if m.Name == "" {
		return b, fmt.Errorf("%w: manifest name is empty", ErrUnwritable)
	}
	var scope ident.Scope
	switch {
	case equalFoldASCII(string(m.Scope), string(ident.ScopeShared)):
		scope = ident.ScopeShared
	case equalFoldASCII(string(m.Scope), string(ident.ScopeDraft)):
		scope = ident.ScopeDraft
	default:
		return b, fmt.Errorf("%w: manifest scope %q is neither shared nor draft", ErrUnwritable, m.Scope)
	}
	for _, f := range []struct{ what, value string }{
		{"name", m.Name},
		{"parent", m.Parent},
	} {
		if strings.ContainsAny(f.value, "\t\r\n") {
			return b, fmt.Errorf("%w: manifest %s %q holds a tab, CR or LF", ErrUnwritable, f.what, f.value)
		}
		if strings.TrimSpace(f.value) != f.value {
			return b, fmt.Errorf("%w: manifest %s %q has leading or trailing white space", ErrUnwritable, f.what, f.value)
		}
	}
	if m.Parent == none {
		return b, fmt.Errorf("%w: manifest parent is %q, which reads back as none", ErrUnwritable, none)
	}

	b = append(b, m.Name...)
	b = append(b, '\t')
	b = append(b, scope...)
	b = append(b, '\t')
	b = appendOptional(b, m.Parent)
	b = append(b, '\t')
	if m.State == nil {
		b = append(b, ident.EmptyState...)
	} else {
		b = ident.AppendBase32(b, m.State[:])
	}
	b = append(b, '\n')
	for _, h := range m.Changes {
		b = ident.AppendBase32(b, h[:])
		b = append(b, '\n')
	}
	return b, nil
}

// MarshalText returns m as manifest text; see [Manifest.AppendText]. It
// implements [encoding.TextMarshaler].
func (m Manifest) MarshalText() ([]byte, error) {
	return m.AppendText(nil)
}

// Fold returns the Merkle state of m's change log, folded from the zero
// hash with [ident.FoldState], or nil when the log is empty.
func (m Manifest) Fold() *ident.Hash {
	if len(m.Changes) == 0 {
		return nil
	}
	s := ident.FoldState(m.Changes)
	return &s
}

// Verify checks that m's declared State is the fold of its log: both nil,
// or both the same hash. Otherwise it returns a *[ManifestError] of kind
// [ManifestStateMismatch] naming both.
func (m Manifest) Verify() error {
	computed := m.Fold()
	switch {
	case m.State == nil && computed == nil:
		return nil
	case m.State != nil && computed != nil && *m.State == *computed:
		return nil
	}
	return &ManifestError{
		Kind: ManifestStateMismatch,
		Text: "declared " + formatState(m.State) + ", computed " + formatState(computed),
	}
}

// formatState returns the base32 text of s, or '-' when s is nil.
func formatState(s *ident.Hash) string {
	if s == nil {
		return ident.EmptyState
	}
	return s.String()
}
