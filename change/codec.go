package change

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/chrono"
	"github.com/z5labs/atomic-go/internal/postcard"
)

// envelope is what a HEADER payload carries, with its hash indices
// resolved to hashes: Atomic's ChangeHeaderV2 in domain form
// (docs/remote/change-format/sections.md#changeheaderv2). A schema version
// 1 payload carries only the header, and decodes to an envelope that is
// Durable and Native with no supersedes, empty lists and no metadata, as
// Atomic's reader supplies.
type envelope struct {
	header         Header
	kind           Kind
	supersedes     *ident.Hash
	origin         Origin
	causalFrontier []ident.Hash
	extraKnown     []ident.Hash

	// metadata is the opaque hashed application bytes, kept verbatim. They
	// are never read as a span patch, even when they start with ATSP.
	metadata []byte
}

// Variant counts of the enums a HEADER payload writes.
const (
	kindVariants       = 2 // WireChangeKindV2
	originVariants     = 3 // WireChangeOriginV2
	objectIDVariants   = 2 // WireGitObjectIdV2
	derivationVariants = 6 // WireGitDerivationV2
)

// decodeHeader decodes the HEADER payload b of a schema version v file,
// resolving its hash indices through t. Versions 2 and 3 share a layout.
func decodeHeader(v changefile.Version, b []byte, t changefile.HashTable) (envelope, error) {
	switch v {
	case changefile.Version1:
		return decodeHeaderV1(b)
	case changefile.Version2, changefile.Version3:
		return decodeHeaderV2(b, t)
	default:
		return envelope{}, sectionError(changefile.SectionHeader, changefile.ErrUnsupportedVersion, fmt.Sprintf("schema version %d", uint32(v)), nil)
	}
}

// appendHeader appends the HEADER payload of env for a schema version v
// file to dst, naming hashes by their index in t. Versions 2 and 3 share
// a layout. Version 1 writes env's header alone, and refuses an envelope
// carrying anything a version 1 payload cannot.
func appendHeader(dst []byte, v changefile.Version, env envelope, t changefile.HashTable) ([]byte, error) {
	switch v {
	case changefile.Version1:
		if !env.isV1() {
			return dst, sectionError(changefile.SectionHeader, changefile.ErrInvalidChangeMetadata, "a schema version 1 header carries only a Durable, Native change with no supersedes, frontier, extra_known or metadata", nil)
		}
		return appendHeaderV1(dst, env.header)
	case changefile.Version2, changefile.Version3:
		return appendHeaderV2(dst, env, t)
	default:
		return dst, sectionError(changefile.SectionHeader, changefile.ErrUnsupportedVersion, fmt.Sprintf("schema version %d", uint32(v)), nil)
	}
}

func (env envelope) isV1() bool {
	return env.kind == Kind{} && env.supersedes == nil && env.origin.Kind == Native &&
		env.origin.Validate() == nil && len(env.causalFrontier) == 0 &&
		len(env.extraKnown) == 0 && len(env.metadata) == 0
}

// decodeHeaderV1 decodes a schema version 1 HEADER payload: a bare
// ChangeHeader (docs/remote/change-format/sections.md#changeheader). Like
// postcard::from_bytes, it ignores bytes after the value.
func decodeHeaderV1(b []byte) (envelope, error) {
	h, err := postcard.Decode(b, decodeHeaderFields)
	if err != nil {
		return envelope{}, sectionError(changefile.SectionHeader, changefile.ErrPayload, "", err)
	}
	if err := h.parseTimestamp(); err != nil {
		return envelope{}, err
	}
	return envelope{header: h}, nil
}

// appendHeaderV1 appends the schema version 1 HEADER payload of h, a bare
// ChangeHeader, to dst.
func appendHeaderV1(dst []byte, h Header) ([]byte, error) {
	ts, err := h.timestampText()
	if err != nil {
		return dst, err
	}
	e := postcard.NewEncoder(dst)
	putHeaderFields(e, h, ts)
	return e.Bytes(), nil
}

