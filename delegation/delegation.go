// Package delegation implements Atomic's agent delegation: the
// [Permission]s a certificate can grant and what each implies, the [Scope]
// that bounds a certificate, the glob [Match] its patterns use, the
// [ResourceRef] a scope is checked against, the [ID] that names a
// certificate, and the signed [Certificate] itself, which an agent presents
// in the [HeaderName] header to act for a human.
//
// Everything here is a pure function of its arguments. [Issue] takes the
// delegator's [identity.Signer] and the issue time, and [Certificate.Expired]
// and [Certificate.Allows] take the time to check at, so nothing reads the
// clock.
//
// The formats are documented in docs/remote/auth.md#delegation,
// docs/remote/auth.md#agentdelegation, docs/remote/auth.md#scope,
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

	// ErrInvalid reports a document, header value or [Grant] that is not a
	// delegation certificate or cannot become one.
	ErrInvalid = errors.New("delegation: invalid certificate")

	// ErrVerify reports a certificate that fails one of the checks of
	// [Certificate.Verify].
	ErrVerify = errors.New("delegation: certificate does not verify")

	// ErrHeaderTooLarge reports a header value longer than [MaxHeaderLen],
	// which [DecodeHeader] refuses before decoding it.
	ErrHeaderTooLarge = errors.New("delegation: header value too large")

	// ErrExpired reports a certificate used after its expiry. Nothing in
	// this package returns it, since [Certificate.Expired] answers with a
	// bool; it is for callers, such as certificate choice, that refuse an
	// expired certificate with an error.
	ErrExpired = errors.New("delegation: certificate expired")
)
