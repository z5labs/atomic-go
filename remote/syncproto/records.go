package syncproto

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/internal/postcard"
)

var (
	// ErrUnknownFamily reports an attempt to encode an object whose
	// [Family] is not [Family.Known].
	ErrUnknownFamily = errors.New("syncproto: unknown object family")

	// ErrInvalidUTF8 reports an attempt to encode a key, name or target
	// that is not valid UTF-8.
	ErrInvalidUTF8 = errors.New("syncproto: invalid UTF-8")
)

// ObjectRecord is one content-addressed object in transit.
type ObjectRecord struct {
	// Family is the family the object belongs to. Atomic trusts it only
	// for routing.
	Family Family

	// Key is the object's key. What it is depends on the family; see
	// ObjectRecord in docs/remote/endpoints/project.md.
	Key string

	// Bytes is the object itself.
	Bytes []byte
}

// RefRecord is a view-ref compare-and-swap in a push, or a view's current
// target in a pull response.
type RefRecord struct {
	// Name is the view name.
	Name string

	// ExpectOld is the target the client believes the ref holds, or nil
	// for a ref it believes does not exist yet. Pull responses leave it
	// nil.
	ExpectOld *string

	// NewTarget is the key of the view snapshot the ref should point at.
	NewTarget string
}

// Pack is the push body and the pull response: a batch of objects plus the
// ref moves that make them reachable.
type Pack struct {
	Objects []ObjectRecord
	Refs    []RefRecord
}

// Wants is the pull and clone request body.
type Wants struct {
	// Refs are the view names to fetch. Empty means every view on the
	// remote.
	Refs []string

	// Haves are the object keys, of any family, the client already holds.
	// The server treats them as a set.
	Haves []string

	// RefsOnly asks for only the view refs and their view-snapshot
	// objects, for the requested views and their ancestor chains: the ref
	// advertisement a push uses. False is a full pull or clone.
	RefsOnly bool
}

// Advertise returns the Wants that asks for the refs of views, or of every
// view when there are none, with no objects beyond their view snapshots.
func Advertise(views ...string) Wants {
	return Wants{Refs: slices.Clone(views), RefsOnly: true}
}

// All returns the Wants that asks for every view and every object whose key
// is not in haves.
func All(haves []string) Wants {
	return Wants{Haves: slices.Clone(haves)}
}

// AppendPostcard appends the postcard encoding of p to dst.
//
// It fails, appending nothing, with an error wrapping [ErrUnknownFamily]
// if an object's family is not [Family.Known], or [ErrInvalidUTF8] if a
// key, name or target is not valid UTF-8.
func (p Pack) AppendPostcard(dst []byte) ([]byte, error) {
	if err := p.check(); err != nil {
		return dst, err
	}
	e := postcard.NewEncoder(dst)
	postcard.PutSeq(e, p.Objects, putObject)
	postcard.PutSeq(e, p.Refs, putRef)
	return e.Bytes(), nil
}

func (p Pack) check() error {
	for i, o := range p.Objects {
		if !o.Family.Known() {
			return fmt.Errorf("%w: objects[%d] has family %d", ErrUnknownFamily, i, uint32(o.Family))
		}
		if !utf8.ValidString(o.Key) {
			return fmt.Errorf("%w: objects[%d].Key", ErrInvalidUTF8, i)
		}
	}
	for i, r := range p.Refs {
		switch {
		case !utf8.ValidString(r.Name):
			return fmt.Errorf("%w: refs[%d].Name", ErrInvalidUTF8, i)
		case r.ExpectOld != nil && !utf8.ValidString(*r.ExpectOld):
			return fmt.Errorf("%w: refs[%d].ExpectOld", ErrInvalidUTF8, i)
		case !utf8.ValidString(r.NewTarget):
			return fmt.Errorf("%w: refs[%d].NewTarget", ErrInvalidUTF8, i)
		}
	}
	return nil
}

func putObject(e *postcard.Encoder, o ObjectRecord) {
	e.Variant(uint32(o.Family))
	e.Str(o.Key)
	e.ByteSeq(o.Bytes)
}

