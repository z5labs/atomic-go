package storage_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/storage"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// activeDelegation is a DelegationInfo body with every member the docs list
// (docs/remote/endpoints/storage.md#delegationinfo).
const activeDelegation = `{"id": "urn:atomic:delegation:X", "delegatorIdentityId": "00000000-0000-0000-0000-000000000001", "delegateIdentityId": "00000000-0000-0000-0000-00000000000a", "status": "active", "issuedAt": "2026-10-27T00:00:00Z", "expiresAt": "2026-11-27T00:00:00Z", "certificate": {"@type": "AgentDelegation"}}`

func wantActiveDelegation() storage.DelegationInfo {
	return storage.DelegationInfo{
		ID:                  "urn:atomic:delegation:X",
		DelegatorIdentityID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		DelegateIdentityID:  agentID,
		Status:              "active",
		IssuedAt:            issuedAt,
		ExpiresAt:           ptr(time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)),
		// The certificate is compacted: its value is the server's, not its
		// whitespace.
		Certificate: jsontext.Value(`{"@type":"AgentDelegation"}`),
	}
}

// revokedDelegation is a DelegationInfo body without its optional members.
const revokedDelegation = `{"id": "urn:atomic:delegation:X", "delegatorIdentityId": "00000000-0000-0000-0000-000000000001", "delegateIdentityId": "00000000-0000-0000-0000-00000000000a", "status": "revoked", "issuedAt": "2026-10-27T00:00:00Z"}`

func wantRevokedDelegation() storage.DelegationInfo {
	d := wantActiveDelegation()
	d.Status = "revoked"
	d.ExpiresAt = nil
	d.Certificate = nil
	return d
}

// revoked returns a certificate from issued and the delegator's revocation
// of it, made with delegation.Revoke.
func revoked(t *testing.T) (delegation.Certificate, delegation.Revocation) {
	t.Helper()
	cert, human, _ := issued(t)
	rev, err := delegation.Revoke(t.Context(), human, cert.ID(), issuedAt.Add(time.Hour), "compromised")
	require.NoError(t, err)
	return cert, rev
}

// escapedID returns id's URN as the Rust client writes it into a path.
func escapedID(id delegation.ID) string {
	return strings.ReplaceAll(id.String(), ":", "%3A")
}

func TestDelegationInfoReadsTheDocsBodies(t *testing.T) {
	var active storage.DelegationInfo
	require.NoError(t, json.Unmarshal([]byte(activeDelegation), &active))
	require.Equal(t, wantActiveDelegation(), active)

	var bare storage.DelegationInfo
	require.NoError(t, json.Unmarshal([]byte(revokedDelegation), &bare))
	require.Equal(t, wantRevokedDelegation(), bare)

	var null storage.DelegationInfo
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSuffix(revokedDelegation, "}")+`, "expiresAt": null, "certificate": null}`), &null))
	require.Equal(t, wantRevokedDelegation(), null)
}

func TestDelegationInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing id":                  `{"delegatorIdentityId": "00000000-0000-0000-0000-000000000001", "delegateIdentityId": "00000000-0000-0000-0000-00000000000a", "status": "active", "issuedAt": "2026-10-27T00:00:00Z"}`,
		"missing issuedAt":            `{"id": "urn:atomic:delegation:X", "delegatorIdentityId": "00000000-0000-0000-0000-000000000001", "delegateIdentityId": "00000000-0000-0000-0000-00000000000a", "status": "active"}`,
		"snake delegate_identity_id":  `{"id": "urn:atomic:delegation:X", "delegatorIdentityId": "00000000-0000-0000-0000-000000000001", "delegate_identity_id": "00000000-0000-0000-0000-00000000000a", "status": "active", "issuedAt": "2026-10-27T00:00:00Z"}`,
		"written twice":               `{"id": "urn:atomic:delegation:X", "id": "urn:atomic:delegation:X", "delegatorIdentityId": "00000000-0000-0000-0000-000000000001", "delegateIdentityId": "00000000-0000-0000-0000-00000000000a", "status": "active", "issuedAt": "2026-10-27T00:00:00Z"}`,
		"bad delegatorIdentityId":     `{"id": "urn:atomic:delegation:X", "delegatorIdentityId": "one", "delegateIdentityId": "00000000-0000-0000-0000-00000000000a", "status": "active", "issuedAt": "2026-10-27T00:00:00Z"}`,
		"certificate after the close": `{"id": "urn:atomic:delegation:X", "delegatorIdentityId": "00000000-0000-0000-0000-000000000001", "delegateIdentityId": "00000000-0000-0000-0000-00000000000a", "status": "active", "issuedAt": "2026-10-27T00:00:00Z"} {}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.DelegationInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestDelegationStatusInfo(t *testing.T) {
	var got storage.DelegationStatusInfo
	require.NoError(t, json.Unmarshal([]byte(`{"id": "urn:atomic:delegation:X", "revoked": true}`), &got))
	require.Equal(t, storage.DelegationStatusInfo{ID: "urn:atomic:delegation:X", Revoked: true}, got)

	for name, body := range map[string]string{
		"missing revoked": `{"id": "urn:atomic:delegation:X"}`,
		"null revoked":    `{"id": "urn:atomic:delegation:X", "revoked": null}`,
		"missing id":      `{"revoked": false}`,
	} {
		t.Run(name, func(t *testing.T) {
			var bad storage.DelegationStatusInfo
			require.Error(t, json.Unmarshal([]byte(body), &bad))
		})
	}
}

func TestDelegationRequestsMarshalTheirDocumentsAsObjects(t *testing.T) {
	cert, rev := revoked(t)

	b, err := json.Marshal(storage.PushDelegationRequest{Certificate: cert})
	require.NoError(t, err)
	want, err := cert.MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"certificate": `+string(want)+`}`, string(b))

	b, err = json.Marshal(storage.RevokeDelegationRequest{Revocation: rev})
	require.NoError(t, err)
	want, err = rev.MarshalJSON()
	require.NoError(t, err)
	require.JSONEq(t, `{"revocation": `+string(want)+`}`, string(b))
	var members map[string]jsontext.Value
	require.NoError(t, json.Unmarshal(b, &members))
	require.Equal(t, jsontext.Kind('{'), members["revocation"].Kind())
}

func TestDelegationsPush(t *testing.T) {
	cert, _ := revoked(t)
	c, got := serve(t, "POST /delegations", raw(http.StatusCreated, envelope(activeDelegation)))
	info, err := c.Delegations().Push(t.Context(), storage.PushDelegationRequest{Certificate: cert})
	require.NoError(t, err)
	require.Equal(t, wantActiveDelegation(), info)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/delegations", got.requestURI)

	var body map[string]jsontext.Value
	require.NoError(t, json.Unmarshal([]byte(got.body), &body))
	parsed, err := delegation.ParseCertificate(body["certificate"])
	require.NoError(t, err)
	require.Equal(t, cert.ID(), parsed.ID())
	require.NoError(t, parsed.Verify(signerFromSeed(t, seed1).PublicKey()))
}

func TestDelegationsPushRefusesTheZeroCertificate(t *testing.T) {
	c, got := serve(t, "/", raw(http.StatusCreated, envelope(activeDelegation)))
	_, err := c.Delegations().Push(t.Context(), storage.PushDelegationRequest{})
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
	require.Zero(t, got.calls)
}

