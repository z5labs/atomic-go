package ident

const base32Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

// EncodeBase32 returns the upper-case, unpadded RFC 4648 base32 of b. The
// last character's unused low bits are zero. A [Size]-byte value encodes to
// [Base32Len] characters.
func EncodeBase32(b []byte) string {
	return string(AppendBase32(nil, b))
}

// AppendBase32 appends the upper-case, unpadded RFC 4648 base32 of b to dst
// and returns the extended slice.
func AppendBase32(dst, b []byte) []byte {
	var acc uint16
	bits := 0
	for _, c := range b {
		acc = acc<<8 | uint16(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			dst = append(dst, base32Alphabet[(acc>>bits)&0x1f])
		}
	}
	if bits > 0 {
		dst = append(dst, base32Alphabet[(acc<<(5-bits))&0x1f])
	}
	return dst
}

// base32Value returns the value of the base32 digit c, which is upper case
// only unless anyCase is set.
func base32Value(c byte, anyCase bool) (byte, bool) {
	switch {
	case 'A' <= c && c <= 'Z':
		return c - 'A', true
	case anyCase && 'a' <= c && c <= 'z':
		return c - 'a', true
	case '2' <= c && c <= '7':
		return c - '2' + 26, true
	}
	return 0, false
}

// DecodeBase32 decodes the base32 text of a [Size]-byte value the way
// atomic-core's Base32 codec does, for change hashes, Merkle states and
// SetIds:
//
//   - either case is accepted;
//   - the input must be exactly [Base32Len] characters;
//   - the four unused low bits of the last character are ignored, so a last
//     character other than 'A' or 'Q' still decodes;
//   - any byte outside the alphabet, including '=', '-', space, CR and LF,
//     is refused.
//
// A failure is a *[ParseError] wrapping [ErrSyntax] or [ErrLength].
func DecodeBase32(s string) ([Size]byte, error) {
	return decodeBase32Lenient("base32", s)
}

func decodeBase32Lenient(what, s string) ([Size]byte, error) {
	var out [Size]byte
	var acc uint16
	bits, n := 0, 0
	for i := 0; i < len(s); i++ {
		if i >= Base32Len {
			// This character would produce a 33rd byte.
			return [Size]byte{}, parseError(what, s, i, ErrLength)
		}
		v, ok := base32Value(s[i], true)
		if !ok {
			return [Size]byte{}, parseError(what, s, i, ErrSyntax)
		}
		acc = acc<<5 | uint16(v)
		bits += 5
		if bits >= 8 {
			bits -= 8
			out[n] = byte(acc >> bits)
			n++
		}
	}
	if n < Size {
		return [Size]byte{}, parseError(what, s, len(s), ErrLength)
	}
	return out, nil
}

// DecodeBase32Strict decodes s as the data-encoding crate's BASE32_NOPAD
// does, which is how Atomic writes Ed25519 public keys, identity IDs and
// delegation IDs, and then requires exactly n bytes:
//
//   - only the upper-case alphabet is accepted;
//   - no '=' padding, whitespace or other byte is accepted;
//   - the input must be the length that encodes n bytes;
//   - the last character's unused low bits must be zero.
//
// A failure is a *[ParseError] wrapping [ErrSyntax] or [ErrLength], or
// [ErrRange] when n is negative.
func DecodeBase32Strict(s string, n int) ([]byte, error) {
	const what = "strict base32"
	if n < 0 {
		return nil, parseError(what, s, 0, ErrRange)
	}
	want := (n*8 + 4) / 5
	out := make([]byte, 0, n)
	var acc uint16
	bits := 0
	for i := 0; i < len(s); i++ {
		if i >= want {
			return nil, parseError(what, s, i, ErrLength)
		}
		v, ok := base32Value(s[i], false)
		if !ok {
			return nil, parseError(what, s, i, ErrSyntax)
		}
		acc = acc<<5 | uint16(v)
		bits += 5
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>bits))
		}
	}
	if len(s) < want {
		return nil, parseError(what, s, len(s), ErrLength)
	}
	if acc&(1<<bits-1) != 0 {
		return nil, parseError(what, s, len(s)-1, ErrSyntax)
	}
	return out, nil
}
