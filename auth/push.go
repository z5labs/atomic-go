package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/z5labs/atomic-go/remote/serverurl"
)

var (
	// ErrAuthenticationFailed is matched by every [*AuthenticationError],
	// the Rust client's AuthError::AuthenticationFailed.
	ErrAuthenticationFailed = errors.New("auth: authentication failed")

	// ErrKeyUnavailable is wrapped by the error a [KeyLoader] returns for
	// a signing key it cannot load.
	ErrKeyUnavailable = errors.New("auth: signing key unavailable")
)

// CredentialIssue is why [CheckPushCredentials] failed
// (docs/remote/auth.md#credentialissue).
type CredentialIssue int

// The reasons [CheckPushCredentials] fails.
const (
	// NoIdentity means no identity resolved, no override was given, and
	// no default identity is set.
	NoIdentity CredentialIssue = iota + 1

	// ExplicitIdentityNotFound means the override names an identity that
	// is not on this machine.
	ExplicitIdentityNotFound

	// KeypairUnavailable means the identity exists but its signing key
	// cannot be loaded.
	KeypairUnavailable
)

// String returns the issue's name, such as "NoIdentity".
func (c CredentialIssue) String() string {
	switch c {
	case NoIdentity:
		return "NoIdentity"
	case ExplicitIdentityNotFound:
		return "ExplicitIdentityNotFound"
	case KeypairUnavailable:
		return "KeypairUnavailable"
	default:
		return "CredentialIssue(" + strconv.Itoa(int(c)) + ")"
	}
}

// remedy ends every [CredentialIssue.Message].
const remedy = "Register your identity with the server first: `atomic identity register <server-url>`."

// Message returns the Rust client's text for the issue. name is the identity
// the issue is about; NoIdentity ignores it. Every message begins
// "Not authenticated for this remote:" and ends by telling the user to
// register their identity.
func (c CredentialIssue) Message(name string) string {
	switch c {
	case NoIdentity:
		return "Not authenticated for this remote: could not determine an identity from the remote URL, " +
			"no --identity was given, and no default identity is set. Pass --identity <NAME> " +
			"(see `atomic identity list`) or set a default with `atomic identity new <name> --set-default`. " + remedy
	case ExplicitIdentityNotFound:
		return "Not authenticated for this remote: --identity '" + name +
			"' does not match any identity on this machine (see `atomic identity list`). " + remedy
	case KeypairUnavailable:
		return "Not authenticated for this remote: could not load the signing key for identity '" + name + "'. " + remedy
	default:
		return "Not authenticated for this remote: " + c.String() + ". " + remedy
	}
}

// AuthenticationError is the error [CheckPushCredentials] returns when this
// machine cannot produce credentials for a remote, the Rust client's
// AuthError::AuthenticationFailed (docs/remote/auth.md#autherror).
// It matches [ErrAuthenticationFailed] with [errors.Is].
type AuthenticationError struct {
	// Remote is the remote's URL.
	Remote string

	// Issue is what is missing.
	Issue CredentialIssue

	// Name is the identity Issue is about, or "" for NoIdentity.
	Name string

	// Err is the cause, such as the error the [KeyLoader] returned, or
	// nil.
	Err error
}

// Error returns the Rust client's text:
//
//	Authentication failed for remote '<remote>: <message>'
//
// where the message is the Issue's [CredentialIssue.Message].
func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("Authentication failed for remote '%s: %s'", e.Remote, e.Issue.Message(e.Name))
}

// Is reports whether target is [ErrAuthenticationFailed].
func (e *AuthenticationError) Is(target error) bool {
	return target == ErrAuthenticationFailed
}

// Unwrap returns Err.
func (e *AuthenticationError) Unwrap() error {
	return e.Err
}

// CheckPushCredentials is the check before a push: it fails early when this
// machine cannot authenticate to p, rather than letting the push reach a
// server that answers a private project's anonymous caller with 404
// (docs/remote/auth.md#check_push_credentials).
//
// It passes when an identity resolves, as [Resolve] resolves it, and its
// signing key loads from store and is the identity's own. It mints no token, checks no certificate and
// sends no request. A failure is an [*AuthenticationError] whose Issue is
// ExplicitIdentityNotFound when sel.Override matched nothing, NoIdentity
// when nothing else resolved, and KeypairUnavailable when the key would not
// load. Any other error is store's, or ctx's.
func CheckPushCredentials(ctx context.Context, store PushStore, p serverurl.Project, sel Selection) error {
	choice := ChooseName(p, sel)
	id, ok, err := resolveChoice(ctx, store, choice)
	if err != nil {
		return fmt.Errorf("auth: CheckPushCredentials: %w", err)
	}
	if !ok {
		if choice.Explicit() {
			return &AuthenticationError{Remote: p.String(), Issue: ExplicitIdentityNotFound, Name: choice.Name}
		}
		return &AuthenticationError{Remote: p.String(), Issue: NoIdentity}
	}
	s, err := store.Signer(ctx, id.PublicKey.ID())
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("auth: CheckPushCredentials: %w", ctx.Err())
	}
	switch {
	case err != nil:
	case s == nil:
		err = fmt.Errorf("%w: the key loader returned no signer", ErrKeyUnavailable)
	case s.PublicKey() != id.PublicKey:
		err = fmt.Errorf("%w: the loaded key is not the identity's", ErrKeyUnavailable)
	default:
		return nil
	}
	return &AuthenticationError{Remote: p.String(), Issue: KeypairUnavailable, Name: id.Name, Err: err}
}
