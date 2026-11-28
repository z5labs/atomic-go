package change

import (
	"context"
	"encoding/hex"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/internal/changefixture"
	"github.com/z5labs/atomic-go/internal/postcard"
	"github.com/z5labs/atomic-go/zstdframe"
)

// The signer DID, signed hash and timestamp of the SIGNATURE example in
// docs/remote/change-format/sections.md#changesignature. The example shows
// only the ends of the signature, so the test uses a signature of its own.
const (
	exampleSignerDID  = "did:atomic:45LZQUIZNX5VFO7UXZHBOU4LMUB6U62NZEUN6QP2VPQ7ANLLTC2Q"
	exampleSignedHash = "1412764afb5f3460f3a0e4f99ac6c5f0608d5fe4cc462f82fb8e4eb06ea3c013"
	exampleTimestamp  = 1794500600
)

// RFC 8032 §7.1 TEST 1, and the did:atomic DID of its key from
// docs/remote/auth.md#did-forms.
const (
	rfc8032Seed      = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	rfc8032AtomicDID = "did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA"
)

func exampleSignature(t *testing.T) Signature {
	t.Helper()
	h, err := ident.ParseHashHex(exampleSignedHash)
	require.NoError(t, err)
	s := Signature{
		SignerDID:  exampleSignerDID,
		SignedHash: h,
		Timestamp:  exampleTimestamp,
		Method:     MethodEd25519,
	}
	for i := range s.Value {
		s.Value[i] = byte(i + 1)
	}
	return s
}

// examplePayload returns the 175-byte payload of exampleSignature, written
// out field by field from the documented layout.
func examplePayload(t *testing.T) []byte {
	t.Helper()
	s := exampleSignature(t)
	b := []byte{0x3f}
	b = append(b, exampleSignerDID...)
	b = append(b, 0x40)
	b = append(b, s.Value[:]...)
	b = append(b, 0x20)
	b = append(b, s.SignedHash[:]...)
	b = append(b, 0xf0, 0x9f, 0xaf, 0xaf, 0x0d)
	b = append(b, 0x07)
	b = append(b, "ed25519"...)
	return b
}

func rfc8032Signer(t *testing.T) identity.Signer {
	t.Helper()
	seed, err := hex.DecodeString(rfc8032Seed)
	require.NoError(t, err)
	s, err := identity.SignerFromSeed(seed)
	require.NoError(t, err)
	return s
}

func TestSignaturePayloadLayout(t *testing.T) {
	payload := examplePayload(t)
	require.Len(t, payload, 175)
	require.Len(t, exampleSignerDID, 0x3f)

	want := exampleSignature(t)
	got, err := DecodeSignature(payload)
	require.NoError(t, err)
	require.Equal(t, want, got)

	enc, err := want.AppendPayload(nil)
	require.NoError(t, err)
	require.Equal(t, payload, enc)

	t.Run("AppendPayload appends to dst", func(t *testing.T) {
		enc, err := want.AppendPayload([]byte{0xAB})
		require.NoError(t, err)
		require.Equal(t, append([]byte{0xAB}, payload...), enc)
	})

	t.Run("bytes after the value are ignored", func(t *testing.T) {
		got, err := DecodeSignature(append(examplePayload(t), 0xEE))
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("the decoded value does not alias the payload", func(t *testing.T) {
		b := examplePayload(t)
		got, err := DecodeSignature(b)
		require.NoError(t, err)
		clear(b)
		require.Equal(t, want, got)
	})
}

func TestDecodeSignatureRefuses(t *testing.T) {
	did := append([]byte{0x3f}, exampleSignerDID...)
	tail := []byte{0xf0, 0x9f, 0xaf, 0xaf, 0x0d, 0x07}
	tail = append(tail, "ed25519"...)

	tests := []struct {
		name    string
		payload []byte
		length  bool
	}{
		{
			name: "a 63-byte signature",
			payload: slices.Concat(did,
				[]byte{0x3f}, make([]byte, 63),
				[]byte{0x20}, make([]byte, 32), tail),
			length: true,
		},
		{
			name: "a 33-byte signed hash",
			payload: slices.Concat(did,
				[]byte{0x40}, make([]byte, 64),
				[]byte{0x21}, make([]byte, 33), tail),
			length: true,
		},
		{
			name:    "a payload that ends early",
			payload: examplePayload(t)[:100],
		},
		{
			name:    "an empty payload",
			payload: nil,
		},
		{
			name:    "a signer DID that is not UTF-8",
			payload: slices.Concat([]byte{0x01, 0xff}, examplePayload(t)[1+0x3f:]),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeSignature(tc.payload)
			requireFormatError(t, err, changefile.ErrPayload, changefile.SectionSignature)
			if tc.length {
				require.ErrorIs(t, err, postcard.ErrBadLength)
			}
		})
	}
}

func TestAppendPayloadRefusesInvalidUTF8(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Signature)
	}{
		{"signer DID", func(s *Signature) { s.SignerDID = "\xff" }},
		{"method", func(s *Signature) { s.Method = "\xff" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := exampleSignature(t)
			tc.edit(&s)
			dst := []byte{0xAB}
			got, err := s.AppendPayload(dst)
			requireFormatError(t, err, changefile.ErrPayload, changefile.SectionSignature)
			require.Equal(t, dst, got)
		})
	}
}

