package postcard

import (
	"encoding/binary"
	"math"
	"unicode/utf8"
)

// Decoder decodes postcard values from a byte slice, one field at a time.
//
// Its first error is sticky: once a method fails, [Decoder.Err] reports
// that failure, the offset stops moving, and every later method returns its
// type's zero value. A payload can therefore be decoded field by field with
// a single error check at the end.
//
// The byte slices returned by [Decoder.ByteSeq] and [Decoder.Fixed] alias
// the input; copy them to keep them past the input's lifetime.
type Decoder struct {
	b   []byte
	off int
	err error
}

// NewDecoder returns a Decoder reading b from its start. It does not copy b.
func NewDecoder(b []byte) *Decoder {
	return &Decoder{b: b}
}

// Err returns the first failure, an *[Error], or nil if there has been none.
func (d *Decoder) Err() error {
	return d.err
}

// Offset returns the number of bytes decoded so far.
func (d *Decoder) Offset() int64 {
	return int64(d.off)
}

// Remaining returns the number of bytes not yet decoded.
func (d *Decoder) Remaining() int {
	return len(d.b) - d.off
}

// Fail records err as the Decoder's failure, at the current offset, unless
// it has already failed. It is how a caller reports a rule the codec cannot
// know, such as a length a field does not allow ([ErrBadLength]). A nil err
// is ignored.
func (d *Decoder) Fail(err error) {
	if err != nil {
		d.failAt(d.off, err)
	}
}

func (d *Decoder) failAt(off int, err error) {
	if d.err == nil {
		d.err = &Error{Offset: int64(off), Err: err}
	}
}

// take returns the next n bytes and advances past them.
func (d *Decoder) take(n int) ([]byte, bool) {
	if d.err != nil {
		return nil, false
	}
	if n > d.Remaining() {
		d.failAt(d.off, ErrUnexpectedEnd)
		return nil, false
	}
	b := d.b[d.off : d.off+n : d.off+n]
	d.off += n
	return b, true
}

func (d *Decoder) varint(maxBytes int, lastMax byte) (uint64, bool) {
	if d.err != nil {
		return 0, false
	}
	start := d.off
	var v uint64
	for i := 0; i < maxBytes; i++ {
		if start+i >= len(d.b) {
			d.failAt(start, ErrUnexpectedEnd)
			return 0, false
		}
		c := d.b[start+i]
		if i == maxBytes-1 && c > lastMax {
			// lastMax is below 0x80, so this also refuses a last byte
			// with the continuation bit set.
			d.failAt(start, ErrBadVarint)
			return 0, false
		}
		v |= uint64(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			d.off = start + i + 1
			return v, true
		}
	}
	// Unreachable: the last permitted byte either fails or ends the loop.
	d.failAt(start, ErrBadVarint)
	return 0, false
}

// U8 decodes a u8.
func (d *Decoder) U8() uint8 {
	b, ok := d.take(1)
	if !ok {
		return 0
	}
	return b[0]
}

// I8 decodes an i8, which is one byte of two's complement.
func (d *Decoder) I8() int8 {
	return int8(d.U8())
}

// U16 decodes a u16 varint of at most 3 bytes.
func (d *Decoder) U16() uint16 {
	v, _ := d.varint(maxVarintU16, lastByteU16)
	return uint16(v)
}

// U32 decodes a u32 varint of at most 5 bytes.
func (d *Decoder) U32() uint32 {
	v, _ := d.varint(maxVarintU32, lastByteU32)
	return uint32(v)
}

// U64 decodes a u64 varint of at most 10 bytes. A usize is encoded the same
// way.
func (d *Decoder) U64() uint64 {
	v, _ := d.varint(maxVarintU64, lastByteU64)
	return v
}

// I16 decodes a zigzag-encoded i16.
func (d *Decoder) I16() int16 {
	return zigzag16(d.U16())
}

// I32 decodes a zigzag-encoded i32.
func (d *Decoder) I32() int32 {
	return zigzag32(d.U32())
}

// I64 decodes a zigzag-encoded i64.
func (d *Decoder) I64() int64 {
	return zigzag64(d.U64())
}

// Bool decodes a bool: 0x00 is false, 0x01 is true, and anything else is
// [ErrBadBool].
func (d *Decoder) Bool() bool {
	start := d.off
	b, ok := d.take(1)
	if !ok {
		return false
	}
	switch b[0] {
	case 0:
		return false
	case 1:
		return true
	}
	d.off = start
	d.failAt(start, ErrBadBool)
	return false
}

