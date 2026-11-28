package delegation_test

import (
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/canonical"
	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
)

// The RFC 8032 §7.1 seeds: TEST 1 is the human, TEST 2 the agent.
const (
	humanSeed = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	agentSeed = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

// goldenJCS is the certificate the human issues to the agent with the
// values of the illustrative certificate in
// docs/remote/auth.md#agentdelegation. It was computed independently of
// this package and of canonical: the JSON written out by hand in JCS order,
// hashed with BLAKE3 and signed with crypto/ed25519.
const goldenJCS = `{"@context":"https://atomic.dev/ns/ctx.jsonld",` +
	`"@id":"urn:atomic:delegation:7XFVC6H4XTSGCGH7NTOIWXLWVFZPTCO5LCK4R5C7JA2VBT47WNBQ",` +
	`"@type":"AgentDelegation",` +
	`"attributedTo":"did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA",` +
	`"contentHash":"blake3:accc4480d5f86bd465a3b6d84f119c3a8061b911110719f012d5b18cd2a1b1b3",` +
	`"delegate":"did:atomic:CAT6ANNSNNQF3RWUW6GQPXBJMYH4YNEYWWMKFZL4JZVRWZZ2D2KQ",` +
	`"delegateKey":"did:key:z6MkiaMbhXHNA4eJVCCj8dbzKzTgYDKf6crKgHVHid1F1WCT",` +
	`"delegateName":"alice+claude",` +
	`"delegator":"did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA",` +
	`"delegatorKey":"did:key:z6MktwupdmLXVVqTzCw4i46r4uGyosGXRnR3XjN4Zq7oMMsw",` +
	`"delegatorName":"alice",` +
	`"expires":"2026-12-24T09:30:00Z",` +
	`"issued":"2026-11-24T09:30:00Z",` +
	`"proof":{"@type":"DataIntegrityProof","cryptosuite":"eddsa-jcs-2022","proofPurpose":"assertionMethod",` +
	`"proofValue":"z4cm9Ncv4BjaNCCeXPPrn8dgZLmhzSK9GyTeFkq9ZWtxM7u5agzoeSfJH6y69CPZEiJqMi69r6hdkT3gasL7zZYGr",` +
	`"verificationMethod":"did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA#key-1"},` +
	`"scope":{"permissions":["read","record","push"],"projects":["acme/*"],"servers":["https://atomic.storage"],"views":[],"workspaces":[]},` +
	`"softwareAgent":"urn:atomic:agent:claude-code"}`

// goldenHeader is the unpadded base64url of goldenJCS, computed with it.
const goldenHeader = "eyJAY29udGV4dCI6Imh0dHBzOi8vYXRvbWljLmRldi9ucy9jdHguanNvbmxkIiwiQGlkIjoidXJuOmF0b21pYzpkZWxlZ2F0aW9uOjdYRlZDNkg0WFRTR0NHSDdOVE9JV1hMV1ZGWlBUQ081TENLNFI1QzdKQTJWQlQ0N1dOQlEiLCJAdHlwZSI6IkFnZW50RGVsZWdhdGlvbiIsImF0dHJpYnV0ZWRUbyI6ImRpZDphdG9taWM6TlFZUUlFVEk2UllXQkhEWjZYWk5YVEJZNFNTS1dMMk5JRlFRVEpIQVQ3SFZCN0lQQUJSQSIsImNvbnRlbnRIYXNoIjoiYmxha2UzOmFjY2M0NDgwZDVmODZiZDQ2NWEzYjZkODRmMTE5YzNhODA2MWI5MTExMTA3MTlmMDEyZDViMThjZDJhMWIxYjMiLCJkZWxlZ2F0ZSI6ImRpZDphdG9taWM6Q0FUNkFOTlNOTlFGM1JXVVc2R1FQWEJKTVlINFlORVlXV01LRlpMNEpaVlJXWloyRDJLUSIsImRlbGVnYXRlS2V5IjoiZGlkOmtleTp6Nk1raWFNYmhYSE5BNGVKVkNDajhkYnpLelRnWURLZjZjcktnSFZIaWQxRjFXQ1QiLCJkZWxlZ2F0ZU5hbWUiOiJhbGljZStjbGF1ZGUiLCJkZWxlZ2F0b3IiOiJkaWQ6YXRvbWljOk5RWVFJRVRJNlJZV0JIRFo2WFpOWFRCWTRTU0tXTDJOSUZRUVRKSEFUN0hWQjdJUEFCUkEiLCJkZWxlZ2F0b3JLZXkiOiJkaWQ6a2V5Ono2TWt0d3VwZG1MWFZWcVR6Q3c0aTQ2cjR1R3lvc0dYUm5SM1hqTjRacTdvTU1zdyIsImRlbGVnYXRvck5hbWUiOiJhbGljZSIsImV4cGlyZXMiOiIyMDI2LTEyLTI0VDA5OjMwOjAwWiIsImlzc3VlZCI6IjIwMjYtMTEtMjRUMDk6MzA6MDBaIiwicHJvb2YiOnsiQHR5cGUiOiJEYXRhSW50ZWdyaXR5UHJvb2YiLCJjcnlwdG9zdWl0ZSI6ImVkZHNhLWpjcy0yMDIyIiwicHJvb2ZQdXJwb3NlIjoiYXNzZXJ0aW9uTWV0aG9kIiwicHJvb2ZWYWx1ZSI6Ino0Y205TmN2NEJqYU5DQ2VYUFBybjhkZ1pMbWh6U0s5R3lUZUZrcTlaV3R4TTd1NWFnem9lU2ZKSDZ5NjlDUFpFaUpxTWk2OXI2aGRrVDNnYXNMN3paWUdyIiwidmVyaWZpY2F0aW9uTWV0aG9kIjoiZGlkOmF0b21pYzpOUVlRSUVUSTZSWVdCSERaNlhaTlhUQlk0U1NLV0wyTklGUVFUSkhBVDdIVkI3SVBBQlJBI2tleS0xIn0sInNjb3BlIjp7InBlcm1pc3Npb25zIjpbInJlYWQiLCJyZWNvcmQiLCJwdXNoIl0sInByb2plY3RzIjpbImFjbWUvKiJdLCJzZXJ2ZXJzIjpbImh0dHBzOi8vYXRvbWljLnN0b3JhZ2UiXSwidmlld3MiOltdLCJ3b3Jrc3BhY2VzIjpbXX0sInNvZnR3YXJlQWdlbnQiOiJ1cm46YXRvbWljOmFnZW50OmNsYXVkZS1jb2RlIn0"

func seedSigner(t *testing.T, seed string) identity.Signer {
	t.Helper()
	s, err := identity.SignerFromSeed(mustHex(t, seed))
	require.NoError(t, err)
	return s
}

func human(t *testing.T) identity.Signer { return seedSigner(t, humanSeed) }
func agent(t *testing.T) identity.Signer { return seedSigner(t, agentSeed) }

func goldenExpires() time.Time {
	return time.Date(2026, time.December, 24, 9, 30, 0, 0, time.UTC)
}

// goldenGrant is the grant of the illustrative certificate.
func goldenGrant(t *testing.T) delegation.Grant {
	t.Helper()
	scope := delegation.NewScope(delegation.PermissionRead, delegation.PermissionRecord, delegation.PermissionPush)
	scope.Projects = []string{"acme/*"}
	scope.Servers = []string{"https://atomic.storage"}
	return delegation.Grant{
		DelegatorName: "alice",
		DelegateKey:   agent(t).PublicKey(),
		DelegateName:  "alice+claude",
		Issued:        goldenIssued(),
		Expires:       goldenExpires(),
		Scope:         scope,
		SoftwareAgent: "urn:atomic:agent:claude-code",
	}
}

func goldenCertificate(t *testing.T) delegation.Certificate {
	t.Helper()
	c, err := delegation.ParseCertificate([]byte(goldenJCS))
	require.NoError(t, err)
	return c
}

// attest signs d as the human and reads it back as a certificate.
func attest(t *testing.T, d canonical.Document) delegation.Certificate {
	t.Helper()
	signed, err := canonical.Attest(context.Background(), d, human(t))
	require.NoError(t, err)
	c, err := delegation.CertificateFromDocument(signed)
	require.NoError(t, err)
	return c
}

// with returns the golden document with member name set to v.
func with(t *testing.T, name string, v any) canonical.Document {
	t.Helper()
	d, err := goldenCertificate(t).Document().With(name, v)
	require.NoError(t, err)
	return d
}

func TestCertificateConstants(t *testing.T) {
	require.Equal(t, "Atomic-Delegation", delegation.HeaderName)
	require.Equal(t, 16384, delegation.MaxHeaderLen)
	require.Equal(t, "AgentDelegation", delegation.TypeAgentDelegation)
	require.Equal(t, "urn:atomic:agent:", delegation.SoftwareAgentPrefix)
}

func TestIssueGolden(t *testing.T) {
	c, err := delegation.Issue(context.Background(), human(t), goldenGrant(t))
	require.NoError(t, err)

	b, err := c.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, goldenJCS, string(b))
	require.Equal(t, goldenHeader, c.HeaderValue())
	require.Equal(t, delegation.URNPrefix+goldenID, c.ID().URN())

	require.NoError(t, c.Verify(human(t).PublicKey()))
	require.NoError(t, c.VerifySelfContained())
}

