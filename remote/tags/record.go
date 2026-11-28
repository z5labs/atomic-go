package tags

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/change"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/chrono"
	"github.com/z5labs/atomic-go/internal/postcard"
)

// MarshalPostcard returns the postcard encoding of r: all ten fields of a
// TagRecord, ending with the metadata option, so the bytes decode in
// Atomic. The timestamp is r.TimestampText when it is set, and otherwise
// r.Timestamp as chrono writes it. Metadata is written as its JSON text.
//
// It fails, with an error wrapping [ErrInvalidRecord], when a string r
// holds is not valid UTF-8, when r.TimestampText is set and is not RFC 3339
// text chrono reads, when r.Timestamp is to be formatted and its year is
// outside 0000 to 9999, when r.Kind is not one Atomic declares, and when
// r.Metadata is set and is not valid JSON.
func (r Record) MarshalPostcard() ([]byte, error) {
	ts, err := r.check()
	if err != nil {
		return nil, err
	}
	e := postcard.NewEncoder(nil)
	e.Str(r.Name)
	e.Str(r.View)
	e.U64(uint64(r.Sequence))
	e.Array32(r.State)
	e.Array32(r.ChangeHash)
	e.Str(ts)
	postcard.PutOption(e, r.Author, putAuthor)
	postcard.PutOption(e, r.Message, (*postcard.Encoder).Str)
	e.Variant(uint32(r.Kind))
	if len(r.Metadata) == 0 {
		e.OptionTag(false)
	} else {
		e.OptionTag(true)
		e.ByteSeq(r.Metadata)
	}
	return e.Bytes(), nil
}

// check reports whether r can be written, and returns the timestamp text to
// write for it.
func (r Record) check() (string, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidRecord, fmt.Sprintf(format, args...))
	}
	switch {
	case !utf8.ValidString(r.Name):
		return "", invalid("name is not valid UTF-8")
	case !utf8.ValidString(r.View):
		return "", invalid("view is not valid UTF-8")
	case r.Message != nil && !utf8.ValidString(*r.Message):
		return "", invalid("message is not valid UTF-8")
	case r.Kind >= kindVariants:
		return "", invalid("kind %s is not one Atomic declares", r.Kind)
	case len(r.Metadata) > 0 && !utf8.Valid(r.Metadata):
		// json.Valid accepts invalid UTF-8 inside a string; the record
		// writes metadata as a Rust String, which does not.
		return "", invalid("metadata is not valid UTF-8")
	case len(r.Metadata) > 0 && !json.Valid(r.Metadata):
		return "", invalid("metadata is not valid JSON")
	}
	if a := r.Author; a != nil {
		switch {
		case !utf8.ValidString(a.Name):
			return "", invalid("author name is not valid UTF-8")
		case a.Email != nil && !utf8.ValidString(*a.Email):
			return "", invalid("author email is not valid UTF-8")
		case a.Identity != nil && !utf8.ValidString(*a.Identity):
			return "", invalid("author identity is not valid UTF-8")
		}
	}
	if r.TimestampText != "" {
		if _, err := chrono.Parse(r.TimestampText); err != nil {
			return "", fmt.Errorf("%w: timestamp text: %w", ErrInvalidRecord, err)
		}
		return r.TimestampText, nil
	}
	if y := r.Timestamp.UTC().Year(); y < 0 || y > 9999 {
		return "", invalid("timestamp year %d is outside 0000 to 9999", y)
	}
	return chrono.Format(r.Timestamp), nil
}

// UnmarshalPostcard decodes the postcard encoding of a TagRecord from b
// into r, replacing what r held. It keeps the timestamp text in
// r.TimestampText and sets r.Timestamp to the instant it names, in UTC.
// Metadata is kept as the JSON text it was written as. Bytes after the
// record are ignored, as postcard::from_bytes ignores them.
//
// A record that ends after its kind, with no metadata option at all, is
// accepted as one with no metadata. That is deliberately more lenient than
// Atomic, whose reader fails on it; see Open questions in
// docs/remote/endpoints/project.md.
//
// A failure wraps [ErrMalformed] and leaves r unchanged: bytes that do not
// decode, a kind Atomic does not declare, timestamp text chrono would not
// read, and metadata that is not valid JSON, which Atomic's reader parses
// and so refuses too.
func (r *Record) UnmarshalPostcard(b []byte) error {
	v, err := postcard.Decode(b, decodeRecord)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	ts, err := chrono.Parse(v.TimestampText)
	if err != nil {
		return fmt.Errorf("%w: timestamp: %w", ErrMalformed, err)
	}
	v.Timestamp = ts
	if v.Metadata != nil && !json.Valid(v.Metadata) {
		return fmt.Errorf("%w: metadata is not valid JSON", ErrMalformed)
	}
	*r = v
	return nil
}