func TestSignedMessage(t *testing.T) {
	h := hashOf(7)
	want := append([]byte("atomic.change.signature.v1\x00"), h[:]...)
	require.Equal(t, want, SignedMessage(h))
	require.Equal(t, "atomic.change.signature.v1", SignatureDomain)
}

func TestNewSignatureSignsAndVerifies(t *testing.T) {
	signer := rfc8032Signer(t)
	change := hashOf(9)
	at := time.Date(2026, 11, 12, 16, 23, 20, 999, time.UTC)

	s, err := NewSignature(context.Background(), change, signer, at)
	require.NoError(t, err)
	require.Equal(t, rfc8032AtomicDID, s.SignerDID)
	require.Equal(t, change, s.SignedHash)
	require.Equal(t, int64(exampleTimestamp), s.Timestamp)
	require.Equal(t, MethodEd25519, s.Method)
	require.NoError(t, VerifySignature(s, change, signer.PublicKey()))

	payload, err := s.AppendPayload(nil)
	require.NoError(t, err)
	decoded, err := DecodeSignature(payload)
	require.NoError(t, err)
	require.NoError(t, VerifySignature(decoded, change, signer.PublicKey()))

	other, err := identity.SignerFromSeed(make([]byte, identity.SeedSize))
	require.NoError(t, err)

	tests := []struct {
		name   string
		edit   func(*Signature)
		change ident.Hash
		key    identity.PublicKey
	}{
		{
			name:   "a changed change hash",
			change: hashOf(10),
			key:    signer.PublicKey(),
		},
		{
			name:   "a changed signed hash",
			edit:   func(s *Signature) { s.SignedHash = hashOf(10) },
			change: change,
			key:    signer.PublicKey(),
		},
		{
			name:   "a changed method",
			edit:   func(s *Signature) { s.Method = "ed448" },
			change: change,
			key:    signer.PublicKey(),
		},
		{
			name:   "another key",
			change: change,
			key:    other.PublicKey(),
		},
		{
			name:   "a changed signature value",
			edit:   func(s *Signature) { s.Value[0] ^= 1 },
			change: change,
			key:    signer.PublicKey(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name+" fails", func(t *testing.T) {
			bad := s
			if tc.edit != nil {
				tc.edit(&bad)
			}
			err := VerifySignature(bad, tc.change, tc.key)
			require.ErrorIs(t, err, ErrInvalidSignature)
		})
	}

	t.Run("the signer DID is not consulted", func(t *testing.T) {
		hinted := s
		hinted.SignerDID = other.PublicKey().AtomicDID().String()
		require.NoError(t, VerifySignature(hinted, change, signer.PublicKey()))
		require.ErrorIs(t, VerifySignature(hinted, change, other.PublicKey()), ErrInvalidSignature)
	})
}

// failingSigner is an identity.Signer whose Sign returns err, or sig when
// err is nil.
type failingSigner struct {
	key identity.PublicKey
	sig []byte
	err error
}

func (s failingSigner) PublicKey() identity.PublicKey { return s.key }

func (s failingSigner) Sign(context.Context, []byte) ([]byte, error) {
	return s.sig, s.err
}

func TestNewSignatureFailures(t *testing.T) {
	signer := rfc8032Signer(t)
	errBoom := errors.New("boom")

	t.Run("a failing Signer propagates", func(t *testing.T) {
		_, err := NewSignature(context.Background(), hashOf(1), failingSigner{key: signer.PublicKey(), err: errBoom}, time.Unix(0, 0))
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("a short signature is refused", func(t *testing.T) {
		_, err := NewSignature(context.Background(), hashOf(1), failingSigner{key: signer.PublicKey(), sig: make([]byte, 63)}, time.Unix(0, 0))
		require.Error(t, err)
	})

	t.Run("a signature that does not verify is refused", func(t *testing.T) {
		_, err := NewSignature(context.Background(), hashOf(1), failingSigner{key: signer.PublicKey(), sig: make([]byte, 64)}, time.Unix(0, 0))
		require.Error(t, err)
	})

	t.Run("a nil Signer is refused", func(t *testing.T) {
		_, err := NewSignature(context.Background(), hashOf(1), nil, time.Unix(0, 0))
		require.Error(t, err)
	})

	t.Run("a done context is returned", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewSignature(ctx, hashOf(1), signer, time.Unix(0, 0))
		require.ErrorIs(t, err, context.Canceled)
	})
}

func parseFile(t *testing.T, b []byte) *changefile.File {
	t.Helper()
	dec, err := zstdframe.NewDecoder(zstdframe.AtomicWindow)
	require.NoError(t, err)
	t.Cleanup(dec.Close)
	f, err := changefile.Parse(b, dec)
	require.NoError(t, err)
	return f
}

func TestSignWorkedExample(t *testing.T) {
	signer := rfc8032Signer(t)
	want, err := ident.ParseHash(changefixture.WorkedExampleHash)
	require.NoError(t, err)

	f := parseFile(t, changefixture.WorkedExample())
	require.Equal(t, want, f.Hash)
	require.False(t, f.Header.Flags.Has(changefile.FlagSignature))

	at := time.Unix(exampleTimestamp, 0)
	out, err := Sign(context.Background(), f, signer, at, zstdframe.Stored{})
	require.NoError(t, err)

	h, err := changefile.Verify(out)
	require.NoError(t, err)
	require.Equal(t, want, h)

	signed := parseFile(t, out)
	require.Equal(t, want, signed.Hash)
	require.True(t, signed.Header.Flags.Has(changefile.FlagSignature))

	var payloads [][]byte
	for _, s := range signed.Sections {
		if s.Type == changefile.SectionSignature {
			payloads = append(payloads, s.Payload)
		}
	}
	require.Len(t, payloads, 1)
	sig, err := DecodeSignature(payloads[0])
	require.NoError(t, err)
	require.Equal(t, rfc8032AtomicDID, sig.SignerDID)
	require.Equal(t, want, sig.SignedHash)
	require.Equal(t, int64(exampleTimestamp), sig.Timestamp)
	require.NoError(t, VerifySignature(sig, signed.Hash, signer.PublicKey()))

	t.Run("signing again replaces the signature", func(t *testing.T) {
		again, err := Sign(context.Background(), signed, signer, at.Add(time.Hour), zstdframe.Stored{})
		require.NoError(t, err)
		f := parseFile(t, again)
		require.Equal(t, want, f.Hash)
		var n int
		for _, s := range f.Sections {
			if s.Type == changefile.SectionSignature {
				n++
				sig, err := DecodeSignature(s.Payload)
				require.NoError(t, err)
				require.Equal(t, int64(exampleTimestamp+3600), sig.Timestamp)
			}
		}
		require.Equal(t, 1, n)
	})

	t.Run("a failing Signer propagates", func(t *testing.T) {
		errBoom := errors.New("boom")
		_, err := Sign(context.Background(), f, failingSigner{key: signer.PublicKey(), err: errBoom}, at, zstdframe.Stored{})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("a nil file is refused", func(t *testing.T) {
		_, err := Sign(context.Background(), nil, signer, at, zstdframe.Stored{})
		require.ErrorIs(t, err, changefile.ErrNilFile)
	})

	t.Run("a nil compressor is refused", func(t *testing.T) {
		_, err := Sign(context.Background(), f, signer, at, nil)
		require.ErrorIs(t, err, changefile.ErrNilCompressor)
	})
}
