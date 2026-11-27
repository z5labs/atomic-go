package postcard

import (
	"encoding/binary"
	"math"
)

// Encoder writes postcard values to a byte slice, one field at a time,
// always in their minimal encoding. Atomic hashes encoded payloads, so the
// minimal encoding is the only one a writer may emit.
type Encoder struct {
	buf []byte
}

// NewEncoder returns an Encoder that appends to buf, which may be nil.
func NewEncoder(buf []byte) *Encoder {
	return &Encoder{buf: buf}
}

// Bytes returns the encoded bytes: buf as given to [NewEncoder], followed by
// everything written since. The slice aliases the Encoder's buffer.
func (e *Encoder) Bytes() []byte {
	return e.buf
}

func (e *Encoder) varint(v uint64) {
	e.buf = binary.AppendUvarint(e.buf, v)
}

// U8 writes a u8.
func (e *Encoder) U8(v uint8) {
	e.buf = append(e.buf, v)
}

// I8 writes an i8.
func (e *Encoder) I8(v int8) {
	e.U8(uint8(v))
}

// U16 writes a u16 varint.
func (e *Encoder) U16(v uint16) {
	e.varint(uint64(v))
}

// U32 writes a u32 varint.
func (e *Encoder) U32(v uint32) {
	e.varint(uint64(v))
}

// U64 writes a u64 varint.
func (e *Encoder) U64(v uint64) {
	e.varint(v)
}

// I16 writes a zigzag-encoded i16.
func (e *Encoder) I16(v int16) {
	e.varint(uint64(unzigzag16(v)))
}

// I32 writes a zigzag-encoded i32.
func (e *Encoder) I32(v int32) {
	e.varint(uint64(unzigzag32(v)))
}

// I64 writes a zigzag-encoded i64.
func (e *Encoder) I64(v int64) {
	e.varint(unzigzag64(v))
}

// Bool writes a bool as 0x00 or 0x01.
func (e *Encoder) Bool(v bool) {
	if v {
		e.U8(1)
		return
	}
	e.U8(0)
}

// F32 writes an f32 as 4 bytes of little-endian IEEE 754 bits.
func (e *Encoder) F32(v float32) {
	e.buf = binary.LittleEndian.AppendUint32(e.buf, math.Float32bits(v))
}

// F64 writes an f64 as 8 bytes of little-endian IEEE 754 bits.
func (e *Encoder) F64(v float64) {
	e.buf = binary.LittleEndian.AppendUint64(e.buf, math.Float64bits(v))
}

// ByteSeq writes a byte sequence: its length, then its bytes.
func (e *Encoder) ByteSeq(b []byte) {
	e.varint(uint64(len(b)))
	e.buf = append(e.buf, b...)
}

// Str writes a string: its byte length, then its bytes. s should be valid
// UTF-8, since a decoder refuses anything else.
func (e *Encoder) Str(s string) {
	e.varint(uint64(len(s)))
	e.buf = append(e.buf, s...)
}

// Fixed writes a fixed-size byte array, with no length prefix.
func (e *Encoder) Fixed(b []byte) {
	e.buf = append(e.buf, b...)
}

// Array32 writes a [u8; 32], such as a hash.
func (e *Encoder) Array32(a [32]byte) {
	e.buf = append(e.buf, a[:]...)
}

// OptionTag writes an Option's tag: 0x01 for Some, after which the caller
// writes the value, or 0x00 for None.
func (e *Encoder) OptionTag(some bool) {
	e.Bool(some)
}

// SeqLen writes a sequence's element count. A negative n panics.
func (e *Encoder) SeqLen(n int) {
	if n < 0 {
		panic("postcard: negative sequence length")
	}
	e.varint(uint64(n))
}

// Variant writes an enum's variant index: the variant's position in the
// declaration, starting at 0, and not any Rust discriminant.
func (e *Encoder) Variant(index uint32) {
	e.varint(uint64(index))
}

// PutOption writes an Option<T>: None for a nil v, otherwise Some followed
// by put's encoding of *v.
func PutOption[T any](e *Encoder, v *T, put func(*Encoder, T)) {
	e.OptionTag(v != nil)
	if v != nil {
		put(e, *v)
	}
}

// PutSeq writes a Vec<T>: the element count, then put's encoding of each
// element.
func PutSeq[T any](e *Encoder, s []T, put func(*Encoder, T)) {
	e.SeqLen(len(s))
	for _, v := range s {
		put(e, v)
	}
}
