package remote

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Kind is the variant of the Rust client's RemoteError that an [Error]
// corresponds to. Callers switch on it, and derive any advice to show a user
// from it.
//
// Rust's Timeout, Io, InvalidUrl, Json, EmptyView and Cancelled variants have
// no Kind: the Rust client either never produces them or produces them only
// before a request is sent (docs/remote/errors.md#open-questions). A timed-out
// request is a [KindConnectionFailed].
type Kind int

const (
	// KindConnectionFailed: the request could not be sent, or a success
	// body could not be read. This includes connect, TLS, redirect and
	// timeout failures. It is the only retryable Kind.
	KindConnectionFailed Kind = iota + 1

	// KindAuthenticationFailed: the project protocol answered 401 or 403.
	KindAuthenticationFailed

	// KindRepositoryNotFound: the repository at [Error.URL] answered 404.
	KindRepositoryNotFound

	// KindViewNotFound: the view named by [Error.Subject] was not found.
	KindViewNotFound

	// KindChangeNotFound: the change whose hash is [Error.Subject] was not
	// found.
	KindChangeNotFound

	// KindTagNotFound: no tag exists for the state in [Error.Subject].
	KindTagNotFound

	// KindStateMismatch: the remote refused to tag the state in
	// [Error.Subject] because its view is at another state.
	KindStateMismatch

	// KindMissingDependencies: the remote lacks the changes listed in
	// [Error.MissingHashes].
	KindMissingDependencies

	// KindProtocol: the server answered, but not in a form the client
	// accepts.
	KindProtocol

	// KindHTTP: a status the request does not handle otherwise.
	KindHTTP

	// KindVersionMismatch: a sync pack failed to decode, which suggests the
	// client and server run incompatible versions.
	KindVersionMismatch

	// KindOther: any other failure; [Error.Message] is its whole text.
	KindOther
)

// kindNames holds each Kind's Rust variant name, indexed by Kind.
var kindNames = [...]string{
	KindConnectionFailed:     "ConnectionFailed",
	KindAuthenticationFailed: "AuthenticationFailed",
	KindRepositoryNotFound:   "RepositoryNotFound",
	KindViewNotFound:         "ViewNotFound",
	KindChangeNotFound:       "ChangeNotFound",
	KindTagNotFound:          "TagNotFound",
	KindStateMismatch:        "StateMismatch",
	KindMissingDependencies:  "MissingDependencies",
	KindProtocol:             "ProtocolError",
	KindHTTP:                 "HttpError",
	KindVersionMismatch:      "VersionMismatch",
	KindOther:                "Other",
}

