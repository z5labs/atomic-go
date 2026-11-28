package legacy

import (
	"errors"
	"strings"

	"github.com/z5labs/atomic-go/ident"
)

// Sentinels the errors of [ParseState] and [ParseChangelistEntry] match
// through [errors.Is]. They name the Rust parse error's variant; the
// error's text is the Rust error's display text.
var (
	// ErrEmpty matches a body or line with nothing in it but white space.
	ErrEmpty = errors.New("legacy: empty")

	// ErrInvalidFormat matches a body or line without the fields it needs.
	ErrInvalidFormat = errors.New("legacy: invalid format")

	// ErrInvalidNumber matches a position or sequence number that is not
	// an unsigned 64-bit decimal.
	ErrInvalidNumber = errors.New("legacy: invalid number")
)

// parseError is a parse failure: its text is the Rust error's display text
// and it matches one of the sentinels above.
type parseError struct {
	msg  string
	kind error
}

func (e *parseError) Error() string { return e.msg }

func (e *parseError) Unwrap() error { return e.kind }

// State is a view's current position and Merkle states, the body of
// [Client.State] (docs/remote/types.md#stateresponse).
type State struct {
	// Empty is true for a view with no changes, sent as "-". Every other
	// field is then zero.
	Empty bool

	// Position is the sequence number of the view's last change.
	Position ident.Sequence

	// Merkle is the view's base32 Merkle state at Position, kept as sent
	// and not checked.
	Merkle string

	// TagMerkle is the base32 Merkle state of the view's most recent tag,
	// kept as sent and not checked.
	TagMerkle string

	// SetID is the base32 SetId of the view, kept as sent and not checked,
	// or "" when the server sent only three tokens.
	SetID string
}

// ParseState parses a state response as the Rust client does. It trims
// body and splits it on any white space, newlines included, so the whole
// body is parsed rather than its first line. "-" alone is the empty view.
// Otherwise it needs at least three tokens: the position, the Merkle state
// and the tag Merkle state, then an optional SetId; tokens after the fourth
// are ignored.
//
// It refuses an empty body with "empty state response" ([ErrEmpty]), fewer
// than three tokens with "invalid state format: '{body}'"
// ([ErrInvalidFormat]), {body} being the trimmed body, and a position that
// is not an unsigned 64-bit decimal with "invalid position: '{value}'"
// ([ErrInvalidNumber]).
func ParseState(body string) (State, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return State{}, &parseError{msg: "empty state response", kind: ErrEmpty}
	}
	if body == "-" {
		return State{Empty: true}, nil
	}
	tokens := strings.Fields(body)
	if len(tokens) < 3 {
		return State{}, &parseError{msg: "invalid state format: '" + body + "'", kind: ErrInvalidFormat}
	}
	pos, err := ident.ParseSequence(tokens[0])
	if err != nil {
		return State{}, &parseError{msg: "invalid position: '" + tokens[0] + "'", kind: ErrInvalidNumber}
	}
	st := State{Position: pos, Merkle: tokens[1], TagMerkle: tokens[2]}
	if len(tokens) > 3 {
		st.SetID = tokens[3]
	}
	return st, nil
}

// ChangelistEntry is one entry of a view's log, one line of
// [Client.Changelist] (docs/remote/types.md#changelistentry).
type ChangelistEntry struct {
	// Sequence is the entry's 0-based position in the log.
	Sequence ident.Sequence

	// Hash is the base32 change hash, kept as sent and not checked.
	Hash string

	// Merkle is the view's base32 Merkle state after this entry, kept as
	// sent and not checked.
	Merkle string

	// Tagged is true when the line ended in '.'.
	Tagged bool
}

// ParseChangelistEntry parses one changelist line as the Rust client does:
// {sequence}.{hash}.{merkle}, with a trailing '.' marking a tagged entry.
//
// It trims the line. When the line ends in '.', it strips every trailing
// '.' and counts the dots left: two or more means the entry is tagged and
// the stripped text is parsed; otherwise the original line is. The text is
// split on '.', and parts after the third are ignored, so "0.A.B.C" is hash
// A and Merkle state B. "1.A.B.." is tagged, and "0.ABC." is refused for its
// empty Merkle state.
//
// It refuses an empty line with "empty changelist line" ([ErrEmpty]); fewer
// than three parts, an empty hash or an empty Merkle state with "invalid
// changelist format: '{line}'" ([ErrInvalidFormat]), {line} being the
// trimmed line; and a sequence that is not an unsigned 64-bit decimal with
// "invalid sequence number: '{value}'" ([ErrInvalidNumber]).
func ParseChangelistEntry(line string) (ChangelistEntry, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return ChangelistEntry{}, &parseError{msg: "empty changelist line", kind: ErrEmpty}
	}

	content, tagged := line, false
	if strings.HasSuffix(line, ".") {
		stripped := strings.TrimRight(line, ".")
		if strings.Count(stripped, ".") >= 2 {
			content, tagged = stripped, true
		}
	}

	invalid := func() (ChangelistEntry, error) {
		return ChangelistEntry{}, &parseError{msg: "invalid changelist format: '" + line + "'", kind: ErrInvalidFormat}
	}
	parts := strings.Split(content, ".")
	if len(parts) < 3 {
		return invalid()
	}
	seq, err := ident.ParseSequence(parts[0])
	if err != nil {
		return ChangelistEntry{}, &parseError{msg: "invalid sequence number: '" + parts[0] + "'", kind: ErrInvalidNumber}
	}
	if parts[1] == "" || parts[2] == "" {
		return invalid()
	}
	return ChangelistEntry{Sequence: seq, Hash: parts[1], Merkle: parts[2], Tagged: tagged}, nil
}

// parseChangelist parses a changelist body: each line trimmed, blank lines
// skipped, and every other line a [ChangelistEntry]. The first line that
// does not parse fails the whole body.
func parseChangelist(body string) ([]ChangelistEntry, error) {
	var entries []ChangelistEntry
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		e, err := ParseChangelistEntry(line)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// TagRef is one tag of a view, one line of [Client.ListTags]: the state
// to pass to [Client.DownloadTag] and the tag's name.
type TagRef struct {
	// Hash is the tagged state, kept as sent and not checked.
	Hash string

	// Name is the tag's name. It may hold spaces.
	Name string
}

// parseTagRefs parses a tag list body as the Rust client does: each line
// trimmed and blank lines skipped, every other line split at its first
// space into hash and name, and a line with no space skipped.
func parseTagRefs(body string) []TagRef {
	var refs []TagRef
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		hash, name, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		refs = append(refs, TagRef{Hash: hash, Name: name})
	}
	return refs
}

// nonBlankLines returns body's lines, each trimmed, with the blank ones
// dropped.
func nonBlankLines(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
