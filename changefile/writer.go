package changefile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/z5labs/atomic-go/ident"
)

var (
	// ErrNilCompressor reports a nil [Compressor] given to [NewWriter] or
	// to [ReplaceUnhashed] with a payload to write. Like
	// [ErrNilDecompressor], it is a mistake in the caller and is returned
	// as it is, not as a [FormatError].
	ErrNilCompressor = errors.New("changefile: nil Compressor")

	// ErrNilFile reports a nil *[File] given to [ReplaceUnhashed]. It is
	// returned as it is, not as a [FormatError].
	ErrNilFile = errors.New("changefile: nil File")

	// ErrWriterFinished reports a section added to a [Writer] after
	// [Writer.Finish] has returned a file. It is returned as it is, not as
	// a [FormatError].
	ErrWriterFinished = errors.New("changefile: Writer already finished")
)

// Writer writes a canonical version 2 or 3 change file, one section at a
// time, following docs/remote/change-format/container.md#writer-rules:
// the sections must be added in [SectionType.Order], each of HEADER, DEPS,
// PROVENANCE, SIGNATURE, SPAN and UNHASHED at most once, and SPAN only in
// version 3. [Writer.Finish] sets the header's counts and flags from the
// sections added, so the caller never writes them.
//
// The payloads given to [Writer.Add] and [Writer.AddChunk] are the
// decompressed payloads; the Writer compresses each one separately with
// its [Compressor]. Encoding a payload, and the order of the hashes in the
// table, are the caller's: the Writer checks the container's rules, not
// the rules Atomic's reader applies to decoded payloads.
//
// A call that fails leaves the Writer as it was, so the caller can carry
// on with another section. A Writer is not safe for concurrent use.
type Writer struct {
	version Version
	table   HashTable
	c       Compressor
	hasher  *ident.Hasher

	// body is every section added so far, frames and payloads, in order.
	body []byte

	// counts holds the sections added so far of each type, by
	// [SectionType.Order].
	counts [9]uint64

	prev    SectionType
	hasPrev bool

	out  []byte
	hash ident.Hash
}

// NewWriter returns a Writer of a change file of version v whose hash
// dedup table is table, compressing each payload with c. The table is
// written, and hashed, as it is; build it with a [TableBuilder] to put the
// zero hash in slot 0, as Atomic does. NewWriter fails with a
// *[FormatError] of kind [ErrUnsupportedVersion] for a v other than
// [Version2] or [Version3], which are the versions Atomic writes, and with
// [ErrNilCompressor] for a nil c.
func NewWriter(v Version, table HashTable, c Compressor) (*Writer, error) {
	if v != Version2 && v != Version3 {
		return nil, formatError(ErrUnsupportedVersion, 0, -1, fmt.Sprintf("a writer writes version 2 or 3, got %d", v))
	}
	if c == nil {
		return nil, ErrNilCompressor
	}
	hasher := ident.NewHasher()
	tb, _ := table.AppendBinary(nil)
	hasher.Write(tb)
	return &Writer{version: v, table: table, c: c, hasher: hasher}, nil
}

// Add compresses payload and adds it as the next section, of type t. It
// fails with a *[FormatError] of kind [ErrInvalidSectionType] for a t that
// is not [SectionType.Known], and of kind [ErrUnexpectedSection] when:
//
//   - t is CONTENT, which [Writer.AddChunk] adds;
//   - t comes before the previous section in [SectionType.Order];
//   - t is HEADER, DEPS, PROVENANCE, SIGNATURE, SPAN or UNHASHED, and a
//     section of t has already been added;
//   - t is SPAN in a version 2 file.
//
// It fails with [ErrWriterFinished] once [Writer.Finish] has returned a
// file.
func (w *Writer) Add(t SectionType, payload []byte) error {
	if t == SectionContent {
		return formatError(ErrUnexpectedSection, t, -1, "CONTENT sections are added with AddChunk")
	}
	if err := w.check(t); err != nil {
		return err
	}
	start := len(w.body)
	w.body = append(w.body, byte(t), 0, 0, 0, 0)
	return w.compress(t, start, FrameSize, payload)
}