// F32 decodes an f32: 4 bytes of little-endian IEEE 754 bits.
func (d *Decoder) F32() float32 {
	b, ok := d.take(4)
	if !ok {
		return 0
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

// F64 decodes an f64: 8 bytes of little-endian IEEE 754 bits.
func (d *Decoder) F64() float64 {
	b, ok := d.take(8)
	if !ok {
		return 0
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b))
}

// length decodes a usize length or count and checks it against the bytes
// left, on the assumption that every element takes at least one byte.
func (d *Decoder) length() (int, bool) {
	start := d.off
	n, ok := d.varint(maxVarintU64, lastByteU64)
	if !ok {
		return 0, false
	}
	if n > uint64(d.Remaining()) {
		d.off = start
		d.failAt(start, ErrUnexpectedEnd)
		return 0, false
	}
	return int(n), true
}

// ByteSeq decodes a byte sequence (Vec<u8>, &[u8]): a varint length, then
// that many bytes. The result aliases the input.
func (d *Decoder) ByteSeq() []byte {
	start := d.off
	n, ok := d.length()
	if !ok {
		return nil
	}
	b, ok := d.take(n)
	if !ok {
		d.off = start
		return nil
	}
	return b
}

// Str decodes a string: a varint length, then that many bytes of UTF-8.
// Bytes that are not valid UTF-8 are [ErrBadUTF8].
func (d *Decoder) Str() string {
	start := d.off
	b := d.ByteSeq()
	if d.err != nil {
		return ""
	}
	if !utf8.Valid(b) {
		d.off = start
		d.failAt(start, ErrBadUTF8)
		return ""
	}
	return string(b)
}

// Fixed decodes a fixed-size array of n bytes ([u8; N]), which has no
// length prefix. The result aliases the input. A negative n is
// [ErrBadLength].
func (d *Decoder) Fixed(n int) []byte {
	if n < 0 {
		d.Fail(ErrBadLength)
		return nil
	}
	b, _ := d.take(n)
	return b
}

// Array32 decodes a [u8; 32], such as a hash.
func (d *Decoder) Array32() [32]byte {
	var out [32]byte
	if b, ok := d.take(len(out)); ok {
		copy(out[:], b)
	}
	return out
}

// OptionTag decodes the tag of an Option: false for None (0x00), true for
// Some (0x01), after which the caller decodes the value. Any other byte is
// [ErrBadOption].
func (d *Decoder) OptionTag() bool {
	start := d.off
	b, ok := d.take(1)
	if !ok {
		return false
	}
	switch b[0] {
	case 0:
		return false
	case 1:
		return true
	}
	d.off = start
	d.failAt(start, ErrBadOption)
	return false
}

// SeqLen decodes the element count of a sequence (Vec<T>, &[T]). Every
// element of every type this package decodes takes at least one byte, so a
// count above [Decoder.Remaining] fails at once with [ErrUnexpectedEnd],
// before the caller allocates anything for it.
func (d *Decoder) SeqLen() int {
	n, _ := d.length()
	return n
}

// Variant decodes an enum's variant index, a u32 varint giving the
// variant's position in the declaration. An index of n or above, where n is
// the number of variants the enum declares, is [ErrUnknownVariant].
func (d *Decoder) Variant(n uint32) uint32 {
	start := d.off
	v, ok := d.varint(maxVarintU32, lastByteU32)
	if !ok {
		return 0
	}
	if uint32(v) >= n {
		d.off = start
		d.failAt(start, ErrUnknownVariant)
		return 0
	}
	return uint32(v)
}

// Option decodes an Option<T>, calling decode for the value of a Some. It
// returns nil for None or on failure.
func Option[T any](d *Decoder, decode func(*Decoder) T) *T {
	if !d.OptionTag() {
		return nil
	}
	v := decode(d)
	if d.err != nil {
		return nil
	}
	return &v
}

// Seq decodes a Vec<T>, calling decode for each element. It returns nil on
// failure. A count larger than the input fails before anything is
// allocated; see [Decoder.SeqLen].
func Seq[T any](d *Decoder, decode func(*Decoder) T) []T {
	n := d.SeqLen()
	if d.err != nil {
		return nil
	}
	out := make([]T, 0, n)
	for range n {
		v := decode(d)
		if d.err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}

// Decode decodes one value from b with decode. Like postcard::from_bytes,
// which every Atomic payload is read with except SPAN, it ignores any bytes
// left over after the value.
func Decode[T any](b []byte, decode func(*Decoder) T) (T, error) {
	d := NewDecoder(b)
	v := decode(d)
	if err := d.Err(); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// DecodeExact is [Decode], except that bytes left over after the value are
// [ErrTrailingBytes].
func DecodeExact[T any](b []byte, decode func(*Decoder) T) (T, error) {
	d := NewDecoder(b)
	v := decode(d)
	if d.err == nil && d.Remaining() > 0 {
		d.Fail(ErrTrailingBytes)
	}
	if err := d.Err(); err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}
