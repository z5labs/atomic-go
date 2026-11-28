package changefile

import (
	"encoding/binary"
	"fmt"

	"github.com/z5labs/atomic-go/ident"
)

// ReadOption changes what [NewReader], [Parse] and [Verify] check.
type ReadOption func(*readOptions)

type readOptions struct {
	allowTrailing bool
	checkChunks   bool
}

// AllowTrailingBytes accepts bytes after the trailer, as Atomic's
// Change::deserialize does. Without it a file with bytes after its trailer
// fails with [ErrTrailingBytes], as VerifiedChange::from_bytes does. The
// trailing bytes are never part of [File.Bytes].
func AllowTrailingBytes() ReadOption {
	return func(o *readOptions) { o.allowTrailing = true }
}

// CheckChunks checks each CONTENT frame against its chunk, which Atomic's
// reader does not: that chunk indices count up from 0 in file order, and
// that the decompressed chunk has the frame's length and BLAKE3 hash. A
// mismatch fails with [ErrChunkMismatch].
//
// [Verify] decompresses nothing, so under Verify only the chunk indices are
// checked.
func CheckChunks() ReadOption {
	return func(o *readOptions) { o.checkChunks = true }
}

// File is a change file that has been read and verified.
type File struct {
	// Header is the file's 64-byte header.
	Header FileHeader

	// Table is the file's hash dedup table.
	Table HashTable

	// Sections are the file's sections in file order, each with its
	// Payload decompressed. In a version 1 file a repeated section is
	// there as often as it is repeated.
	Sections []Section

	// Hash is the change hash: the file's trailer, verified.
	Hash ident.Hash

	bytes []byte
}

// Bytes returns the bytes the file was read from, from the magic to the
// end of the trailer, which are the bytes Hash names. Any trailing bytes
// allowed by [AllowTrailingBytes] are not included. The slice shares memory
// with the slice the file was read from, and must not be modified.
func (f *File) Bytes() []byte {
	return f.bytes
}

// Content returns the change's content blob: the payloads of its CONTENT
// sections, concatenated in file order. It returns nil for a change with no
// content.
func (f *File) Content() []byte {
	var out []byte
	for _, s := range f.Sections {
		if s.Type == SectionContent {
			out = append(out, s.Payload...)
		}
	}
	return out
}

// Reader reads a change file held in memory one section at a time, in the
// order and with the checks of Atomic's Change::deserialize:
// docs/remote/change-format/container.md#reader-validation.
//
// [NewReader] checks the magic, the header and the hash table. Each call to
// [Reader.Next] reads, hashes and decompresses one section and checks its
// place in the file. [Reader.Finish] checks the trailer and what is left.
// Decoding each payload is the caller's, and a caller that decodes a
// section's payload before calling Next again reports the first error in
// file order, as Atomic does.
//
// A Reader does not copy the bytes it reads: the Frame and Compressed of
// each [Section] share memory with them, and they must not be modified
// while the Reader or anything it returned is in use. A Reader is not safe
// for concurrent use.
type Reader struct {
	b      []byte
	dec    Decompressor
	opts   readOptions
	header FileHeader
	table  HashTable
	hasher *ident.Hasher

	// pos is the offset of the next section's frame, or of the trailer.
	pos int

	// want is the number of sections the header frames, and read the
	// number read so far.
	want, read uint64

	// counts holds the sections read so far of each type, by
	// [SectionType.Order].
	counts [9]uint64

	// prev is the last section read, for the order check.
	prev    SectionType
	hasPrev bool

	sections []Section
	done     bool
	err      error
	file     *File
}

// NewReader returns a Reader of the change file b, whose payloads it
// decompresses with dec. It reads the magic, the header and the hash table,
// and fails with a *[FormatError] of kind:
//
//   - [ErrTruncated], when b ends before the end of the table;
//   - [ErrLegacyFormat], when b starts with the little-endian u32 1 or 2,
//     as a pre-V3 bincode file does;
//   - [ErrInvalidMagic], when b starts with anything else but [Magic];
//   - [ErrUnsupportedVersion] or [ErrInvalidHeader], as
//     [FileHeader.Validate] says;
//   - [ErrInvalidHeader], for a repeated table entry.
//
// A nil dec is refused with [ErrNilDecompressor]; [Verify] is the way to
// read a file without decompressing it.
func NewReader(b []byte, dec Decompressor, opts ...ReadOption) (*Reader, error) {
	if dec == nil {
		return nil, ErrNilDecompressor
	}
	return newReader(b, dec, opts)
}