// AddChunk compresses chunk and adds it as the next CONTENT section. Its
// frame numbers it after the CONTENT sections already added, counting from
// 0, and gives its length and BLAKE3 hash. Concatenated in order, the
// chunks are the change's content blob; how the blob is split is the
// caller's, and changes the change hash. AddChunk fails as [Writer.Add]
// does, with kind [ErrUnexpectedSection] when an UNHASHED section has
// already been added.
func (w *Writer) AddChunk(chunk []byte) error {
	if err := w.check(SectionContent); err != nil {
		return err
	}
	if uint64(len(chunk)) > math.MaxUint32 {
		return formatError(ErrPayload, SectionContent, -1, fmt.Sprintf("a chunk of %d bytes exceeds the u32 uncompressed_len", len(chunk)))
	}
	start := len(w.body)
	le := binary.LittleEndian
	w.body = append(w.body, byte(SectionContent))
	w.body = le.AppendUint32(w.body, uint32(w.counts[SectionContent.Order()]))
	sum := ident.Sum(chunk)
	w.body = append(w.body, sum[:]...)
	w.body = le.AppendUint32(w.body, uint32(len(chunk)))
	w.body = append(w.body, 0, 0, 0, 0)
	return w.compress(SectionContent, start, ChunkFrameSize, chunk)
}

// compress appends payload, compressed, to the frame w.body holds from
// start, sets the frame's compressed length, and records the section.
func (w *Writer) compress(t SectionType, start, frameSize int, payload []byte) error {
	w.body = w.c.Compress(w.body, payload)
	n := len(w.body) - start - frameSize
	if uint64(n) > math.MaxUint32 {
		w.body = w.body[:start]
		return formatError(ErrPayload, t, -1, fmt.Sprintf("a compressed payload of %d bytes exceeds the u32 compressed_len", n))
	}
	binary.LittleEndian.PutUint32(w.body[start+frameSize-4:], uint32(n))
	w.added(t, w.body[start:])
	return nil
}

// AddRaw adds s as the next section, copying its Frame and Compressed
// byte for byte, as they are in the file s was read from. Its Payload and
// Chunk are not read, and nothing is compressed, so a hashed section adds
// to the change hash exactly what it added to the hash of that file. A
// CONTENT frame keeps its own chunk index.
//
// AddRaw fails as [Writer.Add] does, CONTENT aside, and with a
// *[FormatError] of kind [ErrInvalidHeader] when the Frame is not a frame
// of s.Type whose compressed length is len(s.Compressed).
func (w *Writer) AddRaw(s Section) error {
	if err := w.check(s.Type); err != nil {
		return err
	}
	t := s.Type
	if len(s.Frame) != t.FrameSize() || SectionType(s.Frame[0]) != t {
		return formatError(ErrInvalidHeader, t, -1, fmt.Sprintf("the frame is not a %d-byte %s frame", t.FrameSize(), t))
	}
	if n := binary.LittleEndian.Uint32(s.Frame[len(s.Frame)-4:]); uint64(n) != uint64(len(s.Compressed)) {
		return formatError(ErrInvalidHeader, t, -1, fmt.Sprintf("the frame gives a compressed length of %d, but the payload is %d bytes", n, len(s.Compressed)))
	}
	start := len(w.body)
	w.body = append(w.body, s.Frame...)
	w.body = append(w.body, s.Compressed...)
	w.added(t, w.body[start:])
	return nil
}

// check reports whether a section of type t can be added next.
func (w *Writer) check(t SectionType) error {
	if w.out != nil {
		return ErrWriterFinished
	}
	if !t.Known() {
		return formatError(ErrInvalidSectionType, 0, -1, fmt.Sprintf("0x%02x", uint8(t)))
	}
	if w.hasPrev && t.Order() < w.prev.Order() {
		return formatError(ErrUnexpectedSection, t, -1, fmt.Sprintf("%s after %s breaks the canonical section order", t, w.prev))
	}
	switch t {
	case SectionHeader, SectionDeps, SectionProvenance, SectionSignature, SectionSpan, SectionUnhashed:
		if w.counts[t.Order()] > 0 {
			return formatError(ErrUnexpectedSection, t, -1, fmt.Sprintf("duplicate %s section", t))
		}
	}
	if t == SectionSpan && w.version != Version3 {
		return formatError(ErrUnexpectedSection, t, -1, fmt.Sprintf("SPAN section in a version %d file", w.version))
	}
	return nil
}

// added records the section b, its frame and payload, of type t.
func (w *Writer) added(t SectionType, b []byte) {
	if t.Hashed() {
		w.hasher.Write(b)
	}
	w.counts[t.Order()]++
	w.prev, w.hasPrev = t, true
}

