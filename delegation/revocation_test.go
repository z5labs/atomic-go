package delegation_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/canonical"
	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
)

// goldenRevocationJCS is the human's revocation of the golden certificate,
// revoked at goldenRevokedAt for goldenReason. Like goldenJCS it was
// computed independently of this package and of canonical: the JSON written
// out by hand in JCS order, hashed with BLAKE3 and signed with
// crypto/ed25519.
const goldenRevocationJCS = `{"@context":"https://atomic.dev/ns/ctx.jsonld",` +
	`"@type":"DelegationRevocation",` +
	`"attributedTo":"did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA",` +
	`"contentHash":"blake3:0cf8b22b159bac8eba8f4ea01292b6d5e268fef85f5ba93ecf3a776068ed00f0",` +
	`"delegation":"urn:atomic:delegation:7XFVC6H4XTSGCGH7NTOIWXLWVFZPTCO5LCK4R5C7JA2VBT47WNBQ",` +
	`"delegator":"did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA",` +
	`"proof":{"@type":"DataIntegrityProof","cryptosuite":"eddsa-jcs-2022","proofPurpose":"assertionMethod",` +
	`"proofValue":"z5YghsiF5oX9afdWXysJ9rAtBYFK77vqKXHWHWnQ6yaubMudvJEELjattmxLQbhKTSfw7DUdRvWktL8iGo9NUaADN",` +
	`"verificationMethod":"did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA#key-1"},` +
	`"reason":"agent key rotated",` +
	`"revokedAt":"2026-11-25T09:30:00Z"}`

const goldenReason = "agent key rotated"

func goldenRevokedAt() time.Time {
	return time.Date(2026, time.November, 25, 9, 30, 0, 0, time.UTC)
}

func goldenDelegationID(t *testing.T) delegation.ID {
	t.Helper()
	id, err := delegation.ParseID(goldenID)
	require.NoError(t, err)
	return id
}

func goldenRevocation(t *testing.T) delegation.Revocation {
	t.Helper()
	r, err := delegation.ParseRevocation([]byte(goldenRevocationJCS))
	require.NoError(t, err)
	return r
}

// revocationWith returns the golden revocation's document with member name
// set to v.
func revocationWith(t *testing.T, name string, v any) canonical.Document {
	t.Helper()
	d, err := goldenRevocation(t).Document().With(name, v)
	require.NoError(t, err)
	return d
}

// attestRevocation signs d as s and reads it back as a revocation.
func attestRevocation(t *testing.T, d canonical.Document, s identity.Signer) delegation.Revocation {
	t.Helper()
	signed, err := canonical.Attest(context.Background(), d, s)
	require.NoError(t, err)
	r, err := delegation.RevocationFromDocument(signed)
	require.NoError(t, err)
	return r
}

func TestDocumentTypes(t *testing.T) {
	require.Equal(t, "DelegationRevocation", delegation.TypeRevocation)
	require.Equal(t, "AgentDelegationRequest", delegation.TypeRequest)
}

func TestRevokeGolden(t *testing.T) {
	// The fraction of a second is truncated, and the zone dropped for UTC.
	at := goldenRevokedAt().Add(700 * time.Millisecond).In(time.FixedZone("CET", 3600))
	r, err := delegation.Revoke(context.Background(), human(t), goldenDelegationID(t), at, goldenReason)
	require.NoError(t, err)

	b, err := r.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, goldenRevocationJCS, string(b))
	require.NoError(t, r.Verify(human(t).PublicKey()))
}

func TestParseRevocationGolden(t *testing.T) {
	r := goldenRevocation(t)
	require.NoError(t, r.Verify(human(t).PublicKey()))

	v := r.DelegationRevocation()
	h := human(t).PublicKey()
	require.Equal(t, canonical.Context, v.Context)
	require.Equal(t, delegation.TypeRevocation, v.Type)
	require.Equal(t, h.AtomicDID().String(), v.AttributedTo)
	require.Equal(t, "blake3:0cf8b22b159bac8eba8f4ea01292b6d5e268fef85f5ba93ecf3a776068ed00f0", v.ContentHash)
	require.Equal(t, goldenDelegationID(t), v.Delegation)
	require.Equal(t, h.AtomicDID(), v.Delegator)
	require.Equal(t, h.AtomicDID().VerificationMethod(), v.Proof.VerificationMethod)
	require.Equal(t, goldenReason, v.Reason)
	require.Equal(t, goldenRevokedAt(), v.RevokedAt)
}