// newReader is [NewReader] without the nil check: a nil dec decompresses
// nothing, which is what [Verify] needs.
func newReader(b []byte, dec Decompressor, opts []ReadOption) (*Reader, error) {
	var o readOptions
	for _, opt := range opts {
		opt(&o)
	}
	if len(b) >= len(Magic) && string(b[:len(Magic)]) != Magic {
		if v := binary.LittleEndian.Uint32(b); v == 1 || v == 2 {
			return nil, formatError(ErrLegacyFormat, 0, 0, fmt.Sprintf("file starts with the little-endian u32 %d, not %q; it must be re-recorded", v, Magic))
		}
	}
	h, err := ParseFileHeader(b)
	if err != nil {
		return nil, err
	}
	if err := h.Validate(); err != nil {
		return nil, err
	}
	end := HeaderSize + int(h.HashTableEntries)*ident.Size
	if len(b) < end {
		return nil, formatError(ErrTruncated, 0, int64(len(b)), fmt.Sprintf("hash dedup table of %d entries needs %d bytes, got %d", h.HashTableEntries, end-HeaderSize, len(b)-HeaderSize))
	}
	entries := make([]ident.Hash, h.HashTableEntries)
	for i := range entries {
		copy(entries[i][:], b[HeaderSize+i*ident.Size:])
	}
	table, err := NewHashTable(entries)
	if err != nil {
		return nil, err
	}
	hasher := ident.NewHasher()
	hasher.Write(b[HeaderSize:end])
	return &Reader{
		b:      b,
		dec:    dec,
		opts:   o,
		header: h,
		table:  table,
		hasher: hasher,
		pos:    end,
		want:   h.SectionCount(),
	}, nil
}

// Header returns the file's header.
func (r *Reader) Header() FileHeader {
	return r.header
}

// Table returns the file's hash dedup table.
func (r *Reader) Table() HashTable {
	return r.table
}

// Next reads the next section and returns it, with its Payload
// decompressed, and true. It returns false and a nil error once the header's
// count of sections has been read, or when the file ends exactly where a
// section would start; [Reader.Finish] then checks the trailer.
//
// It fails with a *[FormatError] of kind:
//
//   - [ErrInvalidSectionType], for a type byte that is not
//     [SectionType.Known];
//   - [ErrTruncated], when the file ends inside the frame or the payload;
//   - [ErrDecompress], when the payload will not decompress;
//   - [ErrUnexpectedSection], for a SPAN section outside version 3 or a
//     second SPAN section, and, in version 2 and 3 files only, a section
//     before the one it follows in [SectionType.Order] or a second HEADER,
//     DEPS, PROVENANCE or UNHASHED section;
//   - [ErrChunkMismatch], under [CheckChunks].
//
// A section whose compressed length is 0 has an empty Payload, and the
// decompressor is not called for it. A version 1 file's checks are only the
// framing, decompression and SPAN ones, so every section it repeats is
// returned; applying the last of each is the caller's.
//
// After an error, Next and [Reader.Finish] return the same error.
func (r *Reader) Next() (Section, bool, error) {
	if r.err != nil {
		return Section{}, false, r.err
	}
	if r.done || r.read == r.want || r.pos == len(r.b) {
		r.done = true
		return Section{}, false, nil
	}
	s, err := r.next()
	if err != nil {
		r.err = err
		return Section{}, false, err
	}
	r.read++
	r.sections = append(r.sections, s)
	return s, true, nil
}

