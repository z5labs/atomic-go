// Package delegation implements Atomic's agent delegation: the
// [Permission]s a certificate can grant and what each implies, the [Scope]
// that bounds a certificate, the glob [Match] its patterns use, the
// [ResourceRef] a scope is checked against, the [ID] that names a
// certificate, and the signed [Certificate] itself, which an agent presents
// in the [HeaderName] header to act for a human. Two more signed documents
// share the certificate's signing method: the [Revocation] a human signs to
// withdraw a certificate ([Revoke]), and the [Request] an agent signs to
// prove it holds its key before a human issues a certificate for it
// ([NewRequest], [Request.Grant]).
//
// Everything here is a pure function of its arguments. [Issue], [Revoke] and
// [NewRequest] take the signer's [identity.Signer] and the time to write,
// and [Certificate.Expired]
// and [Certificate.Allows] take the time to check at, so nothing reads the
// clock.
//
// The formats are documented in docs/remote/auth.md#delegation,
// docs/remote/auth.md#agentdelegation, docs/remote/auth.md#scope,
// docs/remote/auth.md#delegation-ids, docs/remote/auth.md#delegationscope,
// docs/remote/auth.md#delegationpermission,
// docs/remote/auth.md#resourceref, docs/remote/auth.md#revocation-and-epochs,
// docs/remote/auth.md#delegation-requests,
// docs/remote/auth.md#delegationrevocation and
// docs/remote/auth.md#delegationrequest.
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

	// ErrInvalidRevocation reports a document that is not a revocation, or
	// arguments [Revoke] cannot sign one from.
	ErrInvalidRevocation = errors.New("delegation: invalid revocation")

	// ErrVerifyRevocation reports a revocation that fails one of the checks
	// of [Revocation.Verify].
	ErrVerifyRevocation = errors.New("delegation: revocation does not verify")

	// ErrInvalidRequest reports a document that is not a delegation request,
	// or arguments [NewRequest] cannot sign one from.
	ErrInvalidRequest = errors.New("delegation: invalid delegation request")

	// ErrVerifyRequest reports a delegation request that fails one of the
	// checks of [Request.Verify].
	ErrVerifyRequest = errors.New("delegation: delegation request does not verify")
)
