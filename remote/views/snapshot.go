package views

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// Hashes are kept as text, so a snapshot parsed from canonical bytes, such
// as those Atomic writes, re-marshals to those bytes and so to the same key.
// Bytes that are not canonical, with white space, other escapes or another
// member order, parse but re-marshal differently: to check a received View
// object against its key, hash the bytes received.
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
// [ident.SetIDOf] the log, and MerkleState is [Manifest.Fold] of the log, or
// "" when the log is empty.
//
// Every derived field comes from the log, so m's declared State is not
// read. For a manifest that passes [Manifest.Verify] it is the same state,
// and the snapshot is the one Atomic's push mints from it. A draft's log is copied unchanged, whether or not it includes changes
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
	if state := m.Fold(); state != nil {
		s.MerkleState = state.String()
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

// ParseSnapshot parses the JSON of a view snapshot, refusing what
// serde_json refuses when Atomic reads one. scope, own_changes and
// own_set_id are required; parent_view, prev and merkle_state may be
// absent or null. Member names are matched exactly, and members it does
// not know are ignored, as serde_json ignores them. A member given twice,
// a null in prev or own_changes, input that is not valid UTF-8, and a
// \u escape of an unpaired surrogate are refused. Empty lists read as nil.
//
// A failure wraps [ErrInvalidSnapshot].
func ParseSnapshot(b []byte) (Snapshot, error) {
	if !utf8.Valid(b) {
		return Snapshot{}, fmt.Errorf("%w: not valid UTF-8", ErrInvalidSnapshot)
	}
	if err := checkSurrogates(b); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	members, err := objectMembers(b)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}

	var s Snapshot
	var scope string
	var prev, ownChanges []*string
	for _, f := range []struct {
		name     string
		required bool
		into     any
	}{
		{"scope", true, &scope},
		{"parent_view", false, &s.ParentView},
		{"prev", false, &prev},
		{"own_changes", true, &ownChanges},
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
	if s.Prev, err = nonNullStrings("prev", prev); err != nil {
		return Snapshot{}, err
	}
	if s.OwnChanges, err = nonNullStrings("own_changes", ownChanges); err != nil {
		return Snapshot{}, err
	}
	return s, nil
}

// nonNullStrings returns the strings of ps, or nil when ps is empty. A nil
// element, a JSON null, is an error naming member.
func nonNullStrings(member string, ps []*string) ([]string, error) {
	if len(ps) == 0 {
		return nil, nil
	}
	ss := make([]string, len(ps))
	for i, p := range ps {
		if p == nil {
			return nil, fmt.Errorf("%w: %s[%d] is null", ErrInvalidSnapshot, member, i)
		}
		ss[i] = *p
	}
	return ss, nil
}

// objectMembers reads b as one JSON object, with nothing but white space
// after it, and returns its members' raw values by name. A name given
// twice is an error, as it is to a serde-derived struct.
func objectMembers(b []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	members := make(map[string]json.RawMessage)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, ok := tok.(string)
		if !ok {
			return nil, errors.New("member name is not a string")
		}
		if _, dup := members[name]; dup {
			return nil, fmt.Errorf("duplicate member %q", name)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		members[name] = raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			err = errors.New("data after the object")
		}
		return nil, err
	}
	return members, nil
}

// checkSurrogates refuses a \u escape in b of a surrogate that is not half
// of a high-low pair, which serde_json refuses and encoding/json reads as
// U+FFFD. Outside strings valid JSON holds no backslash, so b is scanned
// whole; anything else wrong with b is left to the decoder.
func checkSurrogates(b []byte) error {
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		if i+1 >= len(b) || b[i+1] != 'u' {
			i++
			continue
		}
		r, ok := hex4(b[i+2:])
		if !ok {
			return nil
		}
		i += 5
		switch {
		case r >= 0xDC00 && r <= 0xDFFF:
			return fmt.Errorf("unpaired surrogate \\u%04x", r)
		case r >= 0xD800 && r <= 0xDBFF:
			lo, ok := rune(0), false
			if i+6 < len(b) && b[i+1] == '\\' && b[i+2] == 'u' {
				lo, ok = hex4(b[i+3:])
			}
			if !ok || lo < 0xDC00 || lo > 0xDFFF {
				return fmt.Errorf("unpaired surrogate \\u%04x", r)
			}
			i += 6
		}
	}
	return nil
}

// hex4 reads four hex digits from the start of b.
func hex4(b []byte) (rune, bool) {
	if len(b) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range b[:4] {
		switch {
		case '0' <= c && c <= '9':
			r = r<<4 | rune(c-'0')
		case 'a' <= c && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case 'A' <= c && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
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