// wireEnvelope is ChangeHeaderV2 as the payload writes it, with hash
// indices where envelope has hashes.
type wireEnvelope struct {
	header         Header
	kind           Kind
	supersedes     *uint16
	origin         Origin
	causalFrontier []uint16
	extraKnown     []uint16
	metadata       []byte
}

// decodeHeaderV2 decodes a schema version 2 or 3 HEADER payload, a
// ChangeHeaderV2 (docs/remote/change-format/sections.md#changeheaderv2),
// and resolves its hash indices through t with [changefile.HashTable.Reference].
// Like postcard::from_bytes, it ignores bytes after the value.
//
// Besides a payload that does not decode ([changefile.ErrPayload]) and an
// index that names no slot ([changefile.ErrHashIndexOutOfBounds]), it
// refuses with [changefile.ErrInvalidChangeMetadata] an index naming the
// zero hash, a causal_frontier or extra_known whose hashes are not
// strictly ascending, and an origin that fails [Origin.Validate].
func decodeHeaderV2(b []byte, t changefile.HashTable) (envelope, error) {
	w, err := postcard.Decode(b, decodeWireEnvelope)
	if err != nil {
		return envelope{}, sectionError(changefile.SectionHeader, changefile.ErrPayload, "", err)
	}
	if err := w.header.parseTimestamp(); err != nil {
		return envelope{}, err
	}
	env := envelope{
		header:   w.header,
		kind:     w.kind,
		origin:   w.origin,
		metadata: w.metadata,
	}
	if w.supersedes != nil {
		h, err := reference(changefile.SectionHeader, t, *w.supersedes, "supersedes")
		if err != nil {
			return envelope{}, err
		}
		env.supersedes = &h
	}
	if err := w.origin.Validate(); err != nil {
		return envelope{}, sectionError(changefile.SectionHeader, changefile.ErrInvalidChangeMetadata, "origin", err)
	}
	if env.causalFrontier, err = ascending(t, w.causalFrontier, "causal_frontier"); err != nil {
		return envelope{}, err
	}
	if env.extraKnown, err = ascending(t, w.extraKnown, "extra_known"); err != nil {
		return envelope{}, err
	}
	return env, nil
}

// appendHeaderV2 appends the schema version 2 or 3 HEADER payload of env
// to dst, naming each hash by its index in t.
//
// It refuses what [decodeHeaderV2] would: with
// [changefile.ErrInvalidChangeMetadata] a reference to the zero hash, a
// causal_frontier or extra_known that is not strictly ascending, an origin
// that fails [Origin.Validate], which includes a Git id of the wrong
// length for its algorithm, and a Durable kind with a working copy; with
// [changefile.ErrHashNotFound] a hash t does not hold; and with
// [changefile.ErrPayload] text that is not valid UTF-8 or a timestamp that
// cannot be written.
func appendHeaderV2(dst []byte, env envelope, t changefile.HashTable) ([]byte, error) {
	ts, err := env.header.timestampText()
	if err != nil {
		return dst, err
	}
	if !env.kind.Snapshot && env.kind.WorkingCopy != [16]byte{} {
		return dst, sectionError(changefile.SectionHeader, changefile.ErrInvalidChangeMetadata, "a Durable kind has a working copy", nil)
	}
	if err := env.origin.Validate(); err != nil {
		return dst, sectionError(changefile.SectionHeader, changefile.ErrInvalidChangeMetadata, "origin", err)
	}
	var supersedes *uint16
	if env.supersedes != nil {
		i, err := indexOf(t, *env.supersedes, "supersedes")
		if err != nil {
			return dst, err
		}
		supersedes = &i
	}
	frontier, err := indicesOf(t, env.causalFrontier, "causal_frontier")
	if err != nil {
		return dst, err
	}
	extraKnown, err := indicesOf(t, env.extraKnown, "extra_known")
	if err != nil {
		return dst, err
	}

	e := postcard.NewEncoder(dst)
	putHeaderFields(e, env.header, ts)
	putKind(e, env.kind)
	postcard.PutOption(e, supersedes, (*postcard.Encoder).U16)
	putOrigin(e, env.origin)
	postcard.PutSeq(e, frontier, (*postcard.Encoder).U16)
	postcard.PutSeq(e, extraKnown, (*postcard.Encoder).U16)
	e.ByteSeq(env.metadata)
	return e.Bytes(), nil
}

