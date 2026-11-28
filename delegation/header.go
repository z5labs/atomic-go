package delegation

import (
	"fmt"
	"strings"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/textenc"
)

const (
	// HeaderName is the HTTP header a delegated request carries its
	// certificate in.
	HeaderName = "Atomic-Delegation"

	// MaxHeaderLen is the longest header value, in bytes, that
	// [DecodeHeader] will decode: 16 KiB.
	MaxHeaderLen = 16 << 10
)

// encodeHeader returns the unpadded base64url of canon.
func encodeHeader(canon []byte) string {
	return textenc.EncodeBase64URL(canon)
}

// DecodeHeader reads an Atomic-Delegation header value as Atomic's
// decode_from_transport does (docs/remote/auth.md#header-encoding):
//
//  1. A value longer than [MaxHeaderLen] bytes is refused, before anything
//     else is done with it, with an error wrapping [ErrHeaderTooLarge].
//  2. Leading and trailing white space, newlines included, is trimmed.
//  3. The rest is decoded as strict unpadded base64url, so padding, the
//     standard alphabet's '+' and '/', and white space inside the value are
//     refused.
//  4. The bytes are read with [ParseCertificate].
//
// Failures from step 3 on wrap [ErrInvalid]. Decoding is not trusting: a
// tampered certificate decodes, and then fails [Certificate.Verify]. The
// errors never quote value.
func DecodeHeader(value string) (Certificate, error) {
	if len(value) > MaxHeaderLen {
		return Certificate{}, fmt.Errorf("%w: %d bytes, limit %d", ErrHeaderTooLarge, len(value), MaxHeaderLen)
	}
	b, err := textenc.DecodeBase64URL(strings.TrimSpace(value))
	if err != nil {
		return Certificate{}, fmt.Errorf("%w: header value: %w", ErrInvalid, err)
	}
	return ParseCertificate(b)
}

// Fingerprint returns the fingerprint of an encoded header value: the
// strict, upper-case, unpadded base32 of the BLAKE3 of headerValue's bytes,
// exactly as given. Atomic describes it as a key for caching verified
// certificates.
func Fingerprint(headerValue string) string {
	sum := ident.Sum([]byte(headerValue))
	return ident.EncodeBase32(sum[:])
}