func TestRevocationRoundTrip(t *testing.T) {
	ctx := context.Background()
	c, err := delegation.Issue(ctx, human(t), goldenGrant(t))
	require.NoError(t, err)

	r, err := delegation.Revoke(ctx, human(t), c.ID(), goldenRevokedAt(), "")
	require.NoError(t, err)
	b, err := r.MarshalJSON()
	require.NoError(t, err)
	require.NotContains(t, string(b), `"reason"`)

	back, err := delegation.ParseRevocation(b)
	require.NoError(t, err)
	require.NoError(t, back.Verify(human(t).PublicKey()))
	require.Equal(t, r.DelegationRevocation(), back.DelegationRevocation())
	require.Equal(t, c.ID(), back.DelegationRevocation().Delegation)
	require.Empty(t, back.DelegationRevocation().Reason)

	again, err := back.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, b, again)
}

func TestAgentSignedRevocationFails(t *testing.T) {
	r, err := delegation.Revoke(context.Background(), agent(t), goldenDelegationID(t), goldenRevokedAt(), goldenReason)
	require.NoError(t, err)
	require.NoError(t, r.Verify(agent(t).PublicKey()))

	err = r.Verify(human(t).PublicKey())
	require.ErrorIs(t, err, delegation.ErrVerifyRevocation)
	require.ErrorIs(t, err, canonical.ErrProof)
}

func TestRevocationVerifySteps(t *testing.T) {
	h, g := human(t).PublicKey(), agent(t).PublicKey()

	tests := map[string]struct {
		rev func(t *testing.T) delegation.Revocation
		is  error
		msg string
	}{
		"1 @type": {
			rev: func(t *testing.T) delegation.Revocation {
				return attestRevocation(t, revocationWith(t, "@type", delegation.TypeAgentDelegation), human(t))
			},
			msg: "@type",
		},
		"2 contentHash": {
			rev: func(t *testing.T) delegation.Revocation {
				r, err := delegation.RevocationFromDocument(revocationWith(t, "reason", "no reason"))
				require.NoError(t, err)
				return r
			},
			is: canonical.ErrContentHash,
		},
		"2 signature": {
			rev: func(t *testing.T) delegation.Revocation {
				d := revocationWith(t, "revokedAt", "2026-11-26T09:30:00Z")
				hash, err := canonical.ContentHash(d)
				require.NoError(t, err)
				d, err = d.With("contentHash", hash)
				require.NoError(t, err)
				r, err := delegation.RevocationFromDocument(d)
				require.NoError(t, err)
				return r
			},
			is: canonical.ErrProof,
		},
		"3 delegator names another key": {
			rev: func(t *testing.T) delegation.Revocation {
				return attestRevocation(t, revocationWith(t, "delegator", g.AtomicDID().String()), human(t))
			},
			msg: "delegator",
		},
		"3 delegator as did:key": {
			rev: func(t *testing.T) delegation.Revocation {
				return attestRevocation(t, revocationWith(t, "delegator", h.KeyDID().String()), human(t))
			},
			msg: "delegator",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.rev(t).Verify(h)
			require.ErrorIs(t, err, delegation.ErrVerifyRevocation)
			if tc.is != nil {
				require.ErrorIs(t, err, tc.is)
			}
			if tc.msg != "" {
				require.ErrorContains(t, err, tc.msg)
				require.NotErrorIs(t, err, canonical.ErrProof, "the step before failed instead")
			}
		})
	}
}

func TestZeroRevocationDoesNotVerify(t *testing.T) {
	require.ErrorIs(t, delegation.Revocation{}.Verify(human(t).PublicKey()), delegation.ErrVerifyRevocation)
}

