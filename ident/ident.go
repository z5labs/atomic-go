// Package ident holds the identifiers Atomic names its objects by: the
// 32-byte BLAKE3 [Hash] and its base32 and hex text forms, the Merkle fold
// that gives a view its state, the order-free [SetID] of a view's members,
// view [Scope]s, [Sequence] numbers, content [Position]s, and the check
// [ValidateNewViewName] applies to a view name before a view is created.
//
// Every package in this module shares these types, so a value parsed in one
// package never needs converting before another can use it. Everything here
// is pure computation.
//
// The formats are documented in docs/remote/types.md and, for the strict
// base32 codec of identity keys, in docs/remote/auth.md.
package ident

import (
	"errors"
	"fmt"
)

const (
	// Size is the length in bytes of a [Hash] and a [SetID].
	Size = 32

	// Base32Len is the length of the base32 text form of a Size-byte value:
	// 51 full 5-bit groups and one partial one.
	Base32Len = 52

	// EmptyState is the wire form of an empty view's Merkle state, which
	// has no hash.
	EmptyState = "-"

	// SetIDDomain is the domain separator hashed before every member of a
	// [SetID].
	SetIDDomain = "atomic:setid:v1"
)

var (
	// ErrSyntax reports a character or value that the format does not
	// allow.
	ErrSyntax = errors.New("invalid syntax")

	// ErrLength reports input that is too long or too short.
	ErrLength = errors.New("invalid length")

	// ErrRange reports a value that is out of range, such as a sequence
	// number that does not fit in 64 bits.
	ErrRange = errors.New("value out of range")
)

// ParseError describes a failure to parse text. Every parse failure in this
// package is a *ParseError, and its Err is one of [ErrSyntax], [ErrLength]
// or [ErrRange], so callers can test it with [errors.Is].
type ParseError struct {
	// What names the kind of value being parsed, such as "hash".
	What string
	// Input is the text that failed to parse.
	Input string
	// Offset is the byte offset in Input where the failure was found.
	Offset int
	// Err is the reason: ErrSyntax, ErrLength or ErrRange.
	Err error
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	return fmt.Sprintf("ident: parsing %s %q: %v at offset %d", e.What, e.Input, e.Err, e.Offset)
}

// Unwrap returns e.Err.
func (e *ParseError) Unwrap() error {
	return e.Err
}

func parseError(what, input string, offset int, err error) *ParseError {
	return &ParseError{What: what, Input: input, Offset: offset, Err: err}
}
