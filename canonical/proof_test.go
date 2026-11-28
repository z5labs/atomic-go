package canonical_test

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/canonical"
	"github.com/z5labs/atomic-go/identity"
)

// RFC 8032 §7.1 TEST 1 and TEST 2 seeds.
const (
	seed1 = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	seed2 = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

func signerFromHex(t *testing.T, seed string) identity.Signer {
	t.Helper()
	b, err := hex.DecodeString(seed)
	require.NoError(t, err)
	s, err := identity.SignerFromSeed(b)
	require.NoError(t, err)
	return s
}

func admit(t *testing.T, in string) canonical.Document {
	t.Helper()
	d, err := canonical.Admit([]byte(in))
	require.NoError(t, err)
	return d
}

func canonicalString(t *testing.T, d canonical.Document) string {
	t.Helper()
	b, err := d.Canonical()
	require.NoError(t, err)
	return string(b)
}

func text(t *testing.T, d canonical.Document, name string) string {
	t.Helper()
	s, ok := d.Text(name)
	require.True(t, ok, "member %q", name)
	return s
}

// withProofMember returns d with one member of its proof set to v, leaving
// the rest of the proof as it was.
func withProofMember(t *testing.T, d canonical.Document, name string, v any) canonical.Document {
	t.Helper()
	var proof map[string]any
	require.NoError(t, d.Decode("proof", &proof))
	proof[name] = v
	d, err := d.With("proof", proof)
	require.NoError(t, err)
	return d
}

const goldenDocument = `{
  "@context": "https://atomic.dev/ns/ctx.jsonld",
  "@type": "AgentDelegation",
  "@id": "urn:atomic:delegation:golden",
  "issued": "2026-01-01T00:00:00Z"
}`

func attested(t *testing.T, seed string) (canonical.Document, identity.Signer) {
	t.Helper()
	s := signerFromHex(t, seed)
	d, err := canonical.Attest(context.Background(), admit(t, goldenDocument), s)
	require.NoError(t, err)
	return d, s
}

func TestConstants(t *testing.T) {
	require.Equal(t, "https://atomic.dev/ns/ctx.jsonld", canonical.Context)
	require.Equal(t, "DataIntegrityProof", canonical.ProofType)
	require.Equal(t, "eddsa-jcs-2022", canonical.Cryptosuite)
	require.Equal(t, "assertionMethod", canonical.ProofPurpose)
}

func TestAttestRoundTrip(t *testing.T) {
	for _, seed := range []string{seed1, seed2} {
		d, s := attested(t, seed)
		require.NoError(t, canonical.VerifyAttested(d, s.PublicKey()))

		// It survives the canonical bytes and admission again.
		again := admit(t, canonicalString(t, d))
		require.NoError(t, canonical.VerifyAttested(again, s.PublicKey()))

		p, err := canonical.ProofOf(d)
		require.NoError(t, err)
		require.Equal(t, canonical.ProofType, p.Type)
		require.Equal(t, canonical.Cryptosuite, p.Cryptosuite)
		require.Equal(t, canonical.ProofPurpose, p.ProofPurpose)
		require.Equal(t, s.PublicKey().AtomicDID().VerificationMethod(), p.VerificationMethod)

		did, err := p.Signer()
		require.NoError(t, err)
		require.Equal(t, s.PublicKey().AtomicDID(), did)

		sig, err := p.Signature()
		require.NoError(t, err)
		require.Len(t, sig, identity.SignatureSize)
	}
}

// The signing input, the contentHash and the proof for TEST 1, frozen.
// Generated in Go until Rust captures exist; Ed25519 is deterministic, so
// any change here is a change to what is signed.
const (
	goldenAttributedTo = "did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA"
	goldenContentHash  = "blake3:95b5363ec79fc460031e58e673dd973713bcfe4e4f1836f2c583f7059735dae2"
	goldenSigningInput = `{"@context":"https://atomic.dev/ns/ctx.jsonld","@id":"urn:atomic:delegation:golden","@type":"AgentDelegation","attributedTo":"` + goldenAttributedTo + `","contentHash":"` + goldenContentHash + `","issued":"2026-01-01T00:00:00Z"}`
	goldenProofValue   = "z2kMJyAav7zzCBMuofFRa9cPWRZh9Fdd6tA371Yn6MMcUkiTWBKbY5wRJ3LYn1D8yETbEBf6a37o1u6vnjwupa9kG"
)

func TestAttestGolden(t *testing.T) {
	d, s := attested(t, seed1)
	require.Equal(t, goldenAttributedTo, s.PublicKey().AtomicDID().String())
	require.Equal(t, goldenAttributedTo, text(t, d, "attributedTo"))
	require.Equal(t, goldenContentHash, text(t, d, "contentHash"))
	require.Equal(t, goldenSigningInput, canonicalString(t, d.Without("proof")))

	hash, err := canonical.ContentHash(d)
	require.NoError(t, err)
	require.Equal(t, goldenContentHash, hash)

	p, err := canonical.ProofOf(d)
	require.NoError(t, err)
	require.Equal(t, goldenProofValue, p.ProofValue)

	sig, err := p.Signature()
	require.NoError(t, err)
	require.True(t, s.PublicKey().Verify([]byte(goldenSigningInput), sig))
}

func TestContentHashIgnoresProofAndContentHash(t *testing.T) {
	d, _ := attested(t, seed1)
	bare := admit(t, goldenDocument)
	bare, err := bare.With("attributedTo", goldenAttributedTo)
	require.NoError(t, err)

	got, err := canonical.ContentHash(d)
	require.NoError(t, err)
	want, err := canonical.ContentHash(bare)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.True(t, strings.HasPrefix(got, "blake3:"))
	require.Len(t, got, len("blake3:")+64)
}

func TestAttestKeepsAPresetAttributedTo(t *testing.T) {
	s := signerFromHex(t, seed1)
	other := signerFromHex(t, seed2).PublicKey().AtomicDID().String()
	d, err := admit(t, goldenDocument).With("attributedTo", other)
	require.NoError(t, err)

	d, err = canonical.Attest(context.Background(), d, s)
	require.NoError(t, err)
	require.Equal(t, other, text(t, d, "attributedTo"))
	require.NoError(t, canonical.VerifyAttested(d, s.PublicKey()))
}

func TestAttestReplacesAnEmptyAttributedTo(t *testing.T) {
	s := signerFromHex(t, seed1)
	for name, v := range map[string]string{
		"empty string": `""`,
		"null":         `null`,
		"not a string": `{"@id":"did:atomic:x"}`,
	} {
		d := admit(t, `{"@type":"AgentDelegation","attributedTo":`+v+`}`)
		d, err := canonical.Attest(context.Background(), d, s)
		require.NoError(t, err, name)
		require.Equal(t, goldenAttributedTo, text(t, d, "attributedTo"), name)
		require.NoError(t, canonical.VerifyAttested(d, s.PublicKey()), name)
	}
}

func TestAttestReplacesAnOldProofAndContentHash(t *testing.T) {
	d, s := attested(t, seed1)
	stale, err := d.With("contentHash", "blake3:00")
	require.NoError(t, err)
	stale = withProofMember(t, stale, "proofValue", "zstale")

	again, err := canonical.Attest(context.Background(), stale, s)
	require.NoError(t, err)
	require.Equal(t, canonicalString(t, d), canonicalString(t, again))
}

func TestAttestLeavesItsInputUnchanged(t *testing.T) {
	in := admit(t, goldenDocument)
	before := canonicalString(t, in)
	_, err := canonical.Attest(context.Background(), in, signerFromHex(t, seed1))
	require.NoError(t, err)
	require.Equal(t, before, canonicalString(t, in))
}

func TestAttestReturnsTheSignerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := canonical.Attest(ctx, admit(t, goldenDocument), signerFromHex(t, seed1))
	require.ErrorIs(t, err, context.Canceled)

	_, err = canonical.Sign(ctx, admit(t, goldenDocument), signerFromHex(t, seed1))
	require.ErrorIs(t, err, context.Canceled)
}

