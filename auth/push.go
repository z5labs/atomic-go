package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/z5labs/atomic-go/identity"
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
// signing key loads from store and is the identity's own. It mints no token,
// checks no certificate and sends no request.
//
// A failure is an [*AuthenticationError] whose Issue is
// ExplicitIdentityNotFound when sel.Override matched nothing, NoIdentity
// when nothing else resolved, and KeypairUnavailable when the key would not
// load. As in the Rust client, which asks only whether the key loaded, every
// error from store's [KeyLoader] is KeypairUnavailable, whether or not it
// wraps [ErrKeyUnavailable], and is kept as the AuthenticationError's Err;
// the one exception is a done ctx, whose error is returned as it is. An
// error from store's [Directory] is not an AuthenticationError: it is
// returned wrapped, as [Resolve] returns it.
func CheckPushCredentials(ctx context.Context, store PushStore, p serverurl.Project, sel Selection) error {
	_, _, err := resolveSigner(ctx, store, p, sel)
	var authErr *AuthenticationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &authErr):
		return err
	default:
		return fmt.Errorf("auth: CheckPushCredentials: %w", err)
	}
}

// resolveSigner resolves the identity to authenticate to p as and loads its
// signer, failing as [CheckPushCredentials] documents. An
// [*AuthenticationError] is returned as it is; any other error is returned
// unwrapped, for the caller to name itself in.
func resolveSigner(ctx context.Context, store PushStore, p serverurl.Project, sel Selection) (identity.Identity, identity.Signer, error) {
	choice := ChooseName(p, sel)
	id, ok, err := resolveChoice(ctx, store, choice)
	if err != nil {
		return identity.Identity{}, nil, err
	}
	if !ok {
		if choice.Explicit() {
			return identity.Identity{}, nil, &AuthenticationError{Remote: p.String(), Issue: ExplicitIdentityNotFound, Name: choice.Name}
		}
		return identity.Identity{}, nil, &AuthenticationError{Remote: p.String(), Issue: NoIdentity}
	}
	s, err := loadSigner(ctx, store, id)
	if err != nil {
		if ctx.Err() != nil {
			return identity.Identity{}, nil, ctx.Err()
		}
		return identity.Identity{}, nil, &AuthenticationError{Remote: p.String(), Issue: KeypairUnavailable, Name: id.Name, Err: err}
	}
	return id, s, nil
}

// loadSigner loads id's signer from keys and checks that it holds id's key.
// A nil signer, and one holding another key, are errors wrapping
// [ErrKeyUnavailable]; an error from keys is returned as it is.
func loadSigner(ctx context.Context, keys KeyLoader, id identity.Identity) (identity.Signer, error) {
	s, err := keys.Signer(ctx, id.PublicKey.ID())
	switch {
	case err != nil:
		return nil, err
	case s == nil:
		return nil, fmt.Errorf("%w: the key loader returned no signer", ErrKeyUnavailable)
	case s.PublicKey() != id.PublicKey:
		return nil, fmt.Errorf("%w: the loaded key is not the identity's", ErrKeyUnavailable)
	}
	return s, nil
}