func (r *Reader) next() (Section, error) {
	off := r.pos
	t := SectionType(r.b[off])
	if !t.Known() {
		return Section{}, formatError(ErrInvalidSectionType, 0, int64(off), fmt.Sprintf("0x%02x", uint8(t)))
	}
	fs := t.FrameSize()
	if len(r.b)-off < fs {
		return Section{}, formatError(ErrTruncated, t, int64(len(r.b)), fmt.Sprintf("section frame needs %d bytes, got %d", fs, len(r.b)-off))
	}
	frame := r.b[off : off+fs : off+fs]
	le := binary.LittleEndian
	var n uint32
	var chunk ChunkInfo
	if t == SectionContent {
		chunk.Index = le.Uint32(frame[1:])
		copy(chunk.Hash[:], frame[5:37])
		chunk.UncompressedLen = le.Uint32(frame[37:])
		n = le.Uint32(frame[41:])
	} else {
		n = le.Uint32(frame[1:])
	}
	start := off + fs
	if uint64(len(r.b)-start) < uint64(n) {
		return Section{}, formatError(ErrTruncated, t, int64(len(r.b)), fmt.Sprintf("section payload needs %d bytes, got %d", n, len(r.b)-start))
	}
	end := start + int(n)
	s := Section{
		Type:       t,
		Offset:     int64(off),
		Frame:      frame,
		Compressed: r.b[start:end:end],
		Chunk:      chunk,
	}
	if t.Hashed() {
		r.hasher.Write(s.Frame)
		r.hasher.Write(s.Compressed)
	}
	r.pos = end
	if r.dec != nil {
		s.Payload = []byte{}
		if n > 0 {
			p, err := r.dec.Decompress(nil, s.Compressed)
			if err != nil {
				return Section{}, &FormatError{Kind: ErrDecompress, Section: t, Offset: s.Offset, Err: err}
			}
			if p != nil {
				s.Payload = p
			}
		}
	}
	if err := r.checkPlace(s); err != nil {
		return Section{}, err
	}
	if r.opts.checkChunks && t == SectionContent {
		if err := r.checkChunk(s); err != nil {
			return Section{}, err
		}
	}
	r.counts[t.Order()]++
	r.prev, r.hasPrev = t, true
	return s, nil
}

// checkPlace applies the order, duplicate and SPAN rules of
// docs/remote/change-format/container.md#reader-validation step 6, and
// version 1's SPAN refusal.
func (r *Reader) checkPlace(s Section) error {
	t := s.Type
	if r.header.Version != Version1 {
		if r.hasPrev && t.Order() < r.prev.Order() {
			return formatError(ErrUnexpectedSection, t, s.Offset, fmt.Sprintf("%s after %s breaks the canonical section order", t, r.prev))
		}
		switch t {
		case SectionHeader, SectionDeps, SectionProvenance, SectionUnhashed:
			if r.counts[t.Order()] > 0 {
				return formatError(ErrUnexpectedSection, t, s.Offset, fmt.Sprintf("duplicate %s section", t))
			}
		}
	}
	if t == SectionSpan {
		if r.header.Version != Version3 {
			return formatError(ErrUnexpectedSection, t, s.Offset, fmt.Sprintf("SPAN section in a version %d file", r.header.Version))
		}
		if r.counts[t.Order()] > 0 {
			return formatError(ErrUnexpectedSection, t, s.Offset, "duplicate SPAN section")
		}
	}
	return nil
}

// checkChunk checks a CONTENT frame against its chunk, for [CheckChunks].
// A section read by [Verify] has no payload, so only its index is checked.
func (r *Reader) checkChunk(s Section) error {
	want := r.counts[SectionContent.Order()]
	if uint64(s.Chunk.Index) != want {
		return formatError(ErrChunkMismatch, SectionContent, s.Offset, fmt.Sprintf("chunk_index is %d, expected %d", s.Chunk.Index, want))
	}
	if s.Payload == nil {
		return nil
	}
	if uint64(len(s.Payload)) != uint64(s.Chunk.UncompressedLen) {
		return formatError(ErrChunkMismatch, SectionContent, s.Offset, fmt.Sprintf("uncompressed_len is %d, but the chunk is %d bytes", s.Chunk.UncompressedLen, len(s.Payload)))
	}
	if got := ident.Sum(s.Payload); got != s.Chunk.Hash {
		return formatError(ErrChunkMismatch, SectionContent, s.Offset, fmt.Sprintf("chunk_hash is %s, but the chunk hashes to %s", s.Chunk.Hash, got))
	}
	return nil
}

