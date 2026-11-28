// Package delegation holds the vocabulary of Atomic's agent delegation:
// the [Permission]s a certificate can grant and what each implies, the
// [Scope] that bounds a certificate, the glob [Match] its patterns use, the
// [ResourceRef] a scope is checked against, and the [ID] that names a
// certificate.
//
// Everything here is a pure function of its arguments. Certificates,
// certificate choice and permission checks all build on these, so a scope
// means the same thing wherever it is read.
//
// The formats are documented in docs/remote/auth.md#scope,
// docs/remote/auth.md#delegation-ids, docs/remote/auth.md#delegationscope,
// docs/remote/auth.md#delegationpermission and
// docs/remote/auth.md#resourceref.
package delegation

import "errors"

var (
	// ErrUnknownPermission reports text that is not a [Permission].
	ErrUnknownPermission = errors.New("delegation: unknown permission")

	// ErrInvalidScope reports JSON that is not a delegation scope.
	ErrInvalidScope = errors.New("delegation: invalid scope")

	// ErrInvalidID reports text that is not a delegation id.
	ErrInvalidID = errors.New("delegation: invalid delegation id")
)