// decodeDeps decodes a DEPS payload, a Vec<u16> of hash indices
// (docs/remote/change-format/sections.md#deps-section), and resolves each
// through t with [changefile.HashTable.Reference]. Like Atomic's reader it
// does not check the order of the indices. It ignores bytes after the
// value, as postcard::from_bytes does.
func decodeDeps(b []byte, t changefile.HashTable) ([]ident.Hash, error) {
	indices, err := postcard.Decode(b, decodeIndices)
	if err != nil {
		return nil, sectionError(changefile.SectionDeps, changefile.ErrPayload, "", err)
	}
	var deps []ident.Hash
	for n, i := range indices {
		h, err := reference(changefile.SectionDeps, t, i, fmt.Sprintf("dependency %d", n))
		if err != nil {
			return nil, err
		}
		deps = append(deps, h)
	}
	return deps, nil
}

// appendDeps appends the DEPS payload of deps to dst. Like Atomic's
// ChangeWriter::write_dependencies, it refuses with
// [changefile.ErrInvalidChangeMetadata] an index of 0, an index of
// [changefile.ThisChange], and one that is not greater than the index
// before it.
func appendDeps(dst []byte, deps []changefile.HashIndex) ([]byte, error) {
	for n, i := range deps {
		switch {
		case i == 0:
			return dst, sectionError(changefile.SectionDeps, changefile.ErrInvalidChangeMetadata, fmt.Sprintf("dependency %d is index 0", n), nil)
		case i == changefile.ThisChange:
			return dst, sectionError(changefile.SectionDeps, changefile.ErrInvalidChangeMetadata, fmt.Sprintf("dependency %d is index 0xFFFF", n), nil)
		case n > 0 && i <= deps[n-1]:
			return dst, sectionError(changefile.SectionDeps, changefile.ErrInvalidChangeMetadata, fmt.Sprintf("dependency %d is index %d, not greater than %d before it", n, i, deps[n-1]), nil)
		}
	}
	e := postcard.NewEncoder(dst)
	postcard.PutSeq(e, deps, func(e *postcard.Encoder, i changefile.HashIndex) { e.U16(uint16(i)) })
	return e.Bytes(), nil
}

func decodeWireEnvelope(d *postcard.Decoder) wireEnvelope {
	return wireEnvelope{
		header:         decodeHeaderFields(d),
		kind:           decodeKind(d),
		supersedes:     postcard.Option(d, (*postcard.Decoder).U16),
		origin:         decodeOrigin(d),
		causalFrontier: decodeIndices(d),
		extraKnown:     decodeIndices(d),
		metadata:       clone(d.ByteSeq()),
	}
}

// decodeHeaderFields decodes a ChangeHeader, leaving the timestamp as
// text in TimestampText for [Header.parseTimestamp].
func decodeHeaderFields(d *postcard.Decoder) Header {
	return Header{
		Message:       d.Str(),
		Description:   postcard.Option(d, (*postcard.Decoder).Str),
		TimestampText: d.Str(),
		Authors:       nilIfEmpty(postcard.Seq(d, decodeAuthor)),
	}
}

func decodeAuthor(d *postcard.Decoder) Author {
	return Author{
		Name:     d.Str(),
		Email:    postcard.Option(d, (*postcard.Decoder).Str),
		Identity: postcard.Option(d, (*postcard.Decoder).Str),
	}
}