func TestAgentDelegationFields(t *testing.T) {
	a := goldenCertificate(t).AgentDelegation()
	h, g := human(t).PublicKey(), agent(t).PublicKey()

	require.Equal(t, canonical.Context, a.Context)
	require.Equal(t, delegation.URNPrefix+goldenID, a.ID.URN())
	require.Equal(t, delegation.TypeAgentDelegation, a.Type)
	require.Equal(t, h.AtomicDID().String(), a.AttributedTo)
	require.Equal(t, "blake3:accc4480d5f86bd465a3b6d84f119c3a8061b911110719f012d5b18cd2a1b1b3", a.ContentHash)
	require.Equal(t, g.AtomicDID(), a.Delegate)
	require.Equal(t, g.KeyDID(), a.DelegateKey)
	require.Equal(t, "alice+claude", a.DelegateName)
	require.Equal(t, h.AtomicDID(), a.Delegator)
	require.Equal(t, h.KeyDID(), a.DelegatorKey)
	require.Equal(t, "alice", a.DelegatorName)
	require.Equal(t, goldenExpires(), a.Expires)
	require.Equal(t, goldenIssued(), a.Issued)
	require.Equal(t, canonical.ProofType, a.Proof.Type)
	require.Equal(t, h.AtomicDID().VerificationMethod(), a.Proof.VerificationMethod)
	require.Equal(t, []delegation.Permission{"read", "record", "push"}, a.Scope.Permissions)
	require.Equal(t, []string{"acme/*"}, a.Scope.Projects)
	require.Equal(t, []string{"https://atomic.storage"}, a.Scope.Servers)
	require.Equal(t, "urn:atomic:agent:claude-code", a.SoftwareAgent)
}