// Finish returns the change file and its change hash. It writes the
// header, with the flags and counts of the sections added, zero
// total_uncompressed and zero reserved bytes, then the table, the
// sections and the trailer. It fails with a *[FormatError] of kind
// [ErrInvalidHeader] when no HEADER or no DEPS section was added, or no
// SPAN section in version 3; the Writer is then as it was, and can take
// the missing section if the order allows it.
//
// Once Finish has returned a file, calling it again returns the same
// slice and hash, and every other method fails with [ErrWriterFinished].
// The Writer never changes the slice after returning it.
func (w *Writer) Finish() ([]byte, ident.Hash, error) {
	if w.out != nil {
		return w.out, w.hash, nil
	}
	required := []SectionType{SectionHeader, SectionDeps}
	if w.version == Version3 {
		required = append(required, SectionSpan)
	}
	for _, t := range required {
		if w.counts[t.Order()] == 0 {
			return nil, ident.Hash{}, formatError(ErrInvalidHeader, t, -1, fmt.Sprintf("a version %d file needs a %s section", w.version, t))
		}
	}
	count := func(t SectionType) uint32 { return uint32(w.counts[t.Order()]) }
	h := FileHeader{
		Version:          w.version,
		HashTableEntries: uint32(w.table.Len()),
		GraphSections:    count(SectionGraph),
		SemanticSections: count(SectionSemantic),
		ContentChunks:    count(SectionContent),
	}
	for t, f := range map[SectionType]Flags{
		SectionProvenance: FlagProvenance,
		SectionSemantic:   FlagSemantic,
		SectionUnhashed:   FlagUnhashed,
		SectionSignature:  FlagSignature,
	} {
		if w.counts[t.Order()] > 0 {
			h.Flags |= f
		}
	}
	size := HeaderSize + ident.Size*w.table.Len() + len(w.body) + TrailerSize
	out, _ := h.AppendBinary(make([]byte, 0, size))
	out, _ = w.table.AppendBinary(out)
	out = append(out, w.body...)
	w.hash = w.hasher.Sum()
	w.out = append(out, w.hash[:]...)
	w.body = nil
	return w.out, w.hash, nil
}

// ReplaceUnhashed returns the bytes of f with its section of type t set
// to payload, compressed with c, or removed when payload is nil. t must be
// [SectionSignature] or [SectionUnhashed], the two sections outside the
// change hash, so the file keeps f's change hash: this is how a change is
// signed, or annotated, after it is recorded. See
// docs/remote/change-format/container.md#signing.
//
// Every other section is copied byte for byte, as are the table, the
// trailer and every header field but the flag of t, which is set when
// payload is not nil and cleared when it is. This holds for a version 1
// file too, though a version 1 reader takes any section order: a new
// section goes where a version 2 file has it, before the first section
// that follows it in [SectionType.Order], or last. An empty, non-nil
// payload is written, as an empty section; nil is the only removal.
//
// ReplaceUnhashed fails with [ErrNilFile] for a nil f, with
// [ErrNilCompressor] for a nil c when payload is not nil, and with a
// *[FormatError] of kind:
//
//   - [ErrUnexpectedSection], for a t other than SIGNATURE or UNHASHED;
//   - [ErrInvalidHeader], when f holds a number of sections of t other
//     than its flag declares, as a version 1 file may, so the header
//     could not be kept true by setting the flag;
//   - [ErrPayload], when the compressed payload is longer than a frame
//     can say.
func ReplaceUnhashed(f *File, t SectionType, payload []byte, c Compressor) ([]byte, error) {
	if f == nil {
		return nil, ErrNilFile
	}
	var flag Flags
	switch t {
	case SectionSignature:
		flag = FlagSignature
	case SectionUnhashed:
		flag = FlagUnhashed
	default:
		return nil, formatError(ErrUnexpectedSection, t, -1, "only a SIGNATURE or UNHASHED section can change without changing the change hash")
	}
	if payload != nil && c == nil {
		return nil, ErrNilCompressor
	}
	var held uint64
	for _, s := range f.Sections {
		if s.Type == t {
			held++
		}
	}
	var declared uint64
	if f.Header.Flags.Has(flag) {
		declared = 1
	}
	if held != declared {
		return nil, formatError(ErrInvalidHeader, t, -1, fmt.Sprintf("the header declares %d %s sections, the file holds %d", declared, t, held))
	}

	var frame []byte
	if payload != nil {
		frame = c.Compress([]byte{byte(t), 0, 0, 0, 0}, payload)
		n := len(frame) - FrameSize
		if uint64(n) > math.MaxUint32 {
			return nil, formatError(ErrPayload, t, -1, fmt.Sprintf("a compressed payload of %d bytes exceeds the u32 compressed_len", n))
		}
		binary.LittleEndian.PutUint32(frame[1:], uint32(n))
	}

	h := f.Header
	h.Flags &^= flag
	if payload != nil {
		h.Flags |= flag
	}
	out, _ := h.AppendBinary(make([]byte, 0, len(f.Bytes())+len(frame)))
	out, _ = f.Table.AppendBinary(out)
	for _, s := range f.Sections {
		if s.Type == t {
			continue
		}
		if frame != nil && s.Type.Order() > t.Order() {
			out = append(out, frame...)
			frame = nil
		}
		out = append(out, s.Frame...)
		out = append(out, s.Compressed...)
	}
	out = append(out, frame...)
	return append(out, f.Hash[:]...), nil
}
