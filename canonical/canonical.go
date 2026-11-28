// Package canonical admits JSON documents under strict I-JSON rules and
// writes them as RFC 8785 canonical JSON (JCS), the bytes that delegation
// and project-identity documents are signed over
// (docs/remote/auth.md#signing-a-delegation-document).
//
// A [Document] keeps every member it was admitted with, including members
// this module does not model, so a signature computed over its canonical
// form covers a document from a newer Atomic exactly as that Atomic wrote
// it.
//
// Canonicalization is [jsontext.Value.Canonicalize]: object members are
// sorted by the UTF-16 code units of their names, strings use their minimal
// escaping (only ", \ and the C0 controls are escaped, so U+2028, <, & and >
// stay raw) and all whitespace is removed. Admission refuses every input
// whose canonical form would not be stable: duplicate names, invalid UTF-8,
// lone surrogates, nesting deeper than [MaxDepth], and numbers other than
// integers within ±[MaxInteger], which are the only numbers a double
// represents exactly.
package canonical

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	// MaxDepth is the deepest nesting of objects and arrays a document
	// admits. The document's own top-level object is depth 1.
	MaxDepth = 128

	// MaxInteger is the largest magnitude an admitted integer may have,
	// 2⁵³−1: every integer up to it is exactly representable as an IEEE
	// 754 double, which is how RFC 8785 reads every JSON number.
	MaxInteger = 1<<53 - 1
)

var (
	// ErrNotAdmitted is wrapped by every error for input that admission
	// refuses.
	ErrNotAdmitted = errors.New("canonical: not admitted")

	// ErrNoMember is wrapped by the error [Document.Decode] returns for a
	// name the document does not have.
	ErrNoMember = errors.New("canonical: no such member")
)

// Document is an admitted JSON object. Its members are held as
// [jsontext.Value]s in canonical form, keyed by name.
//
// A Document is immutable: every method that reads a member returns a copy,
// and [Document.With] and [Document.Without] return a new Document, leaving
// the receiver unchanged. The zero Document is the empty object.
type Document struct {
	members map[string]jsontext.Value
}

// Admit parses b as exactly one JSON object and admits it. It refuses, with
// an error wrapping [ErrNotAdmitted], input that is not a single object,
// holds a duplicate member name, invalid UTF-8 or a lone surrogate escape,
// nests deeper than [MaxDepth], or holds a number that is not an integer or
// is an integer beyond ±[MaxInteger]. Whitespace around the object is
// allowed.
func Admit(b []byte) (Document, error) {
	if err := admit(b, 0, true); err != nil {
		return Document{}, err
	}

	dec := jsontext.NewDecoder(bytes.NewReader(b))
	if _, err := dec.ReadToken(); err != nil {
		return Document{}, notAdmitted(err)
	}
	members := make(map[string]jsontext.Value)
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return Document{}, notAdmitted(err)
		}
		name := tok.String()
		v, err := dec.ReadValue()
		if err != nil {
			return Document{}, notAdmitted(err)
		}
		cv, err := canonicalize(v)
		if err != nil {
			return Document{}, err
		}
		members[name] = cv
	}
	return Document{members: members}, nil
}

// FromValue marshals v with encoding/json/v2 and admits the result, so v
// must marshal to a JSON object. A marshal error is returned wrapped; an
// object that admission refuses is an error wrapping [ErrNotAdmitted].
func FromValue(v any) (Document, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return Document{}, fmt.Errorf("canonical: marshal: %w", err)
	}
	return Admit(b)
}

// Canonical returns the document's RFC 8785 canonical bytes.
func (d Document) Canonical() ([]byte, error) {
	b := []byte{'{'}
	first := true
	for name, v := range d.members {
		if !first {
			b = append(b, ',')
		}
		first = false
		var err error
		b, err = jsontext.AppendQuote(b, name)
		if err != nil {
			return nil, fmt.Errorf("canonical: quote member name: %w", err)
		}
		b = append(b, ':')
		b = append(b, v...)
	}
	b = append(b, '}')

	v := jsontext.Value(b)
	if err := v.Canonicalize(); err != nil {
		return nil, fmt.Errorf("canonical: canonicalize: %w", err)
	}
	return v, nil
}

// Len returns the number of members the document has.
func (d Document) Len() int {
	return len(d.members)
}

// Has reports whether the document has a member named name.
func (d Document) Has(name string) bool {
	_, ok := d.members[name]
	return ok
}

// Member returns a copy of the canonical JSON of the member named name, and
// false when there is no such member.
func (d Document) Member(name string) (jsontext.Value, bool) {
	v, ok := d.members[name]
	if !ok {
		return nil, false
	}
	return v.Clone(), true
}

// Text returns the unescaped value of the member named name when that
// member is a JSON string. It returns false when there is no such member or
// it is not a string.
func (d Document) Text(name string) (string, bool) {
	v, ok := d.members[name]
	if !ok || v.Kind() != '"' {
		return "", false
	}
	s, err := jsontext.AppendUnquote(nil, v)
	if err != nil {
		return "", false
	}
	return string(s), true
}