func TestDelegationsList(t *testing.T) {
	c, got := serve(t, "GET /delegations", raw(http.StatusOK, envelope("["+activeDelegation+","+revokedDelegation+"]")))
	list, err := c.Delegations().List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []storage.DelegationInfo{wantActiveDelegation(), wantRevokedDelegation()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/delegations", got.requestURI)
	require.Empty(t, got.body)
}

func TestDelegationsRevoke(t *testing.T) {
	cert, rev := revoked(t)
	c, got := serve(t, "POST /delegations/{id}/revoke", raw(http.StatusOK, envelope(revokedDelegation)))
	info, err := c.Delegations().Revoke(t.Context(), cert.ID(), storage.RevokeDelegationRequest{Revocation: rev})
	require.NoError(t, err)
	require.Equal(t, wantRevokedDelegation(), info)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/delegations/"+escapedID(cert.ID())+"/revoke", got.requestURI)
	require.True(t, strings.HasPrefix(got.requestURI, "/delegations/urn%3Aatomic%3Adelegation%3A"))

	var body map[string]jsontext.Value
	require.NoError(t, json.Unmarshal([]byte(got.body), &body))
	parsed, err := delegation.ParseRevocation(body["revocation"])
	require.NoError(t, err)
	require.Equal(t, cert.ID(), parsed.DelegationRevocation().Delegation)
	require.NoError(t, parsed.Verify(signerFromSeed(t, seed1).PublicKey()))
}

func TestDelegationsRevokeRefusesWithNothingSent(t *testing.T) {
	cert, rev := revoked(t)
	var other delegation.ID
	other[0] = 1
	tests := map[string]struct {
		id  delegation.ID
		req storage.RevokeDelegationRequest
	}{
		"zero id":         {req: storage.RevokeDelegationRequest{Revocation: rev}},
		"zero revocation": {id: cert.ID()},
		"another id":      {id: other, req: storage.RevokeDelegationRequest{Revocation: rev}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(revokedDelegation)))
			_, err := c.Delegations().Revoke(t.Context(), tc.id, tc.req)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			require.Zero(t, got.calls)
		})
	}
}

func TestDelegationsSetDelegatorEpochSendsNoBody(t *testing.T) {
	c, got, rec := domainServe(t, "POST /delegations/epoch", raw(http.StatusOK, envelope(`{"identityId": "00000000-0000-0000-0000-000000000001", "delegationsValidFrom": "2026-10-28T00:00:00Z"}`)))
	epoch, err := c.Delegations().SetDelegatorEpoch(t.Context())
	require.NoError(t, err)
	require.Equal(t, storage.EpochInfo{IdentityID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), DelegationsValidFrom: ptr(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))}, epoch)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/delegations/epoch", got.requestURI)
	require.Empty(t, got.body)
	require.Equal(t, int64(0), rec.contentLength)
	require.Equal(t, []string{"application/json"}, rec.header.Values("Content-Type"))
}

func TestDelegationsSetDelegatorEpochWithANullEpoch(t *testing.T) {
	c, _ := serve(t, "POST /delegations/epoch", raw(http.StatusOK, envelope(`{"identityId": "00000000-0000-0000-0000-000000000001", "delegationsValidFrom": null}`)))
	epoch, err := c.Delegations().SetDelegatorEpoch(t.Context())
	require.NoError(t, err)
	require.Equal(t, storage.EpochInfo{IdentityID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}, epoch)
	require.Nil(t, epoch.DelegationsValidFrom)
}

func TestDelegationsStatus(t *testing.T) {
	cert, _ := revoked(t)
	c, got := serve(t, "GET /delegations/{id}/status", raw(http.StatusOK, envelope(`{"id": "`+cert.ID().String()+`", "revoked": true}`)))
	status, err := c.Delegations().Status(t.Context(), cert.ID())
	require.NoError(t, err)
	require.Equal(t, storage.DelegationStatusInfo{ID: cert.ID().String(), Revoked: true}, status)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/delegations/"+escapedID(cert.ID())+"/status", got.requestURI)
	require.Empty(t, got.body)
}

func TestDelegationsStatusRefusesTheZeroID(t *testing.T) {
	c, got := serve(t, "/", raw(http.StatusOK, envelope(`{"id": "x", "revoked": false}`)))
	_, err := c.Delegations().Status(t.Context(), delegation.ID{})
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
	require.Zero(t, got.calls)
}

func TestDelegationsNotFoundMatchesErrNotFound(t *testing.T) {
	cert, rev := revoked(t)
	notFound := func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "delegation not found"))
	}
	delegations := func(t *testing.T) storage.Delegations {
		c, _ := serve(t, "/", notFound)
		return c.Delegations()
	}

	_, err := delegations(t).Push(t.Context(), storage.PushDelegationRequest{Certificate: cert})
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = delegations(t).List(t.Context())
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = delegations(t).Revoke(t.Context(), cert.ID(), storage.RevokeDelegationRequest{Revocation: rev})
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = delegations(t).SetDelegatorEpoch(t.Context())
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = delegations(t).Status(t.Context(), cert.ID())
	require.ErrorIs(t, err, remote.ErrNotFound)
}