func TestAttestAndSignRefuseANilSigner(t *testing.T) {
	_, err := canonical.Attest(context.Background(), admit(t, goldenDocument), nil)
	require.Error(t, err)
	_, err = canonical.Sign(context.Background(), admit(t, goldenDocument), nil)
	require.Error(t, err)
}

// shortSigner holds a real key and returns a signature that is not 64
// bytes.
type shortSigner struct {
	key identity.PublicKey
}

func (s shortSigner) PublicKey() identity.PublicKey {
	return s.key
}

func (shortSigner) Sign(context.Context, []byte) ([]byte, error) {
	return make([]byte, identity.SignatureSize-1), nil
}

func TestAttestAndSignRefuseAShortSignature(t *testing.T) {
	s := shortSigner{key: signerFromHex(t, seed1).PublicKey()}
	_, err := canonical.Attest(context.Background(), admit(t, goldenDocument), s)
	require.ErrorContains(t, err, "signature is 63 bytes")
	_, err = canonical.Sign(context.Background(), admit(t, goldenDocument), s)
	require.ErrorContains(t, err, "signature is 63 bytes")
}

func TestVerifyAttestedRefusesTampering(t *testing.T) {
	d, s := attested(t, seed1)
	for name, edit := range map[string]func(canonical.Document) (canonical.Document, error){
		"changed member": func(d canonical.Document) (canonical.Document, error) {
			return d.With("issued", "2026-01-02T00:00:00Z")
		},
		"added member": func(d canonical.Document) (canonical.Document, error) {
			return d.With("expires", "2027-01-01T00:00:00Z")
		},
		"removed member": func(d canonical.Document) (canonical.Document, error) {
			return d.Without("@id"), nil
		},
		"changed attributedTo": func(d canonical.Document) (canonical.Document, error) {
			return d.With("attributedTo", "did:atomic:other")
		},
		"no contentHash": func(d canonical.Document) (canonical.Document, error) {
			return d.Without("contentHash"), nil
		},
		"contentHash not a string": func(d canonical.Document) (canonical.Document, error) {
			return d.With("contentHash", 1)
		},
	} {
		tampered, err := edit(d)
		require.NoError(t, err, name)
		require.ErrorIs(t, canonical.VerifyAttested(tampered, s.PublicKey()), canonical.ErrContentHash, name)
	}
}

