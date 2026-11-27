package views

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/ident"
)

// ErrInvalidSnapshot reports bytes that [ParseSnapshot] cannot read as a
// view snapshot.
var ErrInvalidSnapshot = errors.New("views: invalid view snapshot")

// Snapshot is a view snapshot: an immutable record of a view's identity,
// lineage and change log, carried as a View object and keyed by the BLAKE3
// of its canonical JSON.
//
// Hashes are kept as text, so a snapshot that was parsed re-marshals to the
// bytes it was read from, and so to the same key.
//
// The format is documented in docs/remote/endpoints/project.md, under
// ViewSnapshot.
type Snapshot struct {
	// Scope is the view's scope, kept as written. Only "draft", exactly, is
	// a draft; Atomic reads any other value as shared.
	Scope ident.Scope

	// ParentView is the parent view's name, or "" for a root view.
	ParentView string

	// Prev holds the keys of the predecessor snapshots: none for a view's
	// first snapshot, and more than one only for a view merge.
	Prev []string

	// OwnChanges holds the base32 change hashes of the view's log, in log
	// order.
	OwnChanges []string

	// OwnSetID is the base32 SetId of OwnChanges.
	OwnSetID string

	// MerkleState is the base32 Merkle state of the log, or "" for an
	// empty view.
	MerkleState string
}

// FromManifest mints the snapshot of m as Atomic's push does. Scope is
// [ident.ScopeDraft] when m's scope is draft, in any case, and
// [ident.ScopeShared] otherwise. Prev is the one key prev, or none when
// prev is "". OwnChanges is m's log in base32 and log order, OwnSetID is
// [ident.SetIDOf] the log, and MerkleState is m's declared State, or ""
// when it has none.
//
// FromManifest copies m as it is and does not verify it; call
// [Manifest.Verify] first for a manifest that was not computed locally. A
// draft's log is copied unchanged, whether or not it includes changes
// inherited from the parent; see the open question on own_changes in
// docs/remote/endpoints/project.md.
func FromManifest(m Manifest, prev string) Snapshot {
	s := Snapshot{
		Scope:      ident.ScopeShared,
		ParentView: m.Parent,
		OwnSetID:   ident.SetIDOf(m.Changes).String(),
	}
	if m.Scope.IsDraft() {
		s.Scope = ident.ScopeDraft
	}
	if prev != "" {
		s.Prev = []string{prev}
	}
	if len(m.Changes) > 0 {
		s.OwnChanges = make([]string, len(m.Changes))
		for i, h := range m.Changes {
			s.OwnChanges[i] = h.String()
		}
	}
	if m.State != nil {
		s.MerkleState = m.State.String()
	}
	return s
}

// IsDraft reports whether s is a draft: whether its Scope is exactly
// "draft". Unlike [ident.Scope.IsDraft], it does not fold case, since
// Atomic reads any scope but "draft" as shared.
func (s Snapshot) IsDraft() bool {
	return s.Scope == ident.ScopeDraft
}

// MarshalCanonical returns the canonical bytes of s, which are the bytes
// serde_json writes for Atomic's ViewSnapshot: compact JSON with the members
// scope, parent_view, prev, own_changes, own_set_id and merkle_state, in
// that order. parent_view and merkle_state are left out when they are "",
// and prev when it is empty; own_changes is written as [] when it is.
//
// Strings are escaped as serde_json escapes them: '"' and '\' with a
// backslash, the control characters \b, \t, \n, \f and \r by those
// escapes, every other byte below 0x20 as \u00XX in lower-case hex, and
// nothing else, so '<', '>', '&', U+2028 and U+2029 are written as they
// are. A string that is not valid UTF-8 has each invalid byte written as
// U+FFFD, as encoding/json does; Atomic cannot hold such a string, so there
// is no key of its to match.
func (s Snapshot) MarshalCanonical() []byte {
	b := make([]byte, 0, 64+64*len(s.OwnChanges))
	b = append(b, `{"scope":`...)
	b = appendJSONString(b, string(s.Scope))
	if s.ParentView != "" {
		b = append(b, `,"parent_view":`...)
		b = appendJSONString(b, s.ParentView)
	}
	if len(s.Prev) > 0 {
		b = append(b, `,"prev":`...)
		b = appendJSONStrings(b, s.Prev)
	}
	b = append(b, `,"own_changes":`...)
	b = appendJSONStrings(b, s.OwnChanges)
	b = append(b, `,"own_set_id":`...)
	b = appendJSONString(b, s.OwnSetID)
	if s.MerkleState != "" {
		b = append(b, `,"merkle_state":`...)
		b = appendJSONString(b, s.MerkleState)
	}
	return append(b, '}')
}

