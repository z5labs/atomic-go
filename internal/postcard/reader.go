package postcard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"
)

// byteReader is what a Reader reads from.
type byteReader interface {
	io.Reader
	io.ByteReader
}

// Reader decodes postcard values from an [io.Reader], one at a time, so a
// stream such as a sync pack can be decoded object by object without being
// held in memory whole.
//
// Like a [Decoder], its first error is sticky. A failure to read is an
// *[Error] whose Err wraps the underlying reader's error, so it can be
// tested with [errors.Is]; input that ends inside a value is also
// [ErrUnexpectedEnd].
//
// Every length and count is checked against the limit given to
// [NewReader] before anything is allocated for it. A byte sequence or
// string within the limit is not allocated whole up front either: its
// buffer grows as the stream delivers bytes, so a length the stream does
// not back costs only what the stream actually holds.
type Reader struct {
	src    byteReader
	maxLen int
	off    int64
	err    error
}

// NewReader returns a Reader decoding from r. A byte sequence, string or
// sequence whose declared length or count is above maxLen fails with
// [ErrTooLong]. maxLen must not be negative.
//
// If r is not an [io.ByteReader], the Reader buffers it and may read past
// the last value it decodes.
func NewReader(r io.Reader, maxLen int) *Reader {
	if maxLen < 0 {
		panic("postcard: negative maxLen")
	}
	src, ok := r.(byteReader)
	if !ok {
		src = bufio.NewReader(r)
	}
	return &Reader{src: src, maxLen: maxLen}
}

// Err returns the first failure, an *[Error], or nil if there has been none.
func (r *Reader) Err() error {
	return r.err
}

// Offset returns the number of bytes read from the underlying reader so
// far. Unlike a [Decoder], which rewinds to the start of a value that fails,
// a Reader cannot give bytes back to its stream, so after a failure Offset
// counts the bytes of the failed value it consumed too. The *[Error]'s
// Offset is where that value begins, for both.
func (r *Reader) Offset() int64 {
	return r.off
}

func (r *Reader) failAt(off int64, err error) {
	if r.err == nil {
		r.err = &Error{Offset: off, Err: err}
	}
}

// readError makes the end of the input an [ErrUnexpectedEnd], keeping the
// reader's own error reachable.
func readError(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", ErrUnexpectedEnd, err)
	}
	return err
}

func (r *Reader) byte(start int64) (byte, bool) {
	if r.err != nil {
		return 0, false
	}
	c, err := r.src.ReadByte()
	if err != nil {
		r.failAt(start, readError(err))
		return 0, false
	}
	r.off++
	return c, true
}

func (r *Reader) varint(maxBytes int, lastMax byte) (uint64, bool) {
	start := r.off
	var v uint64
	for i := 0; i < maxBytes; i++ {
		c, ok := r.byte(start)
		if !ok {
			return 0, false
		}
		if i == maxBytes-1 && c > lastMax {
			r.failAt(start, ErrBadVarint)
			return 0, false
		}
		v |= uint64(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, true
		}
	}
	// Unreachable: the last permitted byte either fails or ends the loop.
	r.failAt(start, ErrBadVarint)
	return 0, false
}

// U32 decodes a u32 varint of at most 5 bytes.
func (r *Reader) U32() uint32 {
	v, _ := r.varint(maxVarintU32, lastByteU32)
	return uint32(v)
}

// U64 decodes a u64 varint of at most 10 bytes.
func (r *Reader) U64() uint64 {
	v, _ := r.varint(maxVarintU64, lastByteU64)
	return v
}

// tag decodes a one-byte 0 or 1 tag, failing with bad otherwise.
func (r *Reader) tag(bad error) bool {
	start := r.off
	c, ok := r.byte(start)
	if !ok {
		return false
	}
	switch c {
	case 0:
		return false
	case 1:
		return true
	}
	r.failAt(start, bad)
	return false
}

// Bool decodes a bool; see [Decoder.Bool].
func (r *Reader) Bool() bool {
	return r.tag(ErrBadBool)
}

// OptionTag decodes an Option's tag; see [Decoder.OptionTag].
func (r *Reader) OptionTag() bool {
	return r.tag(ErrBadOption)
}

// length decodes a usize length or count and checks it against maxLen.
func (r *Reader) length() (int, bool) {
	start := r.off
	n, ok := r.varint(maxVarintU64, lastByteU64)
	if !ok {
		return 0, false
	}
	if n > uint64(r.maxLen) {
		r.failAt(start, ErrTooLong)
		return 0, false
	}
	return int(n), true
}

// SeqLen decodes the element count of a sequence. A count above the
// Reader's limit is [ErrTooLong].
func (r *Reader) SeqLen() int {
	n, _ := r.length()
	return n
}

// Variant decodes an enum's variant index; see [Decoder.Variant].
func (r *Reader) Variant(n uint32) uint32 {
	start := r.off
	v, ok := r.varint(maxVarintU32, lastByteU32)
	if !ok {
		return 0
	}
	if uint32(v) >= n {
		r.failAt(start, ErrUnknownVariant)
		return 0
	}
	return uint32(v)
}

// ByteSeq decodes a byte sequence into a new slice. A length above the
// Reader's limit is [ErrTooLong], and nothing is allocated for it.
func (r *Reader) ByteSeq() []byte {
	b, _ := r.byteSeq()
	return b
}

func (r *Reader) byteSeq() ([]byte, int64) {
	start := r.off
	n, ok := r.length()
	if !ok {
		return nil, start
	}
	b, err := r.readN(n)
	if err != nil {
		r.failAt(start, readError(err))
		return nil, start
	}
	return b, start
}

// readStep is the most a Reader allocates for a byte sequence before the
// stream has delivered any of it.
const readStep = 64 << 10

// readN reads exactly n bytes into a new slice. Above readStep, the slice
// grows, at most doubling, as the bytes arrive, so a declared length the
// stream does not back cannot make the Reader allocate it.
func (r *Reader) readN(n int) ([]byte, error) {
	b := make([]byte, 0, min(n, readStep))
	for len(b) < n {
		if len(b) == cap(b) {
			b = slices.Grow(b, min(cap(b), n-len(b)))
		}
		m, err := io.ReadFull(r.src, b[len(b):min(cap(b), n)])
		b = b[:len(b)+m]
		r.off += int64(m)
		if err != nil {
			if err == io.EOF && len(b) > 0 {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
	return b, nil
}

// Str decodes a string; see [Decoder.Str]. A length above the Reader's
// limit is [ErrTooLong].
func (r *Reader) Str() string {
	b, start := r.byteSeq()
	if r.err != nil {
		return ""
	}
	if !utf8.Valid(b) {
		r.failAt(start, ErrBadUTF8)
		return ""
	}
	return string(b)
}
