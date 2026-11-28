// Package textenc holds the binary-to-text codecs Atomic's signed documents
// use besides base32: base58btc, as the bs58 crate writes it, for did:key
// identifiers and proof values, and unpadded base64url for JWT segments.
//
// Both decoders are strict. Each accepts exactly one spelling of a given
// byte string, so text that decodes always re-encodes to itself.
package textenc

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrInvalid reports text that is not the canonical encoding of any byte
// string. Every decode failure in this package wraps it.
var ErrInvalid = errors.New("invalid encoding")

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Value returns the value of the base58 digit c.
func base58Value(c byte) (byte, bool) {
	switch {
	case '1' <= c && c <= '9':
		return c - '1', true
	case 'A' <= c && c <= 'H':
		return c - 'A' + 9, true
	case 'J' <= c && c <= 'N':
		return c - 'J' + 17, true
	case 'P' <= c && c <= 'Z':
		return c - 'P' + 22, true
	case 'a' <= c && c <= 'k':
		return c - 'a' + 33, true
	case 'm' <= c && c <= 'z':
		return c - 'm' + 44, true
	}
	return 0, false
}

// EncodeBase58 returns the base58btc text of b in the Bitcoin alphabet, as
// the bs58 crate writes it. Each leading zero byte is written as '1', and an
// empty b gives "".
//
// Its cost grows with the square of len(b).
func EncodeBase58(b []byte) string {
	zeros := 0
	for zeros < len(b) && b[zeros] == 0 {
		zeros++
	}
	// digits holds the value of b[zeros:] in base 58, least significant
	// digit first. Each byte adds at most log(256)/log(58) < 1.37 digits.
	digits := make([]byte, 0, (len(b)-zeros)*137/100+1)
	for _, c := range b[zeros:] {
		carry := uint32(c)
		for i := range digits {
			carry += uint32(digits[i]) << 8
			digits[i] = byte(carry % 58)
			carry /= 58
		}
		for carry > 0 {
			digits = append(digits, byte(carry%58))
			carry /= 58
		}
	}
	out := make([]byte, zeros+len(digits))
	for i := range zeros {
		out[i] = '1'
	}
	for i, d := range digits {
		out[len(out)-1-i] = base58Alphabet[d]
	}
	return string(out)
}

// DecodeBase58 decodes base58btc text in the Bitcoin alphabet, as the bs58
// crate does. Each leading '1' decodes to a zero byte, and "" decodes to an
// empty, non-nil slice. A byte outside the alphabet, including '0', 'O', 'I',
// 'l', whitespace and '=', is an error wrapping [ErrInvalid].
//
// Its cost grows with the square of len(s), so a caller decoding untrusted
// text should bound its length first.
func DecodeBase58(s string) ([]byte, error) {
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	// value holds the number decoded so far in base 256, least significant
	// byte first. Each digit adds at most log(58)/log(256) < 0.74 bytes.
	value := make([]byte, 0, (len(s)-zeros)*74/100+1)
	for i := zeros; i < len(s); i++ {
		d, ok := base58Value(s[i])
		if !ok {
			return nil, fmt.Errorf("textenc: base58: byte %q at offset %d: %w", s[i], i, ErrInvalid)
		}
		carry := uint32(d)
		for j := range value {
			carry += uint32(value[j]) * 58
			value[j] = byte(carry)
			carry >>= 8
		}
		for carry > 0 {
			value = append(value, byte(carry))
			carry >>= 8
		}
	}
	out := make([]byte, zeros+len(value))
	for i, c := range value {
		out[len(out)-1-i] = c
	}
	return out, nil
}

// EncodeBase64URL returns the unpadded base64url text of b (RFC 4648 §5),
// as JWT segments are written.
func EncodeBase64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeBase64URL decodes unpadded base64url text (RFC 4648 §5) strictly:
//
//   - only the URL-safe alphabet is accepted, so '+' and '/' are refused;
//   - '=' padding is refused;
//   - the last character's unused low bits must be zero;
//   - the result must re-encode to s, which refuses the CR and LF that
//     encoding/base64 otherwise skips.
//
// A failure wraps [ErrInvalid]. The error does not quote s, which may be a
// credential.
func DecodeBase64URL(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	if err != nil {
		var corrupt base64.CorruptInputError
		if errors.As(err, &corrupt) {
			return nil, fmt.Errorf("textenc: base64url: offset %d: %w", int64(corrupt), ErrInvalid)
		}
		return nil, fmt.Errorf("textenc: base64url: %w: %w", ErrInvalid, err)
	}
	if EncodeBase64URL(b) != s {
		return nil, fmt.Errorf("textenc: base64url: text is not canonical: %w", ErrInvalid)
	}
	return b, nil
}