// Key returns the snapshot's object key: the lower-case hex of the BLAKE3
// hash of [Snapshot.MarshalCanonical].
func (s Snapshot) Key() string {
	return ident.Sum(s.MarshalCanonical()).Hex()
}

// Manifest returns the manifest of s under name, as Atomic's
// to_manifest_text reads a snapshot back: the scope is draft when
// [Snapshot.IsDraft] and shared otherwise, the parent is ParentView, the
// state is MerkleState, and the log is OwnChanges.
//
// A MerkleState or change hash that is not base32 is a *[ManifestError] of
// kind [ManifestInvalidState] or [ManifestInvalidHash], with the Line it
// would have in the manifest text. Manifest does not check the state
// against the log; call [Manifest.Verify] on the result.
func (s Snapshot) Manifest(name string) (Manifest, error) {
	m := Manifest{Name: name, Scope: ident.ScopeShared, Parent: s.ParentView}
	if s.IsDraft() {
		m.Scope = ident.ScopeDraft
	}
	if s.MerkleState != "" {
		h, err := ident.ParseHash(s.MerkleState)
		if err != nil {
			return Manifest{}, &ManifestError{Kind: ManifestInvalidState, Line: 1, Text: s.MerkleState}
		}
		m.State = &h
	}
	if len(s.OwnChanges) > 0 {
		m.Changes = make([]ident.Hash, len(s.OwnChanges))
		for i, c := range s.OwnChanges {
			h, err := ident.ParseHash(c)
			if err != nil {
				return Manifest{}, &ManifestError{Kind: ManifestInvalidHash, Line: i + 2, Text: c}
			}
			m.Changes[i] = h
		}
	}
	return m, nil
}

// ParseSnapshot parses the JSON of a view snapshot. scope, own_changes and
// own_set_id are required; parent_view, prev and merkle_state may be
// absent or null. Member names are matched exactly, and members it does
// not know are ignored, as serde_json ignores them. Empty lists read as
// nil.
//
// A failure wraps [ErrInvalidSnapshot].
func ParseSnapshot(b []byte) (Snapshot, error) {
	if !utf8.Valid(b) {
		return Snapshot{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidSnapshot)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(b, &members); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	if members == nil {
		return Snapshot{}, fmt.Errorf("%w: not a JSON object", ErrInvalidSnapshot)
	}

	var s Snapshot
	var scope string
	for _, f := range []struct {
		name     string
		required bool
		into     any
	}{
		{"scope", true, &scope},
		{"parent_view", false, &s.ParentView},
		{"prev", false, &s.Prev},
		{"own_changes", true, &s.OwnChanges},
		{"own_set_id", true, &s.OwnSetID},
		{"merkle_state", false, &s.MerkleState},
	} {
		raw, ok := members[f.name]
		if f.required && (!ok || string(raw) == "null") {
			return Snapshot{}, fmt.Errorf("%w: missing %s", ErrInvalidSnapshot, f.name)
		}
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, f.into); err != nil {
			return Snapshot{}, fmt.Errorf("%w: %s: %v", ErrInvalidSnapshot, f.name, err)
		}
	}
	s.Scope = ident.Scope(scope)
	if len(s.Prev) == 0 {
		s.Prev = nil
	}
	if len(s.OwnChanges) == 0 {
		s.OwnChanges = nil
	}
	return s, nil
}

// appendJSONStrings appends ss to b as a JSON array of strings.
func appendJSONStrings(b []byte, ss []string) []byte {
	b = append(b, '[')
	for i, s := range ss {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendJSONString(b, s)
	}
	return append(b, ']')
}

// appendJSONString appends s to b as a JSON string, escaped as serde_json
// escapes it; see [Snapshot.MarshalCanonical].
func appendJSONString(b []byte, s string) []byte {
	const hexDigits = "0123456789abcdef"
	b = append(b, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c >= utf8.RuneSelf {
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				b = utf8.AppendRune(b, utf8.RuneError)
			} else {
				b = append(b, s[i:i+n]...)
			}
			i += n
			continue
		}
		switch c {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\b':
			b = append(b, '\\', 'b')
		case '\t':
			b = append(b, '\\', 't')
		case '\n':
			b = append(b, '\\', 'n')
		case '\f':
			b = append(b, '\\', 'f')
		case '\r':
			b = append(b, '\\', 'r')
		default:
			if c < 0x20 {
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			} else {
				b = append(b, c)
			}
		}
		i++
	}
	return append(b, '"')
}