func TestAgentDelegationIsACopy(t *testing.T) {
	c := goldenCertificate(t)
	a := c.AgentDelegation()
	a.Scope.Permissions[0] = delegation.PermissionFull
	a.Scope.Servers[0] = "https://elsewhere.test"
	require.Equal(t, delegation.PermissionRead, c.AgentDelegation().Scope.Permissions[0])
	require.False(t, c.Allows(delegation.PermissionAdmin, delegation.ResourceRef{}, goldenIssued()))
}

func TestIssueWholeSecondsAndNoExpiry(t *testing.T) {
	g := goldenGrant(t)
	g.Issued = goldenIssued().Add(987654321 * time.Nanosecond).In(time.FixedZone("x", 3600))
	g.Expires = time.Time{}
	g.SoftwareAgent = ""
	c, err := delegation.Issue(context.Background(), human(t), g)
	require.NoError(t, err)

	issued, ok := c.Document().Text("issued")
	require.True(t, ok)
	require.Equal(t, "2026-11-24T09:30:00Z", issued)
	require.False(t, c.Document().Has("expires"))
	require.False(t, c.Document().Has("softwareAgent"))
	require.True(t, c.AgentDelegation().Expires.IsZero())
	require.Equal(t, delegation.URNPrefix+goldenID, c.ID().URN())
	require.False(t, c.Expired(time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, c.Verify(human(t).PublicKey()))
}

func TestIssueRefuses(t *testing.T) {
	ctx := context.Background()
	tests := map[string]struct {
		signer identity.Signer
		edit   func(*delegation.Grant)
	}{
		"nil signer":    {signer: nil, edit: func(*delegation.Grant) {}},
		"zero key":      {edit: func(g *delegation.Grant) { g.DelegateKey = identity.PublicKey{} }},
		"zero issued":   {edit: func(g *delegation.Grant) { g.Issued = time.Time{} }},
		"expires early": {edit: func(g *delegation.Grant) { g.Expires = g.Issued.Add(-time.Second) }},
		"expires at issued after truncation": {edit: func(g *delegation.Grant) {
			g.Expires = g.Issued.Add(500 * time.Millisecond)
		}},
		"software agent prefix": {edit: func(g *delegation.Grant) { g.SoftwareAgent = "claude-code" }},
		"unknown permission": {edit: func(g *delegation.Grant) {
			g.Scope.Permissions = []delegation.Permission{"write"}
		}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := tc.signer
			if name != "nil signer" {
				s = human(t)
			}
			g := goldenGrant(t)
			tc.edit(&g)
			_, err := delegation.Issue(ctx, s, g)
			require.ErrorIs(t, err, delegation.ErrInvalid)
		})
	}
}