func TestRevokeRefuses(t *testing.T) {
	ctx := context.Background()
	id := goldenDelegationID(t)

	_, err := delegation.Revoke(ctx, nil, id, goldenRevokedAt(), "")
	require.ErrorIs(t, err, delegation.ErrInvalidRevocation)

	_, err = delegation.Revoke(ctx, human(t), id, time.Time{}, "")
	require.ErrorIs(t, err, delegation.ErrInvalidRevocation)
}

func TestRevokeChecksTheSigner(t *testing.T) {
	s := liar{claimed: human(t).PublicKey(), signer: agent(t)}
	_, err := delegation.Revoke(context.Background(), s, goldenDelegationID(t), goldenRevokedAt(), "")
	require.ErrorIs(t, err, delegation.ErrVerifyRevocation)
}

func TestRevokeSignerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := delegation.Revoke(ctx, human(t), goldenDelegationID(t), goldenRevokedAt(), "")
	require.ErrorIs(t, err, context.Canceled)
}

func TestParseRevocationRefuses(t *testing.T) {
	without := func(name string) func(t *testing.T) canonical.Document {
		return func(t *testing.T) canonical.Document {
			return goldenRevocation(t).Document().Without(name)
		}
	}
	set := func(name string, v any) func(t *testing.T) canonical.Document {
		return func(t *testing.T) canonical.Document {
			return revocationWith(t, name, v)
		}
	}
	tests := map[string]func(t *testing.T) canonical.Document{
		"no @type":                  without("@type"),
		"no contentHash":            without("contentHash"),
		"no delegation":             without("delegation"),
		"no delegator":              without("delegator"),
		"no proof":                  without("proof"),
		"no revokedAt":              without("revokedAt"),
		"@type not a string":        set("@type", 1),
		"delegation not a URN":      set("delegation", goldenID),
		"delegation not an id":      set("delegation", delegation.URNPrefix+"AAAA"),
		"delegator not a DID":       set("delegator", "alice"),
		"revokedAt not a time":      set("revokedAt", "yesterday"),
		"reason not a string":       set("reason", 7),
		"attributedTo not a string": set("attributedTo", true),
		"proof not a proof":         set("proof", "z123"),
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := delegation.RevocationFromDocument(doc(t))
			require.ErrorIs(t, err, delegation.ErrInvalidRevocation)
		})
	}

	t.Run("not admitted", func(t *testing.T) {
		_, err := delegation.ParseRevocation([]byte(`{"a":1,"a":2}`))
		require.ErrorIs(t, err, delegation.ErrInvalidRevocation)
		require.ErrorIs(t, err, canonical.ErrNotAdmitted)
	})
}

func TestRevocationOptionalMembersMayBeNull(t *testing.T) {
	d := revocationWith(t, "reason", jsontext.Value("null"))
	d, err := d.With("attributedTo", jsontext.Value("null"))
	require.NoError(t, err)

	r, err := delegation.RevocationFromDocument(d)
	require.NoError(t, err)
	require.Empty(t, r.DelegationRevocation().Reason)
	require.Empty(t, r.DelegationRevocation().AttributedTo)
}

func TestRevocationJSON(t *testing.T) {
	_, err := delegation.Revocation{}.MarshalJSON()
	require.ErrorIs(t, err, delegation.ErrInvalidRevocation)

	type body struct {
		Revocation delegation.Revocation `json:"revocation"`
	}
	b, err := json.Marshal(body{Revocation: goldenRevocation(t)})
	require.NoError(t, err)
	require.Equal(t, `{"revocation":`+goldenRevocationJCS+`}`, string(b))

	var got body
	require.NoError(t, json.Unmarshal(b, &got))
	require.NoError(t, got.Revocation.Verify(human(t).PublicKey()))

	var r delegation.Revocation
	require.ErrorIs(t, r.UnmarshalJSON([]byte("null")), delegation.ErrInvalidRevocation)
}