func TestVerifyAttestedRefusesARehashedEdit(t *testing.T) {
	// An edit with its contentHash recomputed passes step 1 and fails on
	// the signature.
	d, s := attested(t, seed1)
	d, err := d.With("issued", "2026-01-02T00:00:00Z")
	require.NoError(t, err)
	hash, err := canonical.ContentHash(d)
	require.NoError(t, err)
	d, err = d.With("contentHash", hash)
	require.NoError(t, err)

	err = canonical.VerifyAttested(d, s.PublicKey())
	require.ErrorIs(t, err, canonical.ErrProof)
	require.NotErrorIs(t, err, canonical.ErrContentHash)
}

func TestVerifyRefusesChangedProofConstants(t *testing.T) {
	d, s := attested(t, seed1)
	for _, member := range []string{"@type", "cryptosuite", "proofPurpose"} {
		changed := withProofMember(t, d, member, "something-else")

		// The signature does not cover the proof, so it still verifies...
		p, err := canonical.ProofOf(changed)
		require.NoError(t, err, member)
		sig, err := p.Signature()
		require.NoError(t, err, member)
		msg, err := changed.Without("proof").Canonical()
		require.NoError(t, err, member)
		require.True(t, s.PublicKey().Verify(msg, sig), member)

		// ...and the constants are what refuse it.
		require.ErrorIs(t, canonical.VerifyAttested(changed, s.PublicKey()), canonical.ErrProof, member)
		require.ErrorIs(t, canonical.VerifyProof(changed, s.PublicKey()), canonical.ErrProof, member)
	}
}

