package wire

import (
	"errors"
	"fmt"
)

// ErrUnsafeQueryValue reports a query key or value that the Rust client's
// URL parse would truncate, split or trim, so that the server would not see
// what the caller meant (docs/remote/transport.md#building-request-urls).
var ErrUnsafeQueryValue = errors.New("unsafe query value")

// Query builds a URL's raw query the way the Rust client's URL parse
// leaves it: the bytes a WHATWG parser percent-encodes in the query of an
// http or https URL are written as %XX, and every other byte is copied raw,
// '%', '+', '=', '?' and '/' included
// (docs/remote/transport.md#building-request-urls). Set a URL's RawQuery
// to [Query.String] to send it.
//
// The zero Query is empty and ready to use.
type Query struct {
	b []byte
}

// Add appends key=value. An empty value is written as "key=".
//
// It refuses, with an error wrapping [ErrUnsafeQueryValue], a key or value
// holding '#', which would end the query, '&', which would start another
// parameter, or a tab, line feed or carriage return, which the parse
// removes; one starting or ending with a space or a C0 control, which the
// parse trims at the end of a URL; and a key holding '='. Nothing is
// appended when it refuses.
func (q *Query) Add(key, value string) error {
	if key == "" {
		return fmt.Errorf("wire: query key is empty: %w", ErrUnsafeQueryValue)
	}
	if err := checkQueryText(key, true); err != nil {
		return fmt.Errorf("wire: query key %q: %w", key, err)
	}
	if err := checkQueryText(value, false); err != nil {
		return fmt.Errorf("wire: query value %q for %q: %w", value, key, err)
	}
	q.separate()
	q.b = appendQueryText(q.b, key)
	q.b = append(q.b, '=')
	q.b = appendQueryText(q.b, value)
	return nil
}

// Flag appends a parameter with no value, such as "views" in "?views". It
// panics if key is empty or would be refused by [Query.Add], since a flag's
// key is a constant of the caller's.
func (q *Query) Flag(key string) {
	if key == "" {
		panic("wire: Query.Flag: empty key")
	}
	if err := checkQueryText(key, true); err != nil {
		panic(fmt.Sprintf("wire: Query.Flag: key %q: %v", key, err))
	}
	q.separate()
	q.b = appendQueryText(q.b, key)
}

// String returns the raw query, without the leading '?'.
func (q *Query) String() string {
	return string(q.b)
}

func (q *Query) separate() {
	if len(q.b) > 0 {
		q.b = append(q.b, '&')
	}
}

// checkQueryText reports why s cannot be sent unchanged in a query.
func checkQueryText(s string, isKey bool) error {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '#', '&', '\t', '\n', '\r':
			return fmt.Errorf("%w: holds %q", ErrUnsafeQueryValue, c)
		case '=':
			if isKey {
				return fmt.Errorf("%w: key holds '='", ErrUnsafeQueryValue)
			}
		}
	}
	if s != "" && (s[0] <= ' ' || s[len(s)-1] <= ' ') {
		return fmt.Errorf("%w: starts or ends with a space or control character", ErrUnsafeQueryValue)
	}
	return nil
}

const upperHex = "0123456789ABCDEF"

// appendQueryText appends s with every byte of the WHATWG special-query
// percent-encode set written as %XX.
func appendQueryText(b []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if encodeInQuery(c) {
			b = append(b, '%', upperHex[c>>4], upperHex[c&0xF])
			continue
		}
		b = append(b, c)
	}
	return b
}

// encodeInQuery reports whether c is in the special-query percent-encode
// set: C0 controls, space, '"', the apostrophe, '<', '>', 0x7F and every byte from
// 0x80 up.
func encodeInQuery(c byte) bool {
	switch {
	case c <= ' ', c >= 0x7F:
		return true
	}
	switch c {
	case '"', '\'', '<', '>':
		return true
	}
	return false
}