// liar claims one key and signs with another.
type liar struct {
	claimed identity.PublicKey
	signer  identity.Signer
}

func (l liar) PublicKey() identity.PublicKey { return l.claimed }

func (l liar) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	return l.signer.Sign(ctx, msg)
}

func TestIssueChecksTheSigner(t *testing.T) {
	s := liar{claimed: human(t).PublicKey(), signer: agent(t)}
	_, err := delegation.Issue(context.Background(), s, goldenGrant(t))
	require.ErrorIs(t, err, delegation.ErrVerify)
}

func TestIssueSignerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := delegation.Issue(ctx, human(t), goldenGrant(t))
	require.ErrorIs(t, err, context.Canceled)
}

func TestParseGolden(t *testing.T) {
	c := goldenCertificate(t)
	require.NoError(t, c.Verify(human(t).PublicKey()))
	require.Equal(t, goldenHeader, c.HeaderValue())

	d, err := delegation.DecodeHeader(goldenHeader)
	require.NoError(t, err)
	require.NoError(t, d.Verify(human(t).PublicKey()))
	b, err := d.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, goldenJCS, string(b))
}

func TestParseCertificateRefuses(t *testing.T) {
	required := []string{
		"@id", "@type", "contentHash", "delegate", "delegateKey", "delegateName",
		"delegator", "delegatorKey", "delegatorName", "issued", "proof", "scope",
	}
	for _, name := range required {
		t.Run("missing "+name, func(t *testing.T) {
			_, err := delegation.CertificateFromDocument(goldenCertificate(t).Document().Without(name))
			require.ErrorIs(t, err, delegation.ErrInvalid)
			require.ErrorContains(t, err, name)
		})
	}

	bad := map[string]struct {
		name  string
		value any
	}{
		"@id not a string":          {"@id", 7},
		"@id not an id":             {"@id", "urn:atomic:delegation:nope"},
		"@type null":                {"@type", nil},
		"delegate not a DID":        {"delegate", "did:web:example.com"},
		"delegateKey not a DID":     {"delegateKey", "did:key:zzz"},
		"issued not a time":         {"issued", "yesterday"},
		"expires not a time":        {"expires", "2026-12-24"},
		"expires empty":             {"expires", ""},
		"expires a number":          {"expires", 1798104600},
		"attributedTo a number":     {"attributedTo", 1},
		"softwareAgent an object":   {"softwareAgent", map[string]string{}},
		"proof with unknown member": {"proof", map[string]string{"extra": "x"}},
		"scope without permissions": {"scope", map[string][]string{"servers": {}}},
	}
	for label, tc := range bad {
		t.Run(label, func(t *testing.T) {
			_, err := delegation.CertificateFromDocument(with(t, tc.name, tc.value))
			require.ErrorIs(t, err, delegation.ErrInvalid)
		})
	}

	t.Run("not JSON", func(t *testing.T) {
		_, err := delegation.ParseCertificate([]byte(`{"@type":`))
		require.ErrorIs(t, err, delegation.ErrInvalid)
		require.ErrorIs(t, err, canonical.ErrNotAdmitted)
	})
	t.Run("duplicate member", func(t *testing.T) {
		_, err := delegation.ParseCertificate([]byte(`{"@type":"AgentDelegation","@type":"AgentDelegation"}`))
		require.ErrorIs(t, err, canonical.ErrNotAdmitted)
	})
	t.Run("array", func(t *testing.T) {
		_, err := delegation.ParseCertificate([]byte(`[]`))
		require.ErrorIs(t, err, delegation.ErrInvalid)
	})
}