func TestProofRefusesUnknownMembers(t *testing.T) {
	d, s := attested(t, seed1)
	d = withProofMember(t, d, "created", "2026-01-01T00:00:00Z")

	_, err := canonical.ProofOf(d)
	require.ErrorIs(t, err, canonical.ErrProof)
	require.ErrorIs(t, canonical.VerifyAttested(d, s.PublicKey()), canonical.ErrProof)
}

func TestProofOfRefuses(t *testing.T) {
	d, _ := attested(t, seed1)
	var proof map[string]any
	require.NoError(t, d.Decode("proof", &proof))

	cases := map[string]canonical.Document{
		"no proof": d.Without("proof"),
	}
	for _, member := range []string{"@type", "cryptosuite", "proofPurpose", "proofValue", "verificationMethod"} {
		missing := map[string]any{}
		for k, v := range proof {
			if k != member {
				missing[k] = v
			}
		}
		md, err := d.With("proof", missing)
		require.NoError(t, err)
		cases["missing "+member] = md
		cases["null "+member] = withProofMember(t, d, member, nil)
		cases["non-string "+member] = withProofMember(t, d, member, 7)
	}
	notObject, err := d.With("proof", "z")
	require.NoError(t, err)
	cases["not an object"] = notObject

	for name, doc := range cases {
		_, err := canonical.ProofOf(doc)
		require.ErrorIs(t, err, canonical.ErrProof, name)
	}
}

func TestVerifyAcceptsADIDKeyVerificationMethod(t *testing.T) {
	d, s := attested(t, seed1)
	d = withProofMember(t, d, "verificationMethod", s.PublicKey().KeyDID().VerificationMethod())

	require.NoError(t, canonical.VerifyAttested(d, s.PublicKey()))
	require.NoError(t, canonical.VerifyProof(d, s.PublicKey()))

	p, err := canonical.ProofOf(d)
	require.NoError(t, err)
	did, err := p.Signer()
	require.NoError(t, err)
	require.Equal(t, s.PublicKey().KeyDID(), did)
}

func TestSignerReadsTheDIDBeforeTheFragment(t *testing.T) {
	// Atomic reads the verification method's DID as the part before the
	// first "#", whatever the fragment is, or with none at all.
	d, s := attested(t, seed1)
	did := s.PublicKey().AtomicDID().String()
	for _, vm := range []string{did, did + "#key-2", did + "#"} {
		named := withProofMember(t, d, "verificationMethod", vm)
		require.NoError(t, canonical.VerifyAttested(named, s.PublicKey()), vm)

		p, err := canonical.ProofOf(named)
		require.NoError(t, err, vm)
		got, err := p.Signer()
		require.NoError(t, err, vm)
		require.Equal(t, s.PublicKey().AtomicDID(), got, vm)
	}

	_, err := canonical.Proof{VerificationMethod: "#key-1"}.Signer()
	require.ErrorIs(t, err, canonical.ErrProof)
	require.ErrorIs(t, err, identity.ErrInvalidDID)
}