func decodeKind(d *postcard.Decoder) Kind {
	var k Kind
	if d.Variant(kindVariants) == 1 {
		k.Snapshot = true
		copy(k.WorkingCopy[:], d.Fixed(len(k.WorkingCopy)))
	}
	return k
}

func decodeOrigin(d *postcard.Decoder) Origin {
	o := Origin{Kind: OriginKind(d.Variant(originVariants))}
	switch o.Kind {
	case GitSynthesized:
		o.Commit = decodeObjectID(d)
		o.Parents = nilIfEmpty(postcard.Seq(d, decodeObjectID))
		o.Derivation = GitDerivation(d.Variant(derivationVariants))
	case GitResolution:
		o.Commit = decodeObjectID(d)
		o.Parents = nilIfEmpty(postcard.Seq(d, decodeObjectID))
	}
	return o
}

func decodeObjectID(d *postcard.Decoder) GitObjectID {
	id := GitObjectID{SHA256: d.Variant(objectIDVariants) == 1}
	id.Bytes = clone(d.Fixed(id.size()))
	return id
}

func decodeIndices(d *postcard.Decoder) []uint16 {
	return nilIfEmpty(postcard.Seq(d, (*postcard.Decoder).U16))
}

func putHeaderFields(e *postcard.Encoder, h Header, timestamp string) {
	e.Str(h.Message)
	postcard.PutOption(e, h.Description, (*postcard.Encoder).Str)
	e.Str(timestamp)
	postcard.PutSeq(e, h.Authors, putAuthor)
}

func putAuthor(e *postcard.Encoder, a Author) {
	e.Str(a.Name)
	postcard.PutOption(e, a.Email, (*postcard.Encoder).Str)
	postcard.PutOption(e, a.Identity, (*postcard.Encoder).Str)
}

func putKind(e *postcard.Encoder, k Kind) {
	if !k.Snapshot {
		e.Variant(0)
		return
	}
	e.Variant(1)
	e.Fixed(k.WorkingCopy[:])
}

// putOrigin writes o, which must already pass [Origin.Validate].
func putOrigin(e *postcard.Encoder, o Origin) {
	e.Variant(uint32(o.Kind))
	switch o.Kind {
	case GitSynthesized:
		putObjectID(e, o.Commit)
		postcard.PutSeq(e, o.Parents, putObjectID)
		e.Variant(uint32(o.Derivation))
	case GitResolution:
		putObjectID(e, o.Commit)
		postcard.PutSeq(e, o.Parents, putObjectID)
	}
}

func putObjectID(e *postcard.Encoder, id GitObjectID) {
	if id.SHA256 {
		e.Variant(1)
	} else {
		e.Variant(0)
	}
	e.Fixed(id.Bytes)
}

// parseTimestamp sets h.Timestamp from h.TimestampText, which it keeps.
func (h *Header) parseTimestamp() error {
	ts, err := chrono.Parse(h.TimestampText)
	if err != nil {
		return sectionError(changefile.SectionHeader, changefile.ErrPayload, "timestamp", err)
	}
	h.Timestamp = ts
	return nil
}

// timestampText returns the timestamp text to write for h, and checks
// that every string h holds is valid UTF-8, which a reader requires.
func (h Header) timestampText() (string, error) {
	invalid := func(what string) error {
		return sectionError(changefile.SectionHeader, changefile.ErrPayload, what+" is not valid UTF-8", nil)
	}
	if !utf8.ValidString(h.Message) {
		return "", invalid("message")
	}
	if h.Description != nil && !utf8.ValidString(*h.Description) {
		return "", invalid("description")
	}
	for i, a := range h.Authors {
		switch {
		case !utf8.ValidString(a.Name):
			return "", invalid(fmt.Sprintf("author %d name", i))
		case a.Email != nil && !utf8.ValidString(*a.Email):
			return "", invalid(fmt.Sprintf("author %d email", i))
		case a.Identity != nil && !utf8.ValidString(*a.Identity):
			return "", invalid(fmt.Sprintf("author %d identity", i))
		}
	}
	if h.TimestampText != "" {
		if _, err := chrono.Parse(h.TimestampText); err != nil {
			return "", sectionError(changefile.SectionHeader, changefile.ErrPayload, "timestamp text", err)
		}
		return h.TimestampText, nil
	}
	if y := h.Timestamp.UTC().Year(); y < 0 || y > 9999 {
		return "", sectionError(changefile.SectionHeader, changefile.ErrPayload, fmt.Sprintf("timestamp year %d is outside 0000 to 9999", y), nil)
	}
	return chrono.Format(h.Timestamp), nil
}

