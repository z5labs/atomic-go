// Package remote is a client for the HTTP remotes of Atomic: the project
// protocol a repository is pushed to and pulled from, and the storage
// management API.
//
// Every failed request from either client comes back as one [*Error]. Its
// [Kind] says which variant of the Rust client's RemoteError it corresponds
// to, and the sentinels [ErrUnauthorized], [ErrForbidden], [ErrNotFound] and
// [ErrConflict] match it through [errors.Is]. [Error.Retryable],
// [Error.IsAuth] and [Error.IsNotFound] are the Rust client's three
// classifiers.
//
// A server may advise the oldest client it supports in the
// [MinVersionHeader] response header. [NeedsUpgrade] compares a version
// against it with the Rust client's rules; the header is advice and never a
// reason to fail a request.
//
// The error model is documented in docs/remote/errors.md and version
// negotiation in docs/remote/transport.md.
package remote

const (
	// MinVersionHeader is the response header in which a server states the
	// oldest client version it supports. Header names are case-insensitive,
	// so any spelling of it matches.
	MinVersionHeader = "X-Atomic-Min-Version"

	// MaxErrorBody is the most bytes of an error response's body that are
	// read and kept in [Error.Body].
	MaxErrorBody = 1 << 20
)
