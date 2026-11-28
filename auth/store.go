package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote/serverurl"
)

// Directory lists the identities on this machine and names the default one.
// It is the part of an identity store that [Resolve] and
// [ResolveForManagement] read.
type Directory interface {
	// Identities returns every identity on this machine, in a stable
	// order. The caller must not modify the slice.
	Identities(ctx context.Context) ([]identity.Identity, error)

	// DefaultIdentity returns the name of the default identity, or "" when
	// no default is set.
	DefaultIdentity(ctx context.Context) (string, error)
}

// KeyLoader loads the signing key of an identity on this machine.
type KeyLoader interface {
	// Signer returns the signer for the identity whose id is id. A key
	// that cannot be loaded, because it is missing, locked or unreadable,
	// is an error wrapping [ErrKeyUnavailable].
	Signer(ctx context.Context, id identity.ID) (identity.Signer, error)
}

// CertificateStore holds the delegation certificates issued to the agent
// identities on this machine.
type CertificateStore interface {
	// Certificates returns the stored certificates whose delegate is the
	// identity whose id is delegate, in any order. They are not verified:
	// [delegation.Choose] verifies the ones it considers.
	Certificates(ctx context.Context, delegate identity.ID) ([]delegation.Certificate, error)
}

// RevocationList says which delegation certificates this machine knows to
// be revoked.
type RevocationList interface {
	// Revoked reports whether the certificate whose id is id is revoked.
	Revoked(ctx context.Context, id delegation.ID) (bool, error)
}

// PushStore is what [CheckPushCredentials] reads: the identities and their
// keys.
type PushStore interface {
	Directory
	KeyLoader
}

// Store is a whole identity store: identities, keys, the certificates
// issued to agents and the revocations this machine knows of. [Keyring] is
// an in-memory Store.
type Store interface {
	PushStore
	CertificateStore
	RevocationList
}

// Resolve returns the local identity to authenticate to p as, chosen as
// [ChooseName] chooses and looked up in dir as the Rust client looks it up
// (docs/remote/auth.md#choosing-an-identity).
//
// The chosen name is matched exactly first, then without regard to case,
// the first identity in dir's order winning. A name that matches nothing
// falls back to dir's default identity unless it is explicit
// ([Choice.Explicit]): an explicit name that matches nothing is never
// replaced. With no name chosen at all, the default is used. The default is
// looked up the same way.
//
// ok is false, and the error nil, when no identity resolves; the Rust
// client then sends the request without credentials, so that public reads
// still work. An error is dir's.
func Resolve(ctx context.Context, dir Directory, p serverurl.Project, sel Selection) (id identity.Identity, ok bool, err error) {
	id, ok, err = resolveChoice(ctx, dir, ChooseName(p, sel))
	if err != nil {
		return identity.Identity{}, false, fmt.Errorf("auth: Resolve: %w", err)
	}
	return id, ok, nil
}

// ResolveForManagement returns the local identity that the management
// client authenticates as, for a server profile whose bound human identity
// is profileIdentity: that identity, matched exactly, or the default
// identity when profileIdentity is "".
//
// It never uses a profile's agent identity, because only the human may
// enroll, renew or revoke a delegation
// (docs/remote/auth.md#choosing-an-identity). Unlike [Resolve], a
// profileIdentity that matches nothing does not fall back to the default,
// and is not matched without regard to case, as in the Rust client.
//
// ok is false, and the error nil, when no identity resolves. An error is
// dir's.
func ResolveForManagement(ctx context.Context, dir Directory, profileIdentity string) (id identity.Identity, ok bool, err error) {
	ids, err := dir.Identities(ctx)
	if err != nil {
		return identity.Identity{}, false, fmt.Errorf("auth: ResolveForManagement: listing identities: %w", err)
	}
	name := profileIdentity
	if name == "" {
		name, err = dir.DefaultIdentity(ctx)
		if err != nil {
			return identity.Identity{}, false, fmt.Errorf("auth: ResolveForManagement: reading the default identity: %w", err)
		}
		if name == "" {
			return identity.Identity{}, false, nil
		}
	}
	for _, id := range ids {
		if id.Name == name {
			return id, true, nil
		}
	}
	return identity.Identity{}, false, nil
}

// resolveChoice looks c up in dir, falling back to the default unless c is
// explicit.
func resolveChoice(ctx context.Context, dir Directory, c Choice) (identity.Identity, bool, error) {
	ids, err := dir.Identities(ctx)
	if err != nil {
		return identity.Identity{}, false, fmt.Errorf("listing identities: %w", err)
	}
	if c.Name != "" {
		if id, ok := lookupLenient(ids, c.Name); ok {
			return id, true, nil
		}
		if c.Explicit() {
			return identity.Identity{}, false, nil
		}
	}
	def, err := dir.DefaultIdentity(ctx)
	if err != nil {
		return identity.Identity{}, false, fmt.Errorf("reading the default identity: %w", err)
	}
	if def == "" {
		return identity.Identity{}, false, nil
	}
	id, ok := lookupLenient(ids, def)
	return id, ok, nil
}

// lookupLenient returns the identity named exactly name, else the first
// whose name matches it without regard to case.
func lookupLenient(ids []identity.Identity, name string) (identity.Identity, bool) {
	for _, id := range ids {
		if id.Name == name {
			return id, true
		}
	}
	for _, id := range ids {
		if strings.EqualFold(id.Name, name) {
			return id, true
		}
	}
	return identity.Identity{}, false
}