func putRef(e *postcard.Encoder, r RefRecord) {
	e.Str(r.Name)
	postcard.PutOption(e, r.ExpectOld, (*postcard.Encoder).Str)
	e.Str(r.NewTarget)
}

// UnmarshalPostcard decodes the postcard encoding of a Pack from b into p,
// replacing what p held. Bytes after the pack are ignored, as Atomic's
// decoder ignores them. An object whose family is not [Family.Known] is
// kept. Each object's Bytes is a copy, not a view of b.
//
// A failure is a *[DecodeError] at [StagePostcard], and leaves p unchanged.
func (p *Pack) UnmarshalPostcard(b []byte) error {
	v, err := unmarshalPack(b, true)
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// unmarshalPack decodes a Pack from b. With copyBytes false, each object's
// Bytes is a view of b.
func unmarshalPack(b []byte, copyBytes bool) (Pack, error) {
	v, err := postcard.Decode(b, func(d *postcard.Decoder) Pack {
		var p Pack
		p.Objects = nilIfEmpty(postcard.Seq(d, func(d *postcard.Decoder) ObjectRecord {
			return decodeObject(d, copyBytes)
		}))
		p.Refs = nilIfEmpty(postcard.Seq(d, decodeRef))
		return p
	})
	if err != nil {
		return Pack{}, &DecodeError{Stage: StagePostcard, Err: err}
	}
	return v, nil
}

func decodeObject(d *postcard.Decoder, copyBytes bool) ObjectRecord {
	o := ObjectRecord{
		Family: Family(d.U32()),
		Key:    d.Str(),
		Bytes:  d.ByteSeq(),
	}
	if copyBytes {
		o.Bytes = bytes.Clone(o.Bytes)
	}
	return o
}

func decodeRef(d *postcard.Decoder) RefRecord {
	return RefRecord{
		Name:      d.Str(),
		ExpectOld: postcard.Option(d, (*postcard.Decoder).Str),
		NewTarget: d.Str(),
	}
}

// AppendPostcard appends the postcard encoding of w to dst.
//
// It fails, appending nothing, with an error wrapping [ErrInvalidUTF8] if a
// ref or have is not valid UTF-8.
func (w Wants) AppendPostcard(dst []byte) ([]byte, error) {
	for i, s := range w.Refs {
		if !utf8.ValidString(s) {
			return dst, fmt.Errorf("%w: Refs[%d]", ErrInvalidUTF8, i)
		}
	}
	for i, s := range w.Haves {
		if !utf8.ValidString(s) {
			return dst, fmt.Errorf("%w: Haves[%d]", ErrInvalidUTF8, i)
		}
	}
	e := postcard.NewEncoder(dst)
	postcard.PutSeq(e, w.Refs, (*postcard.Encoder).Str)
	postcard.PutSeq(e, w.Haves, (*postcard.Encoder).Str)
	e.Bool(w.RefsOnly)
	return e.Bytes(), nil
}

// UnmarshalPostcard decodes the postcard encoding of a Wants from b into w,
// replacing what w held. Bytes after it are ignored, as Atomic's decoder
// ignores them.
//
// A failure is a *[DecodeError] at [StagePostcard], and leaves w unchanged.
func (w *Wants) UnmarshalPostcard(b []byte) error {
	v, err := postcard.Decode(b, decodeWants)
	if err != nil {
		return &DecodeError{Stage: StagePostcard, Err: err}
	}
	*w = v
	return nil
}

func decodeWants(d *postcard.Decoder) Wants {
	return Wants{
		Refs:     nilIfEmpty(postcard.Seq(d, (*postcard.Decoder).Str)),
		Haves:    nilIfEmpty(postcard.Seq(d, (*postcard.Decoder).Str)),
		RefsOnly: d.Bool(),
	}
}

// nilIfEmpty makes an empty decoded list nil, so a value decodes to what
// it was built from whether its lists were nil or empty.
func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
