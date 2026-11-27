// Package views reads and writes the rows of a remote's view inventory: one
// line per view, holding its name, scope, parent, change count, Merkle state
// and SetId, separated by tabs.
//
// Two requests return these rows, and Atomic parses them with different
// strictness. GET {project}/refs/views is read with [ParseInventory], which
// refuses the whole body at its first malformed row, through
// [ParseInfoStrict]. GET {code}?views is read with [ParseInfo], which drops
// the rows it cannot read. [Info.AppendText] writes a row, as a fake server
// would send it.
//
// The format is documented in docs/remote/types.md, under RemoteViewInfo,
// and the two requests in docs/remote/endpoints/project.md, under
// list_view_refs and list_views.
package views

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/z5labs/atomic-go/ident"
)

// none is what a row holds in place of an absent parent, state or SetId.
const none = "-"

// ErrUnwritable reports an [Info] that [Info.AppendText] cannot write as a
// row that reads back as the same Info.
var ErrUnwritable = errors.New("views: info cannot be written as an inventory row")

// Info is one row of a remote's view inventory.
type Info struct {
	// Name is the view name. It is never empty in a parsed row.
	Name string

	// Scope is the view's scope, kept as sent: a row's scope may be in any
	// case, and [ParseInfo] keeps a scope that is neither shared nor
	// draft.
	Scope ident.Scope

	// Parent is the parent view's name, or "" for a root view.
	Parent string

	// ChangeCount is the number of changes in the view, inherited ones
	// included for a draft.
	ChangeCount uint64

	// State is the view's base32 Merkle state, kept as sent and not
	// checked, or "" for an empty view.
	State string

	// SetID is the base32 SetId of the view's effective change set, kept
	// as sent and not checked, or "" when the server did not send one.
	SetID string
}

// ParseInfoStrict parses one row as the refs/views inventory does. It trims
// the line, splits it on tab and trims each field, then refuses:
//
//   - an empty line, with "row is empty";
//   - a row without 5 or 6 fields, with "expected 5 or 6 tab-separated
//     fields, got {n}";
//   - an empty name, with "view name is empty";
//   - a scope other than shared or draft, in any ASCII case, with
//     "unsupported view scope '{scope}'";
//   - a change count that is not an unsigned 64-bit decimal, with "invalid
//     change count '{value}'".
//
// The error's text is the reason alone. Parent, state and SetId are not
// checked; each reads as "" when it is '-' or empty.
//
// The empty-name refusal is kept from Atomic's parser, but no line reaches
// it: trimming the line leaves it starting with a byte that is neither
// white space nor a tab, and that byte is in the name.
//
// Because the line is trimmed before it is split, a five-field row whose
// state is empty, such as "fresh\tshared\t-\t0\t", loses its last tab and
// is refused as having 4 fields.
func ParseInfoStrict(line string) (Info, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Info{}, errors.New("row is empty")
	}
	fields := strings.Split(line, "\t")
	if n := len(fields); n < 5 || n > 6 {
		return Info{}, fmt.Errorf("expected 5 or 6 tab-separated fields, got %d", n)
	}
	for i, f := range fields {
		fields[i] = strings.TrimSpace(f)
	}
	if fields[0] == "" {
		return Info{}, errors.New("view name is empty")
	}
	if !equalFoldASCII(fields[1], string(ident.ScopeShared)) && !equalFoldASCII(fields[1], string(ident.ScopeDraft)) {
		return Info{}, fmt.Errorf("unsupported view scope '%s'", fields[1])
	}
	if _, err := ident.ParseSequence(fields[3]); err != nil {
		return Info{}, fmt.Errorf("invalid change count '%s'", fields[3])
	}
	info, _ := ParseInfo(line)
	return info, nil
}

// ParseInfo parses one row as the ?views inventory does, leniently. ok is
// false for a blank line and for one without a non-empty name and scope;
// any other line parses. A missing or non-numeric change count reads as 0,
// a parent, state or SetId that is '-' or empty reads as "", and fields
// after the sixth are ignored.
//
// Leniency is what lets an older server's JSON answer to ?views, such as
// {"workspace":"w"}, read as no rows rather than as an error.
func ParseInfo(line string) (info Info, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Info{}, false
	}
	fields := strings.SplitN(line, "\t", 7)
	if len(fields) > 6 {
		fields = fields[:6]
	}
	field := func(i int) string {
		if i < len(fields) {
			return strings.TrimSpace(fields[i])
		}
		return ""
	}
	info.Name = field(0)
	info.Scope = ident.Scope(field(1))
	if info.Name == "" || info.Scope == "" {
		return Info{}, false
	}
	info.Parent = optional(field(2))
	if n, err := ident.ParseSequence(field(3)); err == nil {
		info.ChangeCount = uint64(n)
	}
	info.State = optional(field(4))
	info.SetID = optional(field(5))
	return info, true
}

