package storage_test

import (
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/storage"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// agentSeed is the private key seed of RFC 8032 section 7.1 TEST 2, the
// agent's key in the agent and delegation tests. The delegator's is seed1.
const agentSeed = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"

// issuedAt is when the test certificates are issued.
var issuedAt = time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)

// agentID is the agent the agent tests act on.
var agentID = uuid.MustParse("00000000-0000-0000-0000-00000000000a")

func signerFromSeed(t *testing.T, seedHex string) identity.Signer {
	t.Helper()
	seed, err := hex.DecodeString(seedHex)
	require.NoError(t, err)
	s, err := identity.SignerFromSeed(seed)
	require.NoError(t, err)
	return s
}

// issued returns a certificate the seed1 delegator issued, with
// delegation.Issue, to the agentSeed agent.
func issued(t *testing.T) (delegation.Certificate, identity.Signer, identity.Signer) {
	t.Helper()
	human := signerFromSeed(t, seed1)
	agent := signerFromSeed(t, agentSeed)
	cert, err := delegation.Issue(t.Context(), human, delegation.Grant{
		DelegatorName: "alice",
		DelegateKey:   agent.PublicKey(),
		DelegateName:  "agent",
		Issued:        issuedAt,
		Scope:         delegation.NewScope(delegation.PermissionRead),
	})
	require.NoError(t, err)
	return cert, human, agent
}

// enrolledAgent is the reply a CLI test answers enroll_agent with
// (docs/remote/endpoints/storage.md#enroll_agent), with its UUIDs filled
// in. It has no publishedDelegations.
const enrolledAgent = `{"id": "00000000-0000-0000-0000-00000000000a", "name": "agent", "did": "did:atomic:test", "parentIdentityId": "00000000-0000-0000-0000-000000000001", "softwareAgent": null, "status": "active", "createdAt": "2026-10-27T00:00:00Z"}`

func wantEnrolledAgent() storage.AgentIdentityInfo {
	return storage.AgentIdentityInfo{
		ID:               agentID,
		Name:             "agent",
		DID:              "did:atomic:test",
		ParentIdentityID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Status:           "active",
		CreatedAt:        issuedAt,
	}
}

// fullAgent is an AgentIdentityInfo body with every member the docs list
// (docs/remote/endpoints/storage.md#agentidentityinfo).
const fullAgent = `{"id": "00000000-0000-0000-0000-00000000000a", "name": "agent", "did": "did:atomic:test", "parentIdentityId": "00000000-0000-0000-0000-000000000001", "softwareAgent": "urn:atomic:agent:claude-code", "status": "active", "createdAt": "2026-10-27T00:00:00Z", "delegationsValidFrom": "2026-10-28T00:00:00Z", "publishedDelegations": [` + activeDelegation + `]}`

func wantFullAgent() storage.AgentIdentityInfo {
	a := wantEnrolledAgent()
	a.SoftwareAgent = ptr("urn:atomic:agent:claude-code")
	a.DelegationsValidFrom = ptr(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))
	a.PublishedDelegations = []storage.DelegationInfo{wantActiveDelegation()}
	return a
}

func TestAgentIdentityInfoReadsTheDocsBodies(t *testing.T) {
	var enrolled storage.AgentIdentityInfo
	require.NoError(t, json.Unmarshal([]byte(enrolledAgent), &enrolled))
	require.Equal(t, wantEnrolledAgent(), enrolled)
	require.Nil(t, enrolled.PublishedDelegations)

	var nulls storage.AgentIdentityInfo
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSuffix(enrolledAgent, "}")+`, "delegationsValidFrom": null, "publishedDelegations": null}`), &nulls))
	require.Equal(t, wantEnrolledAgent(), nulls)

	var full storage.AgentIdentityInfo
	require.NoError(t, json.Unmarshal([]byte(fullAgent), &full))
	require.Equal(t, wantFullAgent(), full)
}

func TestAgentIdentityInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing did":              `{"id": "00000000-0000-0000-0000-00000000000a", "name": "agent", "parentIdentityId": "00000000-0000-0000-0000-000000000001", "status": "active", "createdAt": "2026-10-27T00:00:00Z"}`,
		"missing parentIdentityId": `{"id": "00000000-0000-0000-0000-00000000000a", "name": "agent", "did": "did:atomic:test", "status": "active", "createdAt": "2026-10-27T00:00:00Z"}`,
		"snake parent_identity_id": `{"id": "00000000-0000-0000-0000-00000000000a", "name": "agent", "did": "did:atomic:test", "parent_identity_id": "00000000-0000-0000-0000-000000000001", "status": "active", "createdAt": "2026-10-27T00:00:00Z"}`,
		"null status":              `{"id": "00000000-0000-0000-0000-00000000000a", "name": "agent", "did": "did:atomic:test", "parentIdentityId": "00000000-0000-0000-0000-000000000001", "status": null, "createdAt": "2026-10-27T00:00:00Z"}`,
		"written twice":            `{"id": "00000000-0000-0000-0000-00000000000a", "id": "00000000-0000-0000-0000-00000000000a", "name": "agent", "did": "did:atomic:test", "parentIdentityId": "00000000-0000-0000-0000-000000000001", "status": "active", "createdAt": "2026-10-27T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.AgentIdentityInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestEpochInfo(t *testing.T) {
	var set storage.EpochInfo
	require.NoError(t, json.Unmarshal([]byte(`{"identityId": "00000000-0000-0000-0000-00000000000a", "delegationsValidFrom": "2026-10-28T00:00:00Z"}`), &set))
	require.Equal(t, storage.EpochInfo{IdentityID: agentID, DelegationsValidFrom: ptr(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))}, set)

	var cleared storage.EpochInfo
	require.NoError(t, json.Unmarshal([]byte(`{"identityId": "00000000-0000-0000-0000-00000000000a", "delegationsValidFrom": null}`), &cleared))
	require.Equal(t, storage.EpochInfo{IdentityID: agentID}, cleared)

	var missing storage.EpochInfo
	require.Error(t, json.Unmarshal([]byte(`{"delegationsValidFrom": null}`), &missing))
}

func TestEnrollAgentRequestMarshalsTheCertificateAsAnObject(t *testing.T) {
	cert, _, agent := issued(t)
	b, err := json.Marshal(storage.EnrollAgentRequest{Name: "agent", PublicKey: agent.PublicKey(), Certificate: cert})
	require.NoError(t, err)

	var members map[string]jsontext.Value
	require.NoError(t, json.Unmarshal(b, &members))
	require.Equal(t, jsontext.Kind('{'), members["certificate"].Kind())
	require.Equal(t, "null", string(members["email"]))
	require.Equal(t, `"`+agent.PublicKey().String()+`"`, string(members["publicKey"]))
	require.Equal(t, `"agent"`, string(members["name"]))

	want, err := cert.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, string(want), string(members["certificate"]))
}

func TestAgentsEnroll(t *testing.T) {
	cert, _, agent := issued(t)
	c, got := serve(t, "POST /identities/agents", raw(http.StatusCreated, envelope(enrolledAgent)))
	info, err := c.Agents().Enroll(t.Context(), storage.EnrollAgentRequest{Name: "agent", PublicKey: agent.PublicKey(), Certificate: cert})
	require.NoError(t, err)
	require.Equal(t, wantEnrolledAgent(), info)
	require.Nil(t, info.PublishedDelegations)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/identities/agents", got.requestURI)

	var body map[string]jsontext.Value
	require.NoError(t, json.Unmarshal([]byte(got.body), &body))
	require.Equal(t, "null", string(body["email"]))
	var sent map[string]jsontext.Value
	require.NoError(t, json.Unmarshal(body["certificate"], &sent))
	require.Equal(t, jsontext.Kind('{'), sent["proof"].Kind())

	parsed, err := delegation.ParseCertificate(body["certificate"])
	require.NoError(t, err)
	require.NoError(t, parsed.Verify(signerFromSeed(t, seed1).PublicKey()))
}

func TestAgentsEnrollSendsTheEmail(t *testing.T) {
	cert, _, agent := issued(t)
	c, got := serve(t, "POST /identities/agents", raw(http.StatusCreated, envelope(enrolledAgent)))
	_, err := c.Agents().Enroll(t.Context(), storage.EnrollAgentRequest{Name: "agent", Email: ptr("agent@example.com"), PublicKey: agent.PublicKey(), Certificate: cert})
	require.NoError(t, err)

	var body map[string]jsontext.Value
	require.NoError(t, json.Unmarshal([]byte(got.body), &body))
	require.Equal(t, `"agent@example.com"`, string(body["email"]))
}

func TestAgentsEnrollRefusesWithNothingSent(t *testing.T) {
	cert, human, agent := issued(t)
	tests := map[string]storage.EnrollAgentRequest{
		"zero certificate": {Name: "agent", PublicKey: agent.PublicKey()},
		"zero public key":  {Name: "agent", Certificate: cert},
		"another key":      {Name: "agent", PublicKey: human.PublicKey(), Certificate: cert},
	}
	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusCreated, envelope(enrolledAgent)))
			_, err := c.Agents().Enroll(t.Context(), req)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			require.Zero(t, got.calls)
		})
	}
}

func TestAgentsEnrollUnavailable(t *testing.T) {
	cert, _, agent := issued(t)
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c, _ := serve(t, "POST /identities/agents", raw(status, `{"error": "enrollment unavailable"}`))
			_, err := c.Agents().Enroll(t.Context(), storage.EnrollAgentRequest{Name: "agent", PublicKey: agent.PublicKey(), Certificate: cert})

			var rerr *remote.Error
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, remote.KindHTTP, rerr.Kind)
			require.Equal(t, status, rerr.StatusCode)
			require.Equal(t, "enroll_agent", rerr.Op)
			require.Equal(t, `{"error": "enrollment unavailable"}`, rerr.Body)
			if status == http.StatusNotFound {
				require.ErrorIs(t, err, remote.ErrNotFound)
			} else {
				require.NotErrorIs(t, err, remote.ErrNotFound)
			}
		})
	}
}

func TestAgentsList(t *testing.T) {
	c, got := serve(t, "GET /identities/agents", raw(http.StatusOK, envelope("["+enrolledAgent+","+fullAgent+"]")))
	list, err := c.Agents().List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []storage.AgentIdentityInfo{wantEnrolledAgent(), wantFullAgent()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/identities/agents", got.requestURI)
	require.Empty(t, got.body)
}

func TestAgentsGet(t *testing.T) {
	c, got := serve(t, "GET /identities/agents/{id}", raw(http.StatusOK, envelope(fullAgent)))
	info, err := c.Agents().Get(t.Context(), agentID)
	require.NoError(t, err)
	require.Equal(t, wantFullAgent(), info)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/identities/agents/00000000-0000-0000-0000-00000000000a", got.requestURI)
	require.Empty(t, got.body)
}

func TestAgentsRetire(t *testing.T) {
	c, got := serve(t, "DELETE /identities/agents/{id}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.Agents().Retire(t.Context(), agentID))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/identities/agents/00000000-0000-0000-0000-00000000000a", got.requestURI)
	require.Empty(t, got.body)
}

func TestAgentsSetEpochSendsNoBody(t *testing.T) {
	c, got, rec := domainServe(t, "POST /identities/agents/{id}/epoch", raw(http.StatusOK, envelope(`{"identityId": "00000000-0000-0000-0000-00000000000a", "delegationsValidFrom": "2026-10-28T00:00:00Z"}`)))
	epoch, err := c.Agents().SetEpoch(t.Context(), agentID)
	require.NoError(t, err)
	require.Equal(t, storage.EpochInfo{IdentityID: agentID, DelegationsValidFrom: ptr(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))}, epoch)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/identities/agents/00000000-0000-0000-0000-00000000000a/epoch", got.requestURI)
	require.Empty(t, got.body)
	require.Equal(t, int64(0), rec.contentLength)
	require.Equal(t, []string{"application/json"}, rec.header.Values("Content-Type"))
}

func TestAgentsDeletesSucceedWhateverTheBody(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"an envelope":   raw(http.StatusOK, envelope(enrolledAgent)),
		"not JSON":      raw(http.StatusOK, "retired"),
		"a failed body": raw(http.StatusAccepted, `{"success": false}`),
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, "DELETE /identities/agents/", h)
			require.NoError(t, c.Agents().Retire(t.Context(), agentID))
			require.NoError(t, c.Agents().ClearEpoch(t.Context(), agentID))
		})
	}
}

func TestAgentsClearEpoch(t *testing.T) {
	c, got := serve(t, "DELETE /identities/agents/{id}/epoch", raw(http.StatusNoContent, ""))
	require.NoError(t, c.Agents().ClearEpoch(t.Context(), agentID))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/identities/agents/00000000-0000-0000-0000-00000000000a/epoch", got.requestURI)
	require.Empty(t, got.body)
}

func TestAgentsNotFoundMatchesErrNotFound(t *testing.T) {
	notFound := func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "agent not found"))
	}
	agents := func(t *testing.T) storage.Agents {
		c, _ := serve(t, "/", notFound)
		return c.Agents()
	}

	_, err := agents(t).List(t.Context())
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = agents(t).Get(t.Context(), agentID)
	require.ErrorIs(t, err, remote.ErrNotFound)

	err = agents(t).Retire(t.Context(), agentID)
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = agents(t).SetEpoch(t.Context(), agentID)
	require.ErrorIs(t, err, remote.ErrNotFound)

	err = agents(t).ClearEpoch(t.Context(), agentID)
	require.ErrorIs(t, err, remote.ErrNotFound)
}
