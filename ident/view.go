package ident

import (
	"strconv"
	"strings"
)

// Scope is a view's scope. Atomic writes it in lower case, as [ScopeShared]
// or [ScopeDraft], but some of its parsers keep the value as sent, so a
// Scope read off the wire may be in any case.
type Scope string

const (
	// ScopeShared is the scope of a view that is shared.
	ScopeShared Scope = "shared"
	// ScopeDraft is the scope of a draft view.
	ScopeDraft Scope = "draft"
)

// ParseScope parses "shared" or "draft", in any case, and returns
// [ScopeShared] or [ScopeDraft]. Anything else is a *[ParseError] wrapping
// [ErrSyntax].
func ParseScope(s string) (Scope, error) {
	switch {
	case strings.EqualFold(s, string(ScopeShared)):
		return ScopeShared, nil
	case strings.EqualFold(s, string(ScopeDraft)):
		return ScopeDraft, nil
	}
	return "", parseError("scope", s, 0, ErrSyntax)
}

// IsDraft reports whether s is "draft", in any case.
func (s Scope) IsDraft() bool {
	return strings.EqualFold(string(s), string(ScopeDraft))
}

// Sequence is a position in a view's change log, and the decimal number
// Atomic writes for one.
type Sequence uint64

// ParseSequence parses an unsigned decimal number as Rust's u64 parser
// does: an optional single leading '+' and then at least one digit, with no
// sign, space or other byte. A value that does not fit in 64 bits is a
// *[ParseError] wrapping [ErrRange]; any other failure wraps [ErrSyntax].
func ParseSequence(s string) (Sequence, error) {
	const what = "sequence"
	digits, start := s, 0
	if strings.HasPrefix(digits, "+") {
		digits, start = digits[1:], 1
	}
	if digits == "" {
		return 0, parseError(what, s, len(s), ErrSyntax)
	}
	var v uint64
	for i := 0; i < len(digits); i++ {
		c := digits[i]
		if c < '0' || c > '9' {
			return 0, parseError(what, s, start+i, ErrSyntax)
		}
		d := uint64(c - '0')
		if v > (^uint64(0)-d)/10 {
			// Like Rust's parser, stop at the first digit that overflows
			// without looking at the rest.
			return 0, parseError(what, s, start+i, ErrRange)
		}
		v = v*10 + d
	}
	return Sequence(v), nil
}

// String returns n in decimal, with no sign or leading zeros.
func (n Sequence) String() string {
	return strconv.FormatUint(uint64(n), 10)
}
