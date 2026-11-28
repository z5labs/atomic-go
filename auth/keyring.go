package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
)

// KeyringEntry is one identity in a [Keyring] and the signer for its key.
type KeyringEntry struct {
	// Identity is the identity. Its Name must not be "" and its PublicKey
	// must not be the zero key.
	Identity identity.Identity

	// Signer signs with the identity's key, and its PublicKey must be
	// Identity.PublicKey. A nil Signer is an identity whose key cannot be
	// loaded: [Keyring.Signer] fails for it with [ErrKeyUnavailable].
	Signer identity.Signer
}

// KeyringContents is everything a [Keyring] holds.
type KeyringContents struct {
	// Default is the name of the default identity, which must be the exact
	// name of one of Entries, or "" for no default.
	Default string

	// Entries are the identities, in the order [Keyring.Identities]
	// returns them. No two may share a name or a key.
	Entries []KeyringEntry

	// Certificates are the delegation certificates held for the agents, in
	// the order [Keyring.Certificates] returns them.
	Certificates []delegation.Certificate

	// Revoked are the ids of the certificates this machine knows to be
	// revoked.
	Revoked []delegation.ID
}

// Keyring is an immutable, in-memory [Store], for a program that keeps its
// keys itself and for tests. Build one with [NewKeyring]. A Keyring may be
// used by any number of goroutines at once.
type Keyring struct {
	def     string
	ids     []identity.Identity
	signers map[identity.ID]identity.Signer
	certs   []delegation.Certificate
	revoked map[delegation.ID]struct{}
}

// NewKeyring returns a [Keyring] holding c, which it copies.
//
// It refuses an entry with no name or a zero key, two entries with the same
// name or the same key, a Signer whose key is not its entry's, and a Default
// that names no entry, naming every problem in one error made with
// [errors.Join], each wrapping [ErrInvalidArgument].
func NewKeyring(c KeyringContents) (*Keyring, error) {
	k := &Keyring{
		def:     c.Default,
		ids:     make([]identity.Identity, 0, len(c.Entries)),
		signers: make(map[identity.ID]identity.Signer, len(c.Entries)),
		certs:   slices.Clone(c.Certificates),
		revoked: make(map[delegation.ID]struct{}, len(c.Revoked)),
	}
	var errs []error
	refuse := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalidArgument}, args...)...))
	}
	names := make(map[string]bool, len(c.Entries))
	keys := make(map[identity.PublicKey]bool, len(c.Entries))
	for i, e := range c.Entries {
		id := e.Identity
		switch {
		case id.Name == "":
			refuse("entry %d has no name", i)
		case names[id.Name]:
			refuse("two entries are named %q", id.Name)
		}
		names[id.Name] = true
		switch {
		case id.PublicKey == identity.PublicKey{}:
			refuse("entry %q has no public key", id.Name)
		case keys[id.PublicKey]:
			refuse("two entries hold the key %s", id.PublicKey)
		}
		keys[id.PublicKey] = true
		if e.Signer != nil && e.Signer.PublicKey() != id.PublicKey {
			refuse("the signer of entry %q holds the key %s, not the entry's %s", id.Name, e.Signer.PublicKey(), id.PublicKey)
		}
		k.ids = append(k.ids, id)
		k.signers[id.PublicKey.ID()] = e.Signer
	}
	if c.Default != "" && !names[c.Default] {
		refuse("the default identity %q is not an entry", c.Default)
	}
	for _, id := range c.Revoked {
		k.revoked[id] = struct{}{}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("auth: NewKeyring: %w", errors.Join(errs...))
	}
	return k, nil
}

// Identities returns the keyring's identities, in the order they were given
// to [NewKeyring]. The slice is the caller's own.
func (k *Keyring) Identities(ctx context.Context) ([]identity.Identity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.Clone(k.ids), nil
}

// DefaultIdentity returns the name of the default identity, or "" when there
// is none.
func (k *Keyring) DefaultIdentity(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return k.def, nil
}

// Signer returns the signer of the identity whose id is id. An identity the
// keyring does not hold, and one whose entry had a nil Signer, are errors
// wrapping [ErrKeyUnavailable].
func (k *Keyring) Signer(ctx context.Context, id identity.ID) (identity.Signer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s, ok := k.signers[id]
	switch {
	case !ok:
		return nil, fmt.Errorf("%w: no identity with id %s", ErrKeyUnavailable, id)
	case s == nil:
		return nil, fmt.Errorf("%w: the key of identity %s cannot be loaded", ErrKeyUnavailable, id)
	}
	return s, nil
}

// Certificates returns the keyring's certificates whose delegate is the
// did:atomic of delegate, in the order they were given to [NewKeyring]. The
// slice is the caller's own.
func (k *Keyring) Certificates(ctx context.Context, delegate identity.ID) ([]delegation.Certificate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	did := delegate.DID()
	var out []delegation.Certificate
	for _, c := range k.certs {
		if c.AgentDelegation().Delegate == did {
			out = append(out, c)
		}
	}
	return out, nil
}

// Revoked reports whether id was among the revoked ids given to
// [NewKeyring].
func (k *Keyring) Revoked(ctx context.Context, id delegation.ID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, ok := k.revoked[id]
	return ok, nil
}
