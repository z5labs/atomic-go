package identity_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/identity"
)

// RFC 8032 §7.1 TEST 1 and TEST 2: the seed, the message and the signature.
var signatureVectors = []struct {
	name, seed, msg, sig string
}{
	{
		name: "RFC 8032 TEST 1",
		seed: "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
		msg:  "",
		sig:  "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e065224901555fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b",
	},
	{
		name: "RFC 8032 TEST 2",
		seed: "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb",
		msg:  "72",
		sig:  "92a009a9f0d4cab8720e820b5f642540a2b27b5416503f8fb3762223ebdb69da085ac1e43e15996e458f3613d0f11d8c387b2eaeb4302aeeb00d291612bb0c00",
	},
}

// adapters builds a Signer for a seed through each of the three
// constructors.
var adapters = []struct {
	name string
	new  func(seed []byte) (identity.Signer, error)
}{
	{"NewSigner", func(seed []byte) (identity.Signer, error) {
		return identity.NewSigner(ed25519.NewKeyFromSeed(seed))
	}},
	{"SignerFromSeed", identity.SignerFromSeed},
	{"FromCryptoSigner", func(seed []byte) (identity.Signer, error) {
		return identity.FromCryptoSigner(ed25519.NewKeyFromSeed(seed))
	}},
}

func TestSignatureVectors(t *testing.T) {
	for _, v := range signatureVectors {
		for _, a := range adapters {
			t.Run(v.name+"/"+a.name, func(t *testing.T) {
				seed := mustHex(t, v.seed)
				msg := mustHex(t, v.msg)
				s, err := a.new(seed)
				require.NoError(t, err)

				want, err := identity.PublicKeyFromBytes(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
				require.NoError(t, err)
				require.Equal(t, want, s.PublicKey())

				sig, err := s.Sign(context.Background(), msg)
				require.NoError(t, err)
				require.Equal(t, v.sig, hex.EncodeToString(sig))
				require.True(t, s.PublicKey().Verify(msg, sig))
			})
		}
	}
}

func TestSignerDoneContext(t *testing.T) {
	seed := mustHex(t, signatureVectors[0].seed)
	for _, a := range adapters {
		t.Run(a.name, func(t *testing.T) {
			s, err := a.new(seed)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			sig, err := s.Sign(ctx, []byte("msg"))
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, sig)
		})
	}
}

func TestNewSignerInvalid(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(mustHex(t, signatureVectors[0].seed))

	_, err := identity.NewSigner(priv[:ed25519.PrivateKeySize-1])
	require.ErrorIs(t, err, identity.ErrInvalidPrivateKey)

	_, err = identity.NewSigner(nil)
	require.ErrorIs(t, err, identity.ErrInvalidPrivateKey)

	// The second TEST's public half after the first TEST's seed.
	other := ed25519.NewKeyFromSeed(mustHex(t, signatureVectors[1].seed))
	mismatched := append(append(ed25519.PrivateKey{}, priv.Seed()...), other[identity.SeedSize:]...)
	_, err = identity.NewSigner(mismatched)
	require.ErrorIs(t, err, identity.ErrInvalidPrivateKey)
}

func TestNewSignerCopiesKey(t *testing.T) {
	v := signatureVectors[0]
	priv := ed25519.NewKeyFromSeed(mustHex(t, v.seed))
	s, err := identity.NewSigner(priv)
	require.NoError(t, err)

	clear(priv)
	sig, err := s.Sign(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, v.sig, hex.EncodeToString(sig))
}

func TestSignerFromSeedInvalid(t *testing.T) {
	for _, n := range []int{0, identity.SeedSize - 1, identity.SeedSize + 1, ed25519.PrivateKeySize} {
		_, err := identity.SignerFromSeed(make([]byte, n))
		require.ErrorIs(t, err, identity.ErrInvalidPrivateKey, "%d bytes", n)
	}
}

func TestFromCryptoSignerECDSA(t *testing.T) {
	d := make([]byte, 32)
	d[31] = 1
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
	require.NoError(t, err)

	s, err := identity.FromCryptoSigner(key)
	require.ErrorIs(t, err, identity.ErrNotEd25519)
	require.Nil(t, s)
}