// String returns the name of the Rust RemoteError variant k corresponds to,
// such as "ConnectionFailed" or "HttpError", or "Kind(n)" for a value that is
// not one of the constants.
func (k Kind) String() string {
	if k > 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// isNotFound reports whether k is one of the not-found kinds.
func (k Kind) isNotFound() bool {
	switch k {
	case KindRepositoryNotFound, KindViewNotFound, KindChangeNotFound, KindTagNotFound:
		return true
	}
	return false
}

// Sentinels an [*Error] matches through [errors.Is]; see [Error.Is].
var (
	// ErrUnauthorized matches an Error whose status is 401.
	ErrUnauthorized = errors.New("remote: unauthorized")

	// ErrForbidden matches an Error whose status is 403.
	ErrForbidden = errors.New("remote: forbidden")

	// ErrNotFound matches an Error whose status is 404 or whose Kind is one
	// of the not-found kinds.
	ErrNotFound = errors.New("remote: not found")

	// ErrConflict matches an Error whose status is 409.
	ErrConflict = errors.New("remote: conflict")
)

// Error is a failed request to a remote, from either client.
//
// Its text, from [Error.Error], is the Rust client's display text for its
// [Kind] and never includes the cause; the cause is reached with
// [errors.Unwrap], [errors.Is] and [errors.As].
type Error struct {
	// Kind is the Rust RemoteError variant this failure corresponds to.
	Kind Kind

	// Op names the client method that failed, such as "get_state".
	Op string

	// Method and URL are the request's. URL never carries userinfo: whoever
	// builds an Error strips it, so the error can be logged and shown.
	Method string
	URL    string

	// StatusCode is the response's status, or 0 when there was no
	// response.
	StatusCode int

	// Code and Message are the server's error code and message, when the
	// response body carries them. Message is the body's text for the
	// project protocol, which sends plain text; it is also the whole text of
	// a [KindOther] and the message of a [KindProtocol] the client produced
	// itself.
	Code, Message string

	// Body is the error response's raw body, at most [MaxErrorBody] bytes.
	Body string

	// Subject is what was not found or not tagged: the view of
	// [KindViewNotFound], the change hash of [KindChangeNotFound], or the
	// state of [KindTagNotFound] and [KindStateMismatch].
	Subject string

	// MissingHashes lists the changes a [KindMissingDependencies] names.
	MissingHashes []string

	// ServerMinVersion is the [MinVersionHeader] value of the response, if
	// it had one, and ClientVersion the running client's version. Both are
	// shown by a [KindVersionMismatch].
	ServerMinVersion, ClientVersion string

	// ContextDone is set when the failure happened because the request's
	// own context was cancelled or reached its deadline. It makes a
	// [KindConnectionFailed] not retryable.
	ContextDone bool

	// Err is the underlying cause, if any.
	Err error
}

// Error returns the Rust client's display text for e's [Kind]
// (docs/remote/errors.md#classification). It never includes the cause.
func (e *Error) Error() string {
	switch e.Kind {
	case KindConnectionFailed:
		return "Failed to connect to remote: " + e.URL
	case KindAuthenticationFailed:
		return "Authentication failed for " + e.URL + ": " + e.Message
	case KindRepositoryNotFound:
		return "Repository not found: " + e.URL
	case KindViewNotFound:
		return "View not found: " + e.Subject
	case KindChangeNotFound:
		return "Change not found: " + e.Subject
	case KindTagNotFound:
		return "Tag not found for state: " + e.Subject
	case KindStateMismatch:
		// The Rust client never learns the remote's state and always
		// reports it as "unknown".
		return "State mismatch: remote is at unknown, cannot tag " + e.Subject
	case KindMissingDependencies:
		return fmt.Sprintf("Missing %d dependencies on remote: %s", len(e.MissingHashes), formatHashes(e.MissingHashes))
	case KindProtocol:
		return "Protocol error: " + e.Message
	case KindHTTP:
		return "HTTP error " + strconv.Itoa(e.StatusCode) + ": " + e.Message
	case KindVersionMismatch:
		return e.versionMismatchText()
	case KindOther:
		return e.Message
	default:
		if e.Message != "" {
			return e.Message
		}
		return "remote: " + e.Kind.String()
	}
}

// formatHashes lists at most three hashes, then how many more there are, as
// the Rust client's MissingDependencies text does.
func formatHashes(hashes []string) string {
	const shown = 3
	switch {
	case len(hashes) == 0:
		return "(none)"
	case len(hashes) <= shown:
		return strings.Join(hashes, ", ")
	default:
		return strings.Join(hashes[:shown], ", ") + ", ... and " + strconv.Itoa(len(hashes)-shown) + " more"
	}
}

func (e *Error) versionMismatchText() string {
	var b strings.Builder
	b.WriteString("Failed to decode data from remote — the client and server may be running incompatible versions.\n")
	b.WriteString("  Client version: " + e.ClientVersion + "\n")
	if e.ServerMinVersion != "" {
		b.WriteString("  Server min-version: " + e.ServerMinVersion + "\n")
	}
	b.WriteString("  Hint: run 'atomic update' to upgrade, or ask the storage owner for their server version.")
	return b.String()
}

// Unwrap returns the cause, so [errors.Is] and [errors.As] reach it: a
// request whose context ended matches [context.Canceled] or
// [context.DeadlineExceeded].
func (e *Error) Unwrap() error {
	return e.Err
}

// Is reports whether e matches target: [ErrUnauthorized], [ErrForbidden] and
// [ErrConflict] by status alone, whatever the Kind, and [ErrNotFound] for a
// 404 or any not-found Kind. Any other target is left to the cause.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	case ErrNotFound:
		return e.IsNotFound()
	}
	return false
}

// Retryable reports whether sending the request again might succeed: a
// [KindConnectionFailed] whose request's own context had not ended.
//
// It deliberately does not inspect the cause. An [http.Client] Timeout
// error matches [context.DeadlineExceeded] through [errors.Is], yet a
// timeout is a retryable ConnectionFailed in Rust
// (docs/remote/errors.md#connectionfailed). Whether the caller's context
// ended is recorded in [Error.ContextDone] instead.
func (e *Error) Retryable() bool {
	return e.Kind == KindConnectionFailed && !e.ContextDone
}

// IsAuth reports whether e is an authentication or authorisation failure:
// a [KindAuthenticationFailed], or a status of 401 or 403.
func (e *Error) IsAuth() bool {
	return e.Kind == KindAuthenticationFailed ||
		e.StatusCode == http.StatusUnauthorized ||
		e.StatusCode == http.StatusForbidden
}

// IsNotFound reports whether e says something was not found: one of the
// not-found kinds, or a status of 404.
func (e *Error) IsNotFound() bool {
	return e.Kind.isNotFound() || e.StatusCode == http.StatusNotFound
}