func TestParseOptionalMembersMayBeNull(t *testing.T) {
	c, err := delegation.CertificateFromDocument(with(t, "attributedTo", nil))
	require.NoError(t, err)
	require.Equal(t, "", c.AgentDelegation().AttributedTo)

	d := with(t, "softwareAgent", nil)
	d, err = d.With("@context", nil)
	require.NoError(t, err)
	c = attest(t, d)
	a := c.AgentDelegation()
	require.Equal(t, "", a.SoftwareAgent)
	require.Equal(t, "", a.Context)
	require.NoError(t, c.Verify(human(t).PublicKey()))
}

// TestVerifySteps fails each of the nine steps of
// docs/remote/auth.md#verifying-a-certificate in turn, with every step
// before it passing.
func TestVerifySteps(t *testing.T) {
	h, g := human(t).PublicKey(), agent(t).PublicKey()

	tests := map[string]struct {
		cert func(t *testing.T) delegation.Certificate
		is   error
		msg  string
	}{
		"1 @type": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "@type", "AgentDelegationRequest"))
			},
			msg: "@type",
		},
		"2 contentHash": {
			cert: func(t *testing.T) delegation.Certificate {
				c, err := delegation.CertificateFromDocument(with(t, "delegateName", "alice+mallory"))
				require.NoError(t, err)
				return c
			},
			is: canonical.ErrContentHash,
		},
		"3 proof metadata": {
			cert: func(t *testing.T) delegation.Certificate {
				p := goldenCertificate(t).AgentDelegation().Proof
				p.Cryptosuite = "eddsa-rdfc-2022"
				c, err := delegation.CertificateFromDocument(with(t, "proof", p))
				require.NoError(t, err)
				return c
			},
			is:  canonical.ErrProof,
			msg: "metadata",
		},
		"4 signature": {
			cert: func(t *testing.T) delegation.Certificate {
				d := with(t, "delegateName", "alice+mallory")
				hash, err := canonical.ContentHash(d)
				require.NoError(t, err)
				d, err = d.With("contentHash", hash)
				require.NoError(t, err)
				c, err := delegation.CertificateFromDocument(d)
				require.NoError(t, err)
				return c
			},
			is:  canonical.ErrProof,
			msg: "signature",
		},
		"5 verificationMethod": {
			cert: func(t *testing.T) delegation.Certificate {
				p := goldenCertificate(t).AgentDelegation().Proof
				p.VerificationMethod = g.AtomicDID().VerificationMethod()
				c, err := delegation.CertificateFromDocument(with(t, "proof", p))
				require.NoError(t, err)
				return c
			},
			is:  canonical.ErrProof,
			msg: "verificationMethod",
		},
		"7 delegator": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "delegator", g.AtomicDID().String()))
			},
			msg: "delegator did:atomic:",
		},
		"7 delegator as did:key": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "delegator", h.KeyDID().String()))
			},
			msg: "delegator did:key:",
		},
		"7 delegatorKey": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "delegatorKey", g.KeyDID().String()))
			},
			msg: "delegatorKey",
		},
		"8 delegate": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "delegate", h.AtomicDID().String()))
			},
			msg: "delegate did:atomic:",
		},
		"8 delegateKey not a did:key": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "delegateKey", g.AtomicDID().String()))
			},
			msg: "not a did:key",
		},
		"9 @id from another issue time": {
			cert: func(t *testing.T) delegation.Certificate {
				id := delegation.NewID(h.ID(), g.ID(), goldenIssued().Add(time.Second))
				return attest(t, with(t, "@id", id.URN()))
			},
			msg: "@id",
		},
		"9 @id without its URN prefix": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "@id", goldenID))
			},
			msg: "@id",
		},
		"9 issued moved": {
			cert: func(t *testing.T) delegation.Certificate {
				return attest(t, with(t, "issued", "2026-11-24T09:30:01Z"))
			},
			msg: "@id",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.cert(t).Verify(h)
			require.ErrorIs(t, err, delegation.ErrVerify)
			if tc.is != nil {
				require.ErrorIs(t, err, tc.is)
			}
			if tc.msg != "" {
				require.ErrorContains(t, err, tc.msg)
			}
		})
	}

	t.Run("6 a missing Required member", func(t *testing.T) {
		signed, err := canonical.Attest(context.Background(), goldenCertificate(t).Document().Without("delegateName"), human(t))
		require.NoError(t, err)
		// Steps 1 to 5 pass: the document is intact and signed by the
		// human.
		require.NoError(t, canonical.VerifyAttested(signed, h))
		_, err = delegation.CertificateFromDocument(signed)
		require.ErrorIs(t, err, delegation.ErrInvalid)
		require.ErrorContains(t, err, "delegateName")
	})

	t.Run("wrong key", func(t *testing.T) {
		err := goldenCertificate(t).Verify(g)
		require.ErrorIs(t, err, delegation.ErrVerify)
		require.ErrorIs(t, err, canonical.ErrProof)
	})

	t.Run("zero certificate", func(t *testing.T) {
		require.ErrorIs(t, delegation.Certificate{}.Verify(h), delegation.ErrVerify)
		require.ErrorIs(t, delegation.Certificate{}.VerifySelfContained(), delegation.ErrVerify)
	})
}

