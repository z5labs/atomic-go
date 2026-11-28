package identity

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"fmt"
)

// Signer signs messages with an identity's Ed25519 key, wherever that key
// lives: in memory, as from [NewSigner] and [SignerFromSeed], or behind a
// [crypto.Signer] such as a hardware security module, as from
// [FromCryptoSigner]. Nothing in this module signs except through a Signer,
// so a hardware-backed key works everywhere one is taken.
//
// Signer is declared once, here, as a deliberate exception to the
// repository's rule that an interface is declared in the package that uses
// it. The token, canonical, delegation, projectidentity and change packages
// all need both methods, and one shared declaration keeps a single
// implementation usable by all of them.
type Signer interface {
	// PublicKey returns the public half of the key Sign signs with.
	PublicKey() PublicKey

	// Sign returns the [SignatureSize]-byte Ed25519 signature of msg.
	// Ed25519 signs the message itself, not a digest of it. Sign returns
	// ctx.Err() when ctx is done before it signs.
	Sign(ctx context.Context, msg []byte) ([]byte, error)
}

// keySigner is a [Signer] over an Ed25519 private key held in memory.
type keySigner struct {
	priv ed25519.PrivateKey
	pub  PublicKey
}

// NewSigner returns a [Signer] for priv, which must be an
// [ed25519.PrivateKeySize]-byte key whose public half is the one its seed
// derives. Anything else is an error wrapping [ErrInvalidPrivateKey]. The
// Signer keeps its own copy of priv.
func NewSigner(priv ed25519.PrivateKey) (Signer, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: %d bytes, want %d", ErrInvalidPrivateKey, len(priv), ed25519.PrivateKeySize)
	}
	derived := ed25519.NewKeyFromSeed(priv.Seed())
	if !bytes.Equal(derived, priv) {
		return nil, fmt.Errorf("%w: public half does not match the seed", ErrInvalidPrivateKey)
	}
	return newKeySigner(derived), nil
}

// SignerFromSeed returns a [Signer] for the Ed25519 key that seed derives,
// the form Atomic stores a secret key in. A seed that is not [SeedSize]
// bytes is an error wrapping [ErrInvalidPrivateKey].
func SignerFromSeed(seed []byte) (Signer, error) {
	if len(seed) != SeedSize {
		return nil, fmt.Errorf("%w: seed is %d bytes, want %d", ErrInvalidPrivateKey, len(seed), SeedSize)
	}
	return newKeySigner(ed25519.NewKeyFromSeed(seed)), nil
}

func newKeySigner(priv ed25519.PrivateKey) keySigner {
	return keySigner{priv: priv, pub: PublicKey(priv[SeedSize:])}
}

func (s keySigner) PublicKey() PublicKey {
	return s.pub
}

func (s keySigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ed25519.Sign(s.priv, msg), nil
}

// cryptoSigner is a [Signer] over a [crypto.Signer] whose key is Ed25519.
type cryptoSigner struct {
	cs  crypto.Signer
	pub PublicKey
}

// FromCryptoSigner returns a [Signer] that signs through cs, such as a key
// held in a hardware security module. cs must hold an Ed25519 key: its
// Public method must return an [ed25519.PublicKey] of [PublicKeySize]
// bytes, and anything else is an error wrapping [ErrNotEd25519].
//
// The Signer calls cs.Sign with a nil random source and crypto.Hash(0),
// which asks for plain Ed25519 over the whole message. Ed25519 is
// deterministic, so no randomness is involved. A signature that is not
// [SignatureSize] bytes, or that does not verify against the key Public
// returned, is an error.
func FromCryptoSigner(cs crypto.Signer) (Signer, error) {
	if cs == nil {
		return nil, fmt.Errorf("%w: nil crypto.Signer", ErrNotEd25519)
	}
	pub, ok := cs.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: crypto.Signer holds a %T", ErrNotEd25519, cs.Public())
	}
	if len(pub) != PublicKeySize {
		return nil, fmt.Errorf("%w: public key is %d bytes, want %d", ErrNotEd25519, len(pub), PublicKeySize)
	}
	return cryptoSigner{cs: cs, pub: PublicKey(pub)}, nil
}

func (s cryptoSigner) PublicKey() PublicKey {
	return s.pub
}

func (s cryptoSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sig, err := s.cs.Sign(nil, msg, crypto.Hash(0))
	if err != nil {
		return nil, fmt.Errorf("identity: sign: %w", err)
	}
	if len(sig) != SignatureSize {
		return nil, fmt.Errorf("identity: sign: signature is %d bytes, want %d", len(sig), SignatureSize)
	}
	if !s.pub.Verify(msg, sig) {
		return nil, fmt.Errorf("identity: sign: signature does not verify against %s", s.pub)
	}
	return sig, nil
}