func decodeRecord(d *postcard.Decoder) Record {
	r := Record{
		Name:          d.Str(),
		View:          d.Str(),
		Sequence:      ident.Sequence(d.U64()),
		State:         d.Array32(),
		ChangeHash:    d.Array32(),
		TimestampText: d.Str(),
		Author:        postcard.Option(d, decodeAuthor),
		Message:       postcard.Option(d, (*postcard.Decoder).Str),
		Kind:          Kind(d.Variant(kindVariants)),
	}
	if d.Err() != nil || d.Remaining() == 0 {
		return r
	}
	if m := postcard.Option(d, (*postcard.Decoder).Str); m != nil {
		r.Metadata = json.RawMessage(*m)
	}
	return r
}

// decodeAuthor decodes an Author as the HEADER section writes one
// (docs/remote/change-format/sections.md#author): the name as a string,
// then the email and the identity, each as an option of a string.
func decodeAuthor(d *postcard.Decoder) change.Author {
	return change.Author{
		Name:     d.Str(),
		Email:    postcard.Option(d, (*postcard.Decoder).Str),
		Identity: postcard.Option(d, (*postcard.Decoder).Str),
	}
}

// putAuthor writes a in the layout [decodeAuthor] reads.
func putAuthor(e *postcard.Encoder, a change.Author) {
	e.Str(a.Name)
	postcard.PutOption(e, a.Email, (*postcard.Encoder).Str)
	postcard.PutOption(e, a.Identity, (*postcard.Encoder).Str)
}

// ContentHash returns Atomic's content hash of r, TagRecord::content_hash:
// the BLAKE3 of these bytes, concatenated with no separators or length
// prefixes (docs/remote/types.md#which-identifier-is-which).
//
//   - the name and then the view, as UTF-8
//   - the sequence, as a little-endian u64
//   - the state and then the change hash, 32 bytes each
//   - the timestamp's Unix seconds, as a little-endian i64
//   - when there is an author, its name, and then its email when it has
//     one; nothing at all for no author
//   - the message, when there is one
//   - the kind, as one byte
//
// The metadata, the author's identity and the timestamp's fraction of a
// second are not hashed. The timestamp is the instant r.TimestampText names
// when it is set, as [Record.MarshalPostcard] writes it, and r.Timestamp
// otherwise. TimestampText that does not parse is not one MarshalPostcard
// would write; ContentHash then falls back to r.Timestamp.
func (r Record) ContentHash() ident.Hash {
	var b bytes.Buffer
	b.WriteString(r.Name)
	b.WriteString(r.View)
	b.Write(binary.LittleEndian.AppendUint64(nil, uint64(r.Sequence)))
	b.Write(r.State[:])
	b.Write(r.ChangeHash[:])
	b.Write(binary.LittleEndian.AppendUint64(nil, uint64(r.instant().Unix())))
	if a := r.Author; a != nil {
		b.WriteString(a.Name)
		if a.Email != nil {
			b.WriteString(*a.Email)
		}
	}
	if r.Message != nil {
		b.WriteString(*r.Message)
	}
	b.WriteByte(byte(r.Kind))
	return ident.Sum(b.Bytes())
}

// Key returns the key Atomic stores r's Tag object under: the base32 text
// of [Record.ContentHash].
func (r Record) Key() string {
	return r.ContentHash().String()
}

// instant returns the timestamp r is written and hashed with.
func (r Record) instant() time.Time {
	if r.TimestampText != "" {
		if t, err := chrono.Parse(r.TimestampText); err == nil {
			return t
		}
	}
	return r.Timestamp
}
