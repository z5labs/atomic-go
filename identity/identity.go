// Package identity holds the types Atomic names its signers by: the Ed25519
// [PublicKey] of an identity, the [ID] that fingerprints it, the two [DID]
// forms documents use to name either, and an [Identity]'s [Type].
//
// Keys and ids are written as strict, upper-case, unpadded base32, the way
// atomic-core's BASE32_NOPAD codec writes them. A did:atomic is an id with a
// prefix, and a did:key carries the key itself in base58btc. Everything here
// is pure computation.
//
// The formats are documented in docs/remote/auth.md#identities-and-dids.
package identity

import (
	"crypto/ed25519"
	"errors"
	"fmt"
)

const (
	// PublicKeySize is the length in bytes of an Ed25519 public key.
	PublicKeySize = ed25519.PublicKeySize

	// SeedSize is the length in bytes of an Ed25519 private key seed,
	// which is how Atomic stores a secret key.
	SeedSize = ed25519.SeedSize

	// SignatureSize is the length in bytes of an Ed25519 signature.
	SignatureSize = ed25519.SignatureSize

	// RegistrationDomain is the first line of the text a registration
	// signs (docs/remote/auth.md#registration).
	RegistrationDomain = "atomic-storage:register"
)

var (
	// ErrInvalidKey reports text or bytes that are not an Ed25519 public
	// key in Atomic's encoding.
	ErrInvalidKey = errors.New("identity: invalid public key")

	// ErrInvalidID reports text that is not an identity id.
	ErrInvalidID = errors.New("identity: invalid identity id")

	// ErrInvalidDID reports text that is not a did:atomic or did:key DID
	// for an Ed25519 key, and the zero [DID] where a valid one is needed.
	ErrInvalidDID = errors.New("identity: invalid did")

	// ErrNotEd25519 reports a key of a type other than Ed25519, such as a
	// did:key whose multicodec prefix is not ed25519-pub.
	ErrNotEd25519 = errors.New("identity: not an ed25519 key")

	// ErrNotRegistrable reports an identity a server cannot register:
	// only a [TypeUser] identity creates a tenant.
	ErrNotRegistrable = errors.New("identity: identity cannot be registered")
)

// Type is an identity's type. Atomic writes it in snake_case, and only the
// three constants below are valid.
type Type string

const (
	// TypeUser is a human. It is the default type.
	TypeUser Type = "user"

	// TypeAgent is an AI or automated system that signs as itself, like a
	// user.
	TypeAgent Type = "agent"

	// TypeDelegated is an agent acting for a user, under a delegation
	// certificate the user signed.
	TypeDelegated Type = "delegated"
)

// IsDelegated reports whether t is [TypeDelegated], the only type that
// changes what a client sends.
func (t Type) IsDelegated() bool {
	return t == TypeDelegated
}

// UnmarshalText sets t from "user", "agent" or "delegated", exactly as
// written, and refuses anything else, including "" and other cases. It
// implements [encoding.TextUnmarshaler]. On failure t is unchanged.
func (t *Type) UnmarshalText(text []byte) error {
	switch v := Type(text); v {
	case TypeUser, TypeAgent, TypeDelegated:
		*t = v
		return nil
	}
	return fmt.Errorf("identity: unknown identity type %q", text)
}

// Identity is a named Ed25519 identity.
type Identity struct {
	// Name is the identity's name, which is also the username it
	// registers with.
	Name string
	// Type is the identity's type.
	Type Type
	// Email is the identity's email address, or "" when it has none.
	Email string
	// PublicKey is the identity's Ed25519 public key.
	PublicKey PublicKey
}