// reference resolves index i through t, as a HEADER or DEPS reference
// must be, reporting a failure in section with what in its detail.
func reference(section changefile.SectionType, t changefile.HashTable, i uint16, what string) (ident.Hash, error) {
	h, err := t.Reference(changefile.HashIndex(i))
	if err != nil {
		return ident.Hash{}, inSection(section, what, err)
	}
	return h, nil
}

// ascending resolves the HEADER indices of a causal_frontier or
// extra_known list through t and checks that the hashes are strictly
// ascending.
func ascending(t changefile.HashTable, indices []uint16, what string) ([]ident.Hash, error) {
	var hs []ident.Hash
	for n, i := range indices {
		h, err := reference(changefile.SectionHeader, t, i, fmt.Sprintf("%s %d", what, n))
		if err != nil {
			return nil, err
		}
		if n > 0 && hs[n-1].Compare(h) >= 0 {
			return nil, sectionError(changefile.SectionHeader, changefile.ErrInvalidChangeMetadata, fmt.Sprintf("%s is not in strictly ascending hash order at entry %d", what, n), nil)
		}
		hs = append(hs, h)
	}
	return hs, nil
}

// indexOf returns the HEADER index of h in t.
func indexOf(t changefile.HashTable, h ident.Hash, what string) (uint16, error) {
	if h.IsZero() {
		return 0, sectionError(changefile.SectionHeader, changefile.ErrInvalidChangeMetadata, what+" is the zero hash", nil)
	}
	i, ok := t.IndexOf(h)
	if !ok {
		return 0, sectionError(changefile.SectionHeader, changefile.ErrHashNotFound, what+": "+h.String(), nil)
	}
	return uint16(i), nil
}

// indicesOf returns the HEADER indices in t of a causal_frontier or
// extra_known list, which must be strictly ascending.
func indicesOf(t changefile.HashTable, hs []ident.Hash, what string) ([]uint16, error) {
	var out []uint16
	for n, h := range hs {
		if n > 0 && hs[n-1].Compare(h) >= 0 {
			return nil, sectionError(changefile.SectionHeader, changefile.ErrInvalidChangeMetadata, fmt.Sprintf("%s is not in strictly ascending hash order at entry %d", what, n), nil)
		}
		i, err := indexOf(t, h, fmt.Sprintf("%s %d", what, n))
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

// sectionError returns a *changefile.FormatError of kind in section, with
// no file offset: a payload codec does not know where its payload sits in
// the file.
func sectionError(section changefile.SectionType, kind error, detail string, err error) *changefile.FormatError {
	return &changefile.FormatError{Kind: kind, Section: section, Offset: -1, Detail: detail, Err: err}
}

// inSection returns err, a *changefile.FormatError from a hash table
// lookup, with its Section set to section and what before its detail.
func inSection(section changefile.SectionType, what string, err error) error {
	var fe *changefile.FormatError
	if !errors.As(err, &fe) {
		return err
	}
	c := *fe
	c.Section = section
	c.Detail = what + ": " + c.Detail
	return &c
}

func clone(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return slices.Clone(b)
}

func nilIfEmpty[T any](s []T) []T {
	if len(s) == 0 {
		return nil
	}
	return s
}