// Finish reads any sections [Reader.Next] has not, then checks the rest of
// the file and returns it. It fails as Next does, and:
//
//   - with a *[FormatError] of kind [ErrTruncated], when the trailer is
//     missing or short;
//   - with a *[HashMismatchError], when the trailer is not the change hash
//     of the table and the hashed sections;
//   - in a version 2 or 3 file, with a *FormatError of kind
//     [ErrInvalidHeader], when the sections read do not match the header's
//     flags and counts;
//   - in a version 1 file, with a *FormatError of kind [ErrInvalidHeader],
//     when there is no HEADER section;
//   - with a *FormatError of kind [ErrTrailingBytes], for bytes after the
//     trailer, unless [AllowTrailingBytes] was given.
//
// Calling Finish again returns the same result.
func (r *Reader) Finish() (*File, error) {
	if r.file != nil {
		return r.file, nil
	}
	for {
		_, ok, err := r.Next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
	}
	f, err := r.finish()
	if err != nil {
		r.err = err
		return nil, err
	}
	r.file = f
	return f, nil
}

func (r *Reader) finish() (*File, error) {
	if len(r.b)-r.pos < TrailerSize {
		return nil, formatError(ErrTruncated, 0, int64(len(r.b)), fmt.Sprintf("trailer needs %d bytes, got %d", TrailerSize, len(r.b)-r.pos))
	}
	var trailer ident.Hash
	copy(trailer[:], r.b[r.pos:])
	if computed := r.hasher.Sum(); computed != trailer {
		return nil, &HashMismatchError{Trailer: trailer, Computed: computed}
	}
	end := r.pos + TrailerSize
	if r.header.Version == Version1 {
		if r.counts[SectionHeader.Order()] == 0 {
			return nil, formatError(ErrInvalidHeader, 0, -1, "missing legacy version 1 HEADER section")
		}
	} else if err := r.checkManifest(); err != nil {
		return nil, err
	}
	if !r.opts.allowTrailing && end < len(r.b) {
		return nil, formatError(ErrTrailingBytes, 0, int64(end), fmt.Sprintf("%d bytes after the trailer", len(r.b)-end))
	}
	return &File{
		Header:   r.header,
		Table:    r.table,
		Sections: r.sections,
		Hash:     trailer,
		bytes:    r.b[:end:end],
	}, nil
}

// checkManifest compares the sections a version 2 or 3 file holds with its
// header, as Atomic's reader does, every type but SIGNATURE.
func (r *Reader) checkManifest() error {
	h := r.header
	one := func(f Flags) uint64 {
		if h.Flags.Has(f) {
			return 1
		}
		return 0
	}
	var span uint64
	if h.Version == Version3 {
		span = 1
	}
	for _, c := range []struct {
		t    SectionType
		want uint64
	}{
		{SectionHeader, 1},
		{SectionDeps, 1},
		{SectionProvenance, one(FlagProvenance)},
		{SectionGraph, uint64(h.GraphSections)},
		{SectionSpan, span},
		{SectionSemantic, uint64(h.SemanticSections)},
		{SectionContent, uint64(h.ContentChunks)},
		{SectionUnhashed, one(FlagUnhashed)},
	} {
		if got := r.counts[c.t.Order()]; got != c.want {
			return formatError(ErrInvalidHeader, c.t, -1, fmt.Sprintf("section manifest does not match the file header: the header declares %d %s sections, the file holds %d", c.want, c.t, got))
		}
	}
	return nil
}

// Parse reads and verifies the whole change file b, decompressing each
// payload with dec. It is [NewReader] followed by [Reader.Finish], and fails
// as they do.
func Parse(b []byte, dec Decompressor, opts ...ReadOption) (*File, error) {
	r, err := NewReader(b, dec, opts...)
	if err != nil {
		return nil, err
	}
	return r.Finish()
}

// Verify checks the change file b and returns its change hash, decompressing
// nothing. It checks everything [Parse] does but what needs a payload: the
// magic, header and table, the framing, the order, duplicate and SPAN rules,
// the trailer, the section counts and any trailing bytes. A payload that
// would not decompress, or would not decode, is not found. This is how a
// Change object's key is checked cheaply against its bytes.
func Verify(b []byte, opts ...ReadOption) (ident.Hash, error) {
	r, err := newReader(b, nil, opts)
	if err != nil {
		return ident.Hash{}, err
	}
	f, err := r.Finish()
	if err != nil {
		return ident.Hash{}, err
	}
	return f.Hash, nil
}
