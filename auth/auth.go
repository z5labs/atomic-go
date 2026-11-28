// Package auth puts credentials on the requests an Atomic client sends.
//
// Credentials reach the clients only through [Transport]. Transport is an
// [http.RoundTripper] that wraps the caller's own transport and is bound to
// one origin, the scheme, host and port of the server the credentials are
// for. A request bound for that origin is cloned, the clone is handed to
// [Credentials.Attach] to add its headers, and the clone is sent; a request
// bound for any other origin is passed to the wrapped transport untouched.
// Neither the project-protocol client nor the storage-management client
// handles a token, and neither imports this package: each takes an
// [*http.Client], and the program that builds one sets its Transport.
//
// Attaching the headers at the transport, hop by hop, is what keeps them
// from leaking on a redirect. An [http.Client] that follows a redirect to
// another host or port sends that hop through the same Transport, which
// sees an origin that is not its own and adds nothing. The Rust client set
// its headers once, on the client, and reqwest's redirect handling removes
// Authorization but keeps Atomic-Delegation, so a cross-host redirect
// carried the certificate (docs/remote/transport.md#redirects-proxies-and-http-version).
// The headers themselves are described in docs/remote/auth.md#request-headers.
//
// Which identity the credentials are for is chosen before any of that.
// [ChooseName] picks an identity name for a project URL by the Rust client's
// rules (docs/remote/auth.md#choosing-an-identity), and [Resolve] looks it up
// in the caller's identity store, falling back to the default identity for a
// name that was inferred rather than asked for. [ResolveForManagement] is the
// management client's choice, which is always a human. [CheckPushCredentials]
// fails a push early, with an [*AuthenticationError] naming the
// [CredentialIssue], when no identity resolves or its key will not load.
//
// The identity store is the caller's: this package sees it only through the
// small interfaces it declares, [Directory], [KeyLoader], [CertificateStore]
// and [RevocationList], composed into [PushStore] and [Store]. [Keyring] is an
// in-memory Store. Rust's on-disk store and its ~/.atomic profiles are not
// read here.
//
// Nothing here reads the host: the wrapped transport, the credentials and
// the origin are all arguments to [NewTransport], the environment's values
// come in a [Selection], and the identities, keys and certificates come
// through a [Store].
package auth

import (
	"errors"
	"net/http"
)

// ErrInvalidArgument is wrapped by every error [NewTransport] and
// [NewKeyring] return for an argument they refused.
var ErrInvalidArgument = errors.New("auth: invalid argument")

// Credentials adds the headers that authenticate one request, such as
// Authorization and Atomic-Delegation (docs/remote/auth.md#request-headers).
//
// Attach is given a request that [Transport] has already cloned for it, so
// it may set headers on req freely; it must not read or replace req.Body.
// Any work it does on the request's behalf, such as signing a token, uses
// req.Context(), so it is cancelled with the request. An error from Attach
// fails the request without sending it.
//
// A Transport may call Attach from many goroutines at once.
type Credentials interface {
	Attach(req *http.Request) error
}

// NoCredentials is the [Credentials] of an anonymous caller: it adds no
// headers, and its Attach never fails. It is for reading from a server that
// serves public projects without authentication.
type NoCredentials struct{}

// Attach adds nothing to req and returns nil.
func (NoCredentials) Attach(*http.Request) error {
	return nil
}