// Decode unmarshals the member named name into v with encoding/json/v2.
// A missing member is an error wrapping [ErrNoMember]; an unmarshal error
// is returned wrapped.
func (d Document) Decode(name string, v any) error {
	m, ok := d.members[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoMember, name)
	}
	if err := json.Unmarshal(m.Clone(), v); err != nil {
		return fmt.Errorf("canonical: decode member %q: %w", name, err)
	}
	return nil
}

// With returns a copy of the document with the member named name set to v,
// replacing any member of that name. v is marshalled with encoding/json/v2,
// except that a [jsontext.Value] is taken as written, and the result is
// admitted as a member value one level below the document's top-level
// object. A value admission refuses is an error wrapping [ErrNotAdmitted].
// The receiver is unchanged either way.
func (d Document) With(name string, v any) (Document, error) {
	var b []byte
	if raw, ok := v.(jsontext.Value); ok {
		b = raw
	} else {
		var err error
		b, err = json.Marshal(v)
		if err != nil {
			return Document{}, fmt.Errorf("canonical: marshal member %q: %w", name, err)
		}
	}
	if err := admit(b, 1, false); err != nil {
		return Document{}, fmt.Errorf("member %q: %w", name, err)
	}
	cv, err := canonicalize(b)
	if err != nil {
		return Document{}, err
	}

	members := make(map[string]jsontext.Value, len(d.members)+1)
	for k, m := range d.members {
		members[k] = m
	}
	members[name] = cv
	return Document{members: members}, nil
}

// Without returns a copy of the document without the members named names.
// Names the document does not have are ignored, and the receiver is
// unchanged.
func (d Document) Without(names ...string) Document {
	members := make(map[string]jsontext.Value, len(d.members))
	for k, m := range d.members {
		members[k] = m
	}
	for _, name := range names {
		delete(members, name)
	}
	return Document{members: members}
}

// MarshalJSON returns the document's canonical bytes, as [Document.Canonical].
func (d Document) MarshalJSON() ([]byte, error) {
	return d.Canonical()
}

// UnmarshalJSON admits b, as [Admit], and replaces d with the result. d is
// unchanged when admission fails.
func (d *Document) UnmarshalJSON(b []byte) error {
	doc, err := Admit(b)
	if err != nil {
		return err
	}
	*d = doc
	return nil
}

// admit walks b, which must hold exactly one JSON value, and checks it
// against the I-JSON admission rules. base is the depth of the value's
// container: 0 for a document, 1 for a member value. object requires the
// value to be an object.
func admit(b []byte, base int, object bool) error {
	dec := jsontext.NewDecoder(bytes.NewReader(b))
	for i := 0; ; i++ {
		tok, err := dec.ReadToken()
		if err != nil {
			if i == 0 && errors.Is(err, io.EOF) {
				return fmt.Errorf("%w: no JSON value", ErrNotAdmitted)
			}
			return notAdmitted(err)
		}
		kind := tok.Kind()
		var number string
		if kind == '0' {
			number = tok.String()
		}
		if i == 0 && object && kind != '{' {
			return fmt.Errorf("%w: top-level value is %s, not an object", ErrNotAdmitted, kindName(kind))
		}
		switch kind {
		case '{', '[':
			if depth := base + dec.StackDepth(); depth > MaxDepth {
				return fmt.Errorf("%w: nesting depth %d exceeds %d at %s", ErrNotAdmitted, depth, MaxDepth, dec.StackPointer())
			}
		case '0':
			if err := admitNumber(number); err != nil {
				return fmt.Errorf("%w at %s", err, dec.StackPointer())
			}
		}
		if dec.StackDepth() == 0 {
			break
		}
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: data after the JSON value", ErrNotAdmitted)
		}
		return notAdmitted(err)
	}
	return nil
}

// admitNumber refuses a number that is not an integer within ±MaxInteger.
// s is the number as written in the input.
func admitNumber(s string) error {
	if strings.ContainsAny(s, ".eE") {
		return fmt.Errorf("%w: number %s is not an integer", ErrNotAdmitted, s)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > MaxInteger || n < -MaxInteger {
		return fmt.Errorf("%w: integer %s is beyond ±%d", ErrNotAdmitted, s, MaxInteger)
	}
	return nil
}

func canonicalize(b []byte) (jsontext.Value, error) {
	v := jsontext.Value(bytes.Clone(b))
	if err := v.Canonicalize(); err != nil {
		return nil, notAdmitted(err)
	}
	return v, nil
}

func notAdmitted(err error) error {
	return fmt.Errorf("%w: %w", ErrNotAdmitted, err)
}

func kindName(k jsontext.Kind) string {
	switch k {
	case 'n':
		return "null"
	case 'f', 't':
		return "a boolean"
	case '"':
		return "a string"
	case '0':
		return "a number"
	case '[':
		return "an array"
	default:
		return "an object"
	}
}
