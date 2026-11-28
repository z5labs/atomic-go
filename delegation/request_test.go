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

func goldenRequested() time.Time {
	return time.Date(2026, time.November, 24, 9, 0, 0, 0, time.UTC)
}

// newRequest is the agent's request to be delegated to as alice+claude.
func newRequest(t *testing.T) delegation.Request {
	t.Helper()
	r, err := delegation.NewRequest(context.Background(), agent(t), "alice+claude", goldenRequested(), "urn:atomic:agent:claude-code")
	require.NoError(t, err)
	return r
}

// requestWith returns the agent's request document with member name set to
// v.
func requestWith(t *testing.T, name string, v any) canonical.Document {
	t.Helper()
	d, err := newRequest(t).Document().With(name, v)
	require.NoError(t, err)
	return d
}

// attestRequest signs d as s and reads it back as a request.
func attestRequest(t *testing.T, d canonical.Document, s identity.Signer) delegation.Request {
	t.Helper()
	signed, err := canonical.Attest(context.Background(), d, s)
	require.NoError(t, err)
	r, err := delegation.RequestFromDocument(signed)
	require.NoError(t, err)
	return r
}

func TestNewRequest(t *testing.T) {
	at := goldenRequested().Add(250 * time.Millisecond).In(time.FixedZone("PST", -8*3600))
	r, err := delegation.NewRequest(context.Background(), agent(t), "alice+claude", at, "urn:atomic:agent:claude-code")
	require.NoError(t, err)
	require.NoError(t, r.Verify())

	g := agent(t).PublicKey()
	v := r.AgentDelegationRequest()
	require.Equal(t, canonical.Context, v.Context)
	require.Equal(t, delegation.TypeRequest, v.Type)
	require.Equal(t, g.AtomicDID().String(), v.AttributedTo)
	require.Equal(t, g.AtomicDID(), v.Delegate)
	require.Equal(t, g.KeyDID(), v.DelegateKey)
	require.Equal(t, "alice+claude", v.DelegateName)
	require.Equal(t, g.AtomicDID().VerificationMethod(), v.Proof.VerificationMethod)
	require.Equal(t, goldenRequested(), v.Requested)
	require.Equal(t, "urn:atomic:agent:claude-code", v.SoftwareAgent)

	b, err := r.MarshalJSON()
	require.NoError(t, err)
	require.Contains(t, string(b), `"requested":"2026-11-24T09:00:00Z"`)
}

func TestRequestRoundTrip(t *testing.T) {
	r := newRequest(t)
	b, err := r.MarshalJSON()
	require.NoError(t, err)

	back, err := delegation.ParseRequest(b)
	require.NoError(t, err)
	require.NoError(t, back.Verify())
	require.Equal(t, r.AgentDelegationRequest(), back.AgentDelegationRequest())

	again, err := back.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, b, again)
}

func TestNewRequestWithoutSoftwareAgent(t *testing.T) {
	r, err := delegation.NewRequest(context.Background(), agent(t), "ci-runner", goldenRequested(), "")
	require.NoError(t, err)
	require.NoError(t, r.Verify())
	require.False(t, r.Document().Has("softwareAgent"))
	require.Empty(t, r.AgentDelegationRequest().SoftwareAgent)
}

func TestEditedRequestFailsVerify(t *testing.T) {
	r, err := delegation.RequestFromDocument(requestWith(t, "delegateName", "alice+mallory"))
	require.NoError(t, err)
	err = r.Verify()
	require.ErrorIs(t, err, delegation.ErrVerifyRequest)
	require.ErrorIs(t, err, canonical.ErrContentHash)

	// Recomputing the content hash after the edit does not help: the
	// signature covers it.
	d := requestWith(t, "delegateName", "alice+mallory")
	hash, err := canonical.ContentHash(d)
	require.NoError(t, err)
	d, err = d.With("contentHash", hash)
	require.NoError(t, err)
	r, err = delegation.RequestFromDocument(d)
	require.NoError(t, err)
	err = r.Verify()
	require.ErrorIs(t, err, delegation.ErrVerifyRequest)
	require.ErrorIs(t, err, canonical.ErrProof)
}

