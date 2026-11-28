package storage

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const upperHex = "0123456789ABCDEF"

// segment returns s written as one path segment, the bytes the Rust client
// puts on the wire when it formats s into a path unescaped and reqwest's URL
// parse reads it back (docs/remote/transport.md#building-request-urls).
//
// It refuses, with an error wrapping [ErrInvalidArgument], every s that
// parse would turn into another path: "", ".", "..", and any spelling of
// those with %2e in either case, which the parse resolves away, and any s
// holding '/' or '\', which add a segment, '?' or '#', which end the path, a
// C0 control, which the parse removes or encodes, or 0x7F.
//
// Space, '"', '<', '>', '`', '{', '}' and every byte from 0x80 up are
// written as %XX, as the parse writes them. Every other byte, '%', the
// apostrophe and '+' included, is written as it is, so "%41" reaches the
// server as "%41".
//
// Two cases differ from Rust, because net/url cannot send them unchanged in
// a path it is given escaped. '|' and '^' are written as %7C and %5E, which
// a server decodes to the same byte. A '%' not followed by two hex digits,
// which the Rust client sends as it is, is refused.
func segment(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%w: a path segment is empty", ErrInvalidArgument)
	}
	if isDotSegment(s) {
		return "", fmt.Errorf("%w: path segment %q is a dot segment", ErrInvalidArgument, s)
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c < 0x20 || c == 0x7F:
			return "", fmt.Errorf("%w: path segment %q holds the control character %#02x", ErrInvalidArgument, s, c)
		case c == '/' || c == '\\' || c == '?' || c == '#':
			return "", fmt.Errorf("%w: path segment %q holds %q", ErrInvalidArgument, s, c)
		case c == '%':
			if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
				return "", fmt.Errorf("%w: path segment %q holds a '%%' not followed by two hex digits", ErrInvalidArgument, s)
			}
			b.WriteByte(c)
		case encodeInPath(c):
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xF])
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// encodeInPath reports whether segment writes c as %XX: the bytes of the
// WHATWG path percent-encode set that segment does not refuse, and the two
// net/url will not send raw.
func encodeInPath(c byte) bool {
	if c >= 0x80 {
		return true
	}
	switch c {
	case ' ', '"', '<', '>', '`', '{', '}', '|', '^':
		return true
	}
	return false
}

// component returns s percent-encoded as urlencoding 2.1.3's encode does it,
// which is how the Rust client writes email addresses, names, delegation
// IDs and DIDs into a path or query: every byte but ASCII letters, digits,
// '-', '.', '_' and '~' becomes %XX with upper-case hex digits
// (docs/remote/endpoints/storage.md#base-urls). So
// "urn:atomic:delegation:X" becomes "urn%3Aatomic%3Adelegation%3AX".
//
// It refuses nothing. A component that is "." or ".." still makes a dot
// segment in a path; the [Client] refuses that path when it is sent.
func component(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xF])
		}
	}
	return b.String()
}

// isDotSegment reports whether the escaped segment s is one the WHATWG URL
// parse resolves away: "." or "..", with any of their dots written as %2e
// in either case.
func isDotSegment(s string) bool {
	if len(s) > 6 {
		return false
	}
	d := strings.ReplaceAll(strings.ToLower(s), "%2e", ".")
	return d == "." || d == ".."
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// errDotSegment reports an escaped path holding a dot segment.
var errDotSegment = errors.New("holds a '.' or '..' segment")

// checkDotSegments reports whether the escaped path p holds a segment that
// [isDotSegment] says would be resolved away.
func checkDotSegments(p string) error {
	for _, s := range strings.Split(p, "/") {
		if isDotSegment(s) {
			return errDotSegment
		}
	}
	return nil
}

// requestURL returns the URL of a request for the escaped path, which starts
// with '/', and raw query: the base URL followed by both, sent exactly as
// they are.
//
// It refuses, with an error wrapping [ErrInvalidArgument], a path holding a
// dot segment, which the Rust client's URL parse would resolve away, and one
// net/url would not send unchanged.
func (c *Client) requestURL(path, query string) (*url.URL, error) {
	raw := c.basePath + path
	if err := checkDotSegments(raw); err != nil {
		return nil, fmt.Errorf("%w: request path %q %w", ErrInvalidArgument, raw, err)
	}
	unescaped, err := url.PathUnescape(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: request path %q: %w", ErrInvalidArgument, raw, err)
	}
	u := *c.base
	u.Path = unescaped
	u.RawPath = raw
	u.RawQuery = query
	if got := u.EscapedPath(); got != raw {
		return nil, fmt.Errorf("%w: request path %q would be sent as %q", ErrInvalidArgument, raw, got)
	}
	return &u, nil
}