func TestVerifyRefusesAnotherKey(t *testing.T) {
	d, s := attested(t, seed1)
	other := signerFromHex(t, seed2).PublicKey()

	// Verified against another key, the signature fails.
	require.ErrorIs(t, canonical.VerifyAttested(d, other), canonical.ErrProof)

	// A verificationMethod naming another key fails, in either form, even
	// though the signature is the right key's.
	for _, did := range []identity.DID{other.AtomicDID(), other.KeyDID()} {
		named := withProofMember(t, d, "verificationMethod", did.VerificationMethod())
		require.ErrorIs(t, canonical.VerifyAttested(named, s.PublicKey()), canonical.ErrProof, did.String())
	}

	// So does one that is not a DID.
	named := withProofMember(t, d, "verificationMethod", "did:web:example.com#key-1")
	require.ErrorIs(t, canonical.VerifyAttested(named, s.PublicKey()), canonical.ErrProof)
}

func TestVerifyRefusesABadProofValue(t *testing.T) {
	d, s := attested(t, seed1)
	p, err := canonical.ProofOf(d)
	require.NoError(t, err)
	body := strings.TrimPrefix(p.ProofValue, "z")

	for name, v := range map[string]string{
		"empty":             "",
		"no multibase":      body,
		"other multibase":   "u" + body,
		"not base58":        "z0OIl",
		"too short":         "z" + body[:len(body)-4],
		"too long":          "z" + body + "2222",
		"another signature": "z" + strings.Repeat("2", len(body)),
	} {
		bad := withProofMember(t, d, "proofValue", v)
		require.ErrorIs(t, canonical.VerifyAttested(bad, s.PublicKey()), canonical.ErrProof, name)

		if name != "another signature" {
			bp, err := canonical.ProofOf(bad)
			require.NoError(t, err, name)
			_, err = bp.Signature()
			require.ErrorIs(t, err, canonical.ErrProof, name)
		}
	}
}

func TestSignRoundTrip(t *testing.T) {
	for _, seed := range []string{seed1, seed2} {
		s := signerFromHex(t, seed)
		in := admit(t, goldenDocument)
		d, err := canonical.Sign(context.Background(), in, s)
		require.NoError(t, err)

		require.False(t, d.Has("contentHash"))
		require.False(t, d.Has("attributedTo"))
		require.Equal(t, canonicalString(t, in), canonicalString(t, d.Without("proof")))
		require.NoError(t, canonical.VerifyProof(d, s.PublicKey()))
		require.NoError(t, canonical.VerifyProof(admit(t, canonicalString(t, d)), s.PublicKey()))

		// A bare proof carries no contentHash to verify.
		require.ErrorIs(t, canonical.VerifyAttested(d, s.PublicKey()), canonical.ErrContentHash)

		p, err := canonical.ProofOf(d)
		require.NoError(t, err)
		sig, err := p.Signature()
		require.NoError(t, err)
		require.True(t, s.PublicKey().Verify([]byte(canonicalString(t, in)), sig))
	}
}

func TestSignReplacesAnOldProof(t *testing.T) {
	s := signerFromHex(t, seed1)
	d, err := canonical.Sign(context.Background(), admit(t, goldenDocument), s)
	require.NoError(t, err)
	again, err := canonical.Sign(context.Background(), withProofMember(t, d, "proofValue", "zstale"), s)
	require.NoError(t, err)
	require.Equal(t, canonicalString(t, d), canonicalString(t, again))
}

func TestVerifyProofFailsAfterAnEdit(t *testing.T) {
	s := signerFromHex(t, seed1)
	d, err := canonical.Sign(context.Background(), admit(t, goldenDocument), s)
	require.NoError(t, err)

	edited, err := d.With("issued", "2026-01-02T00:00:00Z")
	require.NoError(t, err)
	require.ErrorIs(t, canonical.VerifyProof(edited, s.PublicKey()), canonical.ErrProof)

	require.ErrorIs(t, canonical.VerifyProof(d.Without("@id"), s.PublicKey()), canonical.ErrProof)
	require.ErrorIs(t, canonical.VerifyProof(d.Without("proof"), s.PublicKey()), canonical.ErrProof)
	require.ErrorIs(t, canonical.VerifyProof(d, signerFromHex(t, seed2).PublicKey()), canonical.ErrProof)
}