func TestFromCryptoSignerNil(t *testing.T) {
	_, err := identity.FromCryptoSigner(nil)
	require.ErrorIs(t, err, identity.ErrNotEd25519)
}

func TestFromCryptoSignerShortKey(t *testing.T) {
	_, err := identity.FromCryptoSigner(fakeCryptoSigner{pub: ed25519.PublicKey(make([]byte, 31))})
	require.ErrorIs(t, err, identity.ErrNotEd25519)
}

func TestFromCryptoSignerOptions(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(mustHex(t, signatureVectors[0].seed))
	fake := &recordingCryptoSigner{priv: priv}
	s, err := identity.FromCryptoSigner(fake)
	require.NoError(t, err)

	_, err = s.Sign(context.Background(), []byte("msg"))
	require.NoError(t, err)
	require.True(t, fake.called)
	require.Nil(t, fake.rand)
	require.Equal(t, crypto.Hash(0), fake.opts)
}

func TestFromCryptoSignerFailure(t *testing.T) {
	pub := ed25519.NewKeyFromSeed(mustHex(t, signatureVectors[0].seed)).Public().(ed25519.PublicKey)
	errHSM := errors.New("hsm unplugged")

	s, err := identity.FromCryptoSigner(fakeCryptoSigner{pub: pub, err: errHSM})
	require.NoError(t, err)
	_, err = s.Sign(context.Background(), []byte("msg"))
	require.ErrorIs(t, err, errHSM)

	s, err = identity.FromCryptoSigner(fakeCryptoSigner{pub: pub, sig: make([]byte, identity.SignatureSize-1)})
	require.NoError(t, err)
	_, err = s.Sign(context.Background(), []byte("msg"))
	require.Error(t, err)
}

func TestFromCryptoSignerKeyDisagrees(t *testing.T) {
	// The crypto.Signer reports TEST 2's key but signs with TEST 1's.
	signing := ed25519.NewKeyFromSeed(mustHex(t, signatureVectors[0].seed))
	reported := ed25519.NewKeyFromSeed(mustHex(t, signatureVectors[1].seed)).Public()
	s, err := identity.FromCryptoSigner(mismatchedCryptoSigner{pub: reported, priv: signing})
	require.NoError(t, err)

	sig, err := s.Sign(context.Background(), []byte("msg"))
	require.Error(t, err)
	require.Nil(t, sig)
}

// mismatchedCryptoSigner reports pub but signs with priv.
type mismatchedCryptoSigner struct {
	pub  crypto.PublicKey
	priv ed25519.PrivateKey
}

func (m mismatchedCryptoSigner) Public() crypto.PublicKey { return m.pub }

func (m mismatchedCryptoSigner) Sign(rand io.Reader, msg []byte, opts crypto.SignerOpts) ([]byte, error) {
	return m.priv.Sign(rand, msg, opts)
}

// fakeCryptoSigner is a crypto.Signer with a fixed public key that returns
// a fixed signature or error.
type fakeCryptoSigner struct {
	pub crypto.PublicKey
	sig []byte
	err error
}

func (f fakeCryptoSigner) Public() crypto.PublicKey { return f.pub }

func (f fakeCryptoSigner) Sign(io.Reader, []byte, crypto.SignerOpts) ([]byte, error) {
	return f.sig, f.err
}

// recordingCryptoSigner signs with priv and records what it was called
// with.
type recordingCryptoSigner struct {
	priv   ed25519.PrivateKey
	called bool
	rand   io.Reader
	opts   crypto.SignerOpts
}

func (r *recordingCryptoSigner) Public() crypto.PublicKey { return r.priv.Public() }

func (r *recordingCryptoSigner) Sign(rand io.Reader, msg []byte, opts crypto.SignerOpts) ([]byte, error) {
	r.called, r.rand, r.opts = true, rand, opts
	return r.priv.Sign(rand, msg, opts)
}