func TestVerifySelfContained(t *testing.T) {
	require.NoError(t, goldenCertificate(t).VerifySelfContained())

	// Signed by the agent and naming the agent as delegator: intact, so
	// self-contained verification passes, but not the human's.
	g := agent(t).PublicKey()
	d := with(t, "delegator", g.AtomicDID().String())
	d, err := d.With("delegatorKey", g.KeyDID().String())
	require.NoError(t, err)
	d, err = d.With("@id", delegation.NewID(g.ID(), g.ID(), goldenIssued()).URN())
	require.NoError(t, err)
	signed, err := canonical.Attest(context.Background(), d.Without("attributedTo"), agent(t))
	require.NoError(t, err)
	c, err := delegation.CertificateFromDocument(signed)
	require.NoError(t, err)
	require.NoError(t, c.VerifySelfContained())
	require.ErrorIs(t, c.Verify(human(t).PublicKey()), delegation.ErrVerify)

	t.Run("delegatorKey not a did:key", func(t *testing.T) {
		c := attest(t, with(t, "delegatorKey", human(t).PublicKey().AtomicDID().String()))
		err := c.VerifySelfContained()
		require.ErrorIs(t, err, delegation.ErrVerify)
		require.ErrorContains(t, err, "not a did:key")
	})
}