// optional returns "" for a field that is '-' or empty, and the field
// otherwise.
func optional(f string) string {
	if f == none {
		return ""
	}
	return f
}

// InventoryError reports the first row of a refs/views inventory body that
// [ParseInfoStrict] refuses.
type InventoryError struct {
	// Line is the row's 1-based line number in the body, blank lines
	// counted.
	Line int

	// Err is why [ParseInfoStrict] refused the row. Its text is the reason
	// alone, such as "view name is empty".
	Err error
}

// Error returns the message Atomic's client reports for the row, "Invalid
// view inventory row {line}: {reason}", so a client can report it as the
// CLI does.
func (e *InventoryError) Error() string {
	return "Invalid view inventory row " + strconv.Itoa(e.Line) + ": " + e.Err.Error()
}

// Unwrap returns e.Err.
func (e *InventoryError) Unwrap() error {
	return e.Err
}

// ParseInventory parses a refs/views inventory body: one row per line,
// separated by "\n", with a trailing "\r" on a line trimmed away. Blank
// lines are skipped, and every other line must pass [ParseInfoStrict]. The
// first that does not ends the parse with an *[InventoryError] naming its
// line. A body with no rows is a nil slice and no error.
func ParseInventory(body string) ([]Info, error) {
	var infos []Info
	for i, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		info, err := ParseInfoStrict(line)
		if err != nil {
			return nil, &InventoryError{Line: i + 1, Err: err}
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// AppendText appends i to b as one inventory row, without a line ending:
// six tab-separated fields, or five when SetID is "". An empty Parent or
// State is written as '-', so the row always has its state field and
// [ParseInfoStrict] reads it back as i.
//
// It refuses, with an error wrapping [ErrUnwritable], an Info that would not
// read back as itself: an empty Name or Scope; a field holding a tab, CR,
// LF or leading or trailing white space; and a Parent, State or SetID that
// is "-", which would read back as "". It does not check that Scope is
// shared or draft, so a fake server can still send a row only [ParseInfo]
// accepts.
func (i Info) AppendText(b []byte) ([]byte, error) {
	if i.Name == "" {
		return b, fmt.Errorf("%w: name is empty", ErrUnwritable)
	}
	if i.Scope == "" {
		return b, fmt.Errorf("%w: scope is empty", ErrUnwritable)
	}
	for _, f := range []struct{ what, value string }{
		{"name", i.Name},
		{"scope", string(i.Scope)},
		{"parent", i.Parent},
		{"state", i.State},
		{"set id", i.SetID},
	} {
		if strings.ContainsAny(f.value, "\t\r\n") {
			return b, fmt.Errorf("%w: %s %q holds a tab, CR or LF", ErrUnwritable, f.what, f.value)
		}
		if strings.TrimSpace(f.value) != f.value {
			return b, fmt.Errorf("%w: %s %q has leading or trailing white space", ErrUnwritable, f.what, f.value)
		}
		if f.value == none && f.what != "name" && f.what != "scope" {
			return b, fmt.Errorf("%w: %s is %q, which reads back as none", ErrUnwritable, f.what, none)
		}
	}

	b = append(b, i.Name...)
	b = append(b, '\t')
	b = append(b, i.Scope...)
	b = append(b, '\t')
	b = appendOptional(b, i.Parent)
	b = append(b, '\t')
	b = strconv.AppendUint(b, i.ChangeCount, 10)
	b = append(b, '\t')
	b = appendOptional(b, i.State)
	if i.SetID != "" {
		b = append(b, '\t')
		b = append(b, i.SetID...)
	}
	return b, nil
}

// MarshalText returns i as one inventory row; see [Info.AppendText].
func (i Info) MarshalText() ([]byte, error) {
	return i.AppendText(nil)
}

// appendOptional appends f to b, or '-' when f is "".
func appendOptional(b []byte, f string) []byte {
	if f == "" {
		return append(b, none...)
	}
	return append(b, f...)
}

// equalFoldASCII reports whether s and t are equal under ASCII case
// folding alone, as Rust's eq_ignore_ascii_case compares them. Unlike
// strings.EqualFold it does not fold "ſ" (U+017F) to "s".
func equalFoldASCII(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if lowerASCII(s[i]) != lowerASCII(t[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