func TestRequestVerifySteps(t *testing.T) {
	h, g := human(t).PublicKey(), agent(t).PublicKey()

	tests := map[string]struct {
		req func(t *testing.T) delegation.Request
		is  error
		msg string
	}{
		"1 @type": {
			req: func(t *testing.T) delegation.Request {
				return attestRequest(t, requestWith(t, "@type", delegation.TypeAgentDelegation), agent(t))
			},
			msg: "@type",
		},
		"2 delegateKey is a did:atomic": {
			req: func(t *testing.T) delegation.Request {
				return attestRequest(t, requestWith(t, "delegateKey", g.AtomicDID().String()), agent(t))
			},
			msg: "delegateKey",
		},
		"3 signed by another key": {
			req: func(t *testing.T) delegation.Request {
				return attestRequest(t, requestWith(t, "attributedTo", jsontext.Value("null")), human(t))
			},
			is: canonical.ErrProof,
		},
		"3 delegateKey names another key": {
			req: func(t *testing.T) delegation.Request {
				return attestRequest(t, requestWith(t, "delegateKey", h.KeyDID().String()), agent(t))
			},
			is: canonical.ErrProof,
		},
		"4 delegate names another key": {
			req: func(t *testing.T) delegation.Request {
				return attestRequest(t, requestWith(t, "delegate", h.AtomicDID().String()), agent(t))
			},
			msg: "delegate ",
		},
		"4 delegate as did:key": {
			req: func(t *testing.T) delegation.Request {
				return attestRequest(t, requestWith(t, "delegate", g.KeyDID().String()), agent(t))
			},
			msg: "delegate ",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := tc.req(t).Verify()
			require.ErrorIs(t, err, delegation.ErrVerifyRequest)
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

func TestZeroRequestDoesNotVerify(t *testing.T) {
	require.ErrorIs(t, delegation.Request{}.Verify(), delegation.ErrVerifyRequest)
}

func TestNewRequestRefuses(t *testing.T) {
	ctx := context.Background()

	_, err := delegation.NewRequest(ctx, nil, "alice+claude", goldenRequested(), "")
	require.ErrorIs(t, err, delegation.ErrInvalidRequest)

	_, err = delegation.NewRequest(ctx, agent(t), "alice+claude", time.Time{}, "")
	require.ErrorIs(t, err, delegation.ErrInvalidRequest)

	_, err = delegation.NewRequest(ctx, agent(t), "alice+claude", goldenRequested(), "claude-code")
	require.ErrorIs(t, err, delegation.ErrInvalidRequest)
}

func TestNewRequestChecksTheSigner(t *testing.T) {
	s := liar{claimed: agent(t).PublicKey(), signer: human(t)}
	_, err := delegation.NewRequest(context.Background(), s, "alice+claude", goldenRequested(), "")
	require.ErrorIs(t, err, delegation.ErrVerifyRequest)
}

func TestNewRequestSignerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := delegation.NewRequest(ctx, agent(t), "alice+claude", goldenRequested(), "")
	require.ErrorIs(t, err, context.Canceled)
}

func TestParseRequestRefuses(t *testing.T) {
	without := func(name string) func(t *testing.T) canonical.Document {
		return func(t *testing.T) canonical.Document {
			return newRequest(t).Document().Without(name)
		}
	}
	set := func(name string, v any) func(t *testing.T) canonical.Document {
		return func(t *testing.T) canonical.Document {
			return requestWith(t, name, v)
		}
	}
	tests := map[string]func(t *testing.T) canonical.Document{
		"no @type":                   without("@type"),
		"no contentHash":             without("contentHash"),
		"no delegate":                without("delegate"),
		"no delegateKey":             without("delegateKey"),
		"no delegateName":            without("delegateName"),
		"no proof":                   without("proof"),
		"no requested":               without("requested"),
		"delegate not a DID":         set("delegate", "alice"),
		"delegateKey not a DID":      set("delegateKey", "did:key:nope"),
		"delegateName not a string":  set("delegateName", 1),
		"requested not a time":       set("requested", "2026-11-24"),
		"softwareAgent not a string": set("softwareAgent", []string{"x"}),
		"proof not a proof":          set("proof", map[string]string{}),
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := delegation.RequestFromDocument(doc(t))
			require.ErrorIs(t, err, delegation.ErrInvalidRequest)
		})
	}

	t.Run("not admitted", func(t *testing.T) {
		_, err := delegation.ParseRequest([]byte(`[]`))
		require.ErrorIs(t, err, delegation.ErrInvalidRequest)
		require.ErrorIs(t, err, canonical.ErrNotAdmitted)
	})
}

func TestRequestJSON(t *testing.T) {
	_, err := delegation.Request{}.MarshalJSON()
	require.ErrorIs(t, err, delegation.ErrInvalidRequest)

	r := newRequest(t)
	b, err := json.Marshal(r)
	require.NoError(t, err)

	var got delegation.Request
	require.NoError(t, json.Unmarshal(b, &got))
	require.NoError(t, got.Verify())
	require.Equal(t, r.AgentDelegationRequest(), got.AgentDelegationRequest())

	require.ErrorIs(t, got.UnmarshalJSON([]byte("null")), delegation.ErrInvalidRequest)
	require.NoError(t, got.Verify(), "a failed UnmarshalJSON leaves the request unchanged")
}

func TestRequestGrantThenIssue(t *testing.T) {
	r := newRequest(t)
	require.NoError(t, r.Verify())

	scope := delegation.NewScope(delegation.PermissionRead, delegation.PermissionRecord, delegation.PermissionPush)
	scope.Projects = []string{"acme/*"}
	scope.Servers = []string{"https://atomic.storage"}
	g := r.Grant("alice", goldenIssued(), goldenExpires(), scope)

	want := goldenGrant(t)
	require.Equal(t, want, g)

	c, err := delegation.Issue(context.Background(), human(t), g)
	require.NoError(t, err)
	require.NoError(t, c.Verify(human(t).PublicKey()))

	req, cert := r.AgentDelegationRequest(), c.AgentDelegation()
	require.Equal(t, req.Delegate, cert.Delegate)
	require.Equal(t, req.DelegateKey, cert.DelegateKey)
	require.Equal(t, req.DelegateName, cert.DelegateName)
	require.Equal(t, req.SoftwareAgent, cert.SoftwareAgent)

	// The request carries the golden certificate's agent, so the
	// countersigned grant is the golden certificate.
	b, err := c.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, goldenJCS, string(b))
}

func TestRequestGrantWithoutDIDKey(t *testing.T) {
	g := agent(t).PublicKey()
	r, err := delegation.RequestFromDocument(requestWith(t, "delegateKey", g.AtomicDID().String()))
	require.NoError(t, err)

	grant := r.Grant("alice", goldenIssued(), time.Time{}, delegation.NewScope(delegation.PermissionRead))
	require.Equal(t, identity.PublicKey{}, grant.DelegateKey)
	_, err = delegation.Issue(context.Background(), human(t), grant)
	require.ErrorIs(t, err, delegation.ErrInvalid)
}