func TestTamperedCertificateFailsVerify(t *testing.T) {
	tampered := strings.Replace(goldenJCS, `"push"]`, `"full"]`, 1)
	require.NotEqual(t, goldenJCS, tampered)
	header := base64.RawURLEncoding.EncodeToString([]byte(tampered))

	c, err := delegation.DecodeHeader(header)
	require.NoError(t, err, "decoding is not trusting")
	require.ErrorIs(t, c.Verify(human(t).PublicKey()), delegation.ErrVerify)
	require.ErrorIs(t, c.VerifySelfContained(), delegation.ErrVerify)
}

func TestUnknownMemberSurvives(t *testing.T) {
	c := attest(t, with(t, "futureMember", map[string]any{"z": []int{1, 2}, "a": "b"}))

	b, err := c.MarshalJSON()
	require.NoError(t, err)
	parsed, err := delegation.ParseCertificate(b)
	require.NoError(t, err)
	decoded, err := delegation.DecodeHeader(parsed.HeaderValue())
	require.NoError(t, err)

	v, ok := decoded.Document().Member("futureMember")
	require.True(t, ok)
	require.Equal(t, `{"a":"b","z":[1,2]}`, string(v))
	require.NoError(t, decoded.Verify(human(t).PublicKey()))
	require.Equal(t, c.HeaderValue(), decoded.HeaderValue())
}

// headerOfLen issues a certificate whose header value is exactly n bytes,
// n a multiple of 4, by sizing the scope's description.
func headerOfLen(t *testing.T, n int) string {
	t.Helper()
	g := goldenGrant(t)
	g.Scope.Description = "x"
	c, err := delegation.Issue(context.Background(), human(t), g)
	require.NoError(t, err)
	b, err := c.MarshalJSON()
	require.NoError(t, err)
	g.Scope.Description = strings.Repeat("x", 1+n/4*3-len(b))
	c, err = delegation.Issue(context.Background(), human(t), g)
	require.NoError(t, err)
	h := c.HeaderValue()
	require.Len(t, h, n)
	return h
}

func TestDecodeHeaderLength(t *testing.T) {
	h := headerOfLen(t, delegation.MaxHeaderLen)

	c, err := delegation.DecodeHeader(h)
	require.NoError(t, err)
	require.NoError(t, c.Verify(human(t).PublicKey()))

	// One more byte is refused before anything else, even though it is
	// white space that trimming would have removed.
	_, err = delegation.DecodeHeader(h + " ")
	require.ErrorIs(t, err, delegation.ErrHeaderTooLarge)
	require.NotErrorIs(t, err, delegation.ErrInvalid)

	_, err = delegation.DecodeHeader(strings.Repeat("!", delegation.MaxHeaderLen+1))
	require.ErrorIs(t, err, delegation.ErrHeaderTooLarge)

	// Under the cap, the same garbage is a decoding failure.
	_, err = delegation.DecodeHeader(strings.Repeat("!", delegation.MaxHeaderLen))
	require.ErrorIs(t, err, delegation.ErrInvalid)
}

func TestDecodeHeaderWhitespace(t *testing.T) {
	for name, v := range map[string]string{
		"leading space and trailing newline": " " + goldenHeader + "\n",
		"tabs and CRLF":                      "\t" + goldenHeader + "\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			c, err := delegation.DecodeHeader(v)
			require.NoError(t, err)
			require.NoError(t, c.Verify(human(t).PublicKey()))
		})
	}

	for name, v := range map[string]string{
		"interior newline":  goldenHeader[:100] + "\n" + goldenHeader[100:],
		"interior space":    goldenHeader[:100] + " " + goldenHeader[100:],
		"padding":           base64.URLEncoding.EncodeToString([]byte(goldenJCS)),
		"standard alphabet": goldenHeader[:10] + "+" + goldenHeader[11:],
		"empty":             "",
		"only white space":  " \n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := delegation.DecodeHeader(v)
			require.ErrorIs(t, err, delegation.ErrInvalid)
		})
	}
}

func TestFingerprint(t *testing.T) {
	sum := ident.Sum([]byte(goldenHeader))
	require.Equal(t, ident.EncodeBase32(sum[:]), delegation.Fingerprint(goldenHeader))
	require.Len(t, delegation.Fingerprint(goldenHeader), ident.Base32Len)
	require.NotEqual(t, delegation.Fingerprint(goldenHeader), delegation.Fingerprint(goldenHeader+"\n"))
}

func TestExpiresNull(t *testing.T) {
	c := attest(t, with(t, "expires", jsontext.Value("null")))
	v, ok := c.Document().Member("expires")
	require.True(t, ok)
	require.Equal(t, "null", string(v))

	parsed, err := delegation.DecodeHeader(c.HeaderValue())
	require.NoError(t, err)
	require.NoError(t, parsed.Verify(human(t).PublicKey()))
	require.True(t, parsed.AgentDelegation().Expires.IsZero())
	require.False(t, parsed.Expired(time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)))
}

func TestExpiry(t *testing.T) {
	c := goldenCertificate(t)
	exp := goldenExpires()

	require.False(t, c.Expired(goldenIssued()))
	require.False(t, c.Expired(exp.Add(-time.Second)))
	require.False(t, c.Expired(exp), "still valid at exactly expires")
	require.True(t, c.Expired(exp.Add(time.Nanosecond)))
	require.True(t, c.Expired(exp.Add(time.Hour)))

	noExpiry := attest(t, goldenCertificate(t).Document().Without("expires"))
	require.False(t, noExpiry.Expired(time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC)))
}

func TestCertificateAllows(t *testing.T) {
	c := goldenCertificate(t)
	r := delegation.ResourceRef{Server: "https://Atomic.Storage/", Project: "acme/api"}

	require.True(t, c.Allows(delegation.PermissionPush, r, goldenIssued()))
	require.True(t, c.Allows(delegation.PermissionPush, r, goldenExpires()))
	require.False(t, c.Allows(delegation.PermissionPush, r, goldenExpires().Add(time.Second)), "expired")
	require.False(t, c.Allows(delegation.PermissionAdmin, r, goldenIssued()), "not granted")
	require.False(t, c.Allows(delegation.PermissionRead, delegation.ResourceRef{Project: "other/api"}, goldenIssued()), "out of scope")
	require.False(t, c.Allows(delegation.PermissionRead, delegation.ResourceRef{Server: "https://elsewhere.test"}, goldenIssued()), "other server")
}

func TestCertificateJSON(t *testing.T) {
	type envelope struct {
		Certificate delegation.Certificate `json:"certificate"`
	}
	b, err := json.Marshal(envelope{Certificate: goldenCertificate(t)})
	require.NoError(t, err)
	require.Equal(t, `{"certificate":`+goldenJCS+`}`, string(b))

	var e envelope
	require.NoError(t, json.Unmarshal(b, &e))
	require.Equal(t, goldenHeader, e.Certificate.HeaderValue())
	require.NoError(t, e.Certificate.Verify(human(t).PublicKey()))

	var c delegation.Certificate
	require.ErrorIs(t, c.UnmarshalJSON([]byte(`null`)), delegation.ErrInvalid)
	require.ErrorIs(t, c.UnmarshalJSON([]byte(`{}`)), delegation.ErrInvalid)
	require.Equal(t, "", c.HeaderValue(), "unchanged on failure")

	_, err = delegation.Certificate{}.MarshalJSON()
	require.ErrorIs(t, err, delegation.ErrInvalid)
}
