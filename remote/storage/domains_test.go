package storage_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/storage"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// domainID is the domain alias the domain tests act on.
var domainID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// pendingDomain is a DomainAliasInfo body with every member the docs list
// (docs/remote/endpoints/storage.md#domainaliasinfo).
const pendingDomain = `{"id": "00000000-0000-0000-0000-000000000001", "orgId": "00000000-0000-0000-0000-000000000009", "domain": "eng.acme.com", "status": "pending", "verificationMethod": "dns-txt", "verificationToken": "atomic-verify=abc"}`

// verifiedDomain is a DomainAliasInfo body without its optional member.
const verifiedDomain = `{"id": "00000000-0000-0000-0000-000000000001", "orgId": "00000000-0000-0000-0000-000000000009", "domain": "eng.acme.com", "status": "verified", "verificationMethod": "dns-txt"}`

func wantPendingDomain() storage.DomainAliasInfo {
	return storage.DomainAliasInfo{
		ID:                 domainID,
		OrgID:              uuid.MustParse("00000000-0000-0000-0000-000000000009"),
		Domain:             "eng.acme.com",
		Status:             "pending",
		VerificationMethod: "dns-txt",
		VerificationToken:  ptr("atomic-verify=abc"),
	}
}

func wantVerifiedDomain() storage.DomainAliasInfo {
	d := wantPendingDomain()
	d.Status = "verified"
	d.VerificationToken = nil
	return d
}

// recordedRequest is what domainServe saw of the last request beyond what
// serve records.
type recordedRequest struct {
	header        http.Header
	contentLength int64
}

// domainServe is serve with the request's headers and Content-Length
// recorded too, as the server read them off the wire.
func domainServe(t *testing.T, pattern string, h http.HandlerFunc) (*storage.Client, *served, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{contentLength: -2}
	c, got := serve(t, pattern, func(w http.ResponseWriter, r *http.Request) {
		rec.header = r.Header.Clone()
		rec.contentLength = r.ContentLength
		h(w, r)
	})
	return c, got, rec
}

func TestDomainAliasInfoReadsTheDocsBody(t *testing.T) {
	var got storage.DomainAliasInfo
	require.NoError(t, json.Unmarshal([]byte(pendingDomain), &got))
	require.Equal(t, wantPendingDomain(), got)

	var bare storage.DomainAliasInfo
	require.NoError(t, json.Unmarshal([]byte(verifiedDomain), &bare))
	require.Equal(t, wantVerifiedDomain(), bare)
}

func TestDomainAliasInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing id":                 `{"orgId": "00000000-0000-0000-0000-000000000009", "domain": "a.com", "status": "pending", "verificationMethod": "dns-txt"}`,
		"missing verificationMethod": `{"id": "00000000-0000-0000-0000-000000000001", "orgId": "00000000-0000-0000-0000-000000000009", "domain": "a.com", "status": "pending"}`,
		"snake_case org_id":          `{"id": "00000000-0000-0000-0000-000000000001", "org_id": "00000000-0000-0000-0000-000000000009", "domain": "a.com", "status": "pending", "verificationMethod": "dns-txt"}`,
		"null domain":                `{"id": "00000000-0000-0000-0000-000000000001", "orgId": "00000000-0000-0000-0000-000000000009", "domain": null, "status": "pending", "verificationMethod": "dns-txt"}`,
		"written twice":              `{"id": "00000000-0000-0000-0000-000000000001", "id": "00000000-0000-0000-0000-000000000001", "orgId": "00000000-0000-0000-0000-000000000009", "domain": "a.com", "status": "pending", "verificationMethod": "dns-txt"}`,
		"bad id":                     `{"id": "one", "orgId": "00000000-0000-0000-0000-000000000009", "domain": "a.com", "status": "pending", "verificationMethod": "dns-txt"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var got storage.DomainAliasInfo
			require.Error(t, json.Unmarshal([]byte(body), &got))
		})
	}
}

func TestDomainsList(t *testing.T) {
	c, got := serve(t, "GET /orgs/{slug}/domains", raw(http.StatusOK, envelope("["+pendingDomain+","+verifiedDomain+"]")))
	list, err := c.Domains().List(t.Context(), "acme")
	require.NoError(t, err)
	require.Equal(t, []storage.DomainAliasInfo{wantPendingDomain(), wantVerifiedDomain()}, list)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, "/orgs/acme/domains", got.requestURI)
	require.Empty(t, got.body)
}

func TestDomainsClaim(t *testing.T) {
	c, got := serve(t, "POST /orgs/{slug}/domains", raw(http.StatusCreated, envelope(pendingDomain)))
	d, err := c.Domains().Claim(t.Context(), "acme", storage.ClaimDomainRequest{Domain: "eng.acme.com"})
	require.NoError(t, err)
	require.Equal(t, wantPendingDomain(), d)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/orgs/acme/domains", got.requestURI)
	require.JSONEq(t, `{"domain": "eng.acme.com"}`, got.body)
}

func TestDomainsVerifySendsNoBody(t *testing.T) {
	c, got, rec := domainServe(t, "POST /orgs/{slug}/domains/{id}/verify", raw(http.StatusOK, envelope(verifiedDomain)))
	d, err := c.Domains().Verify(t.Context(), "acme", domainID)
	require.NoError(t, err)
	require.Equal(t, wantVerifiedDomain(), d)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/orgs/acme/domains/00000000-0000-0000-0000-000000000001/verify", got.requestURI)
	require.Empty(t, got.body)
	require.Equal(t, int64(0), rec.contentLength)
	require.Equal(t, []string{"application/json"}, rec.header.Values("Content-Type"))
}

func TestDomainsRevoke(t *testing.T) {
	c, got := serve(t, "DELETE /orgs/{slug}/domains/{id}", raw(http.StatusNoContent, ""))
	require.NoError(t, c.Domains().Revoke(t.Context(), "acme", domainID))
	require.Equal(t, http.MethodDelete, got.method)
	require.Equal(t, "/orgs/acme/domains/00000000-0000-0000-0000-000000000001", got.requestURI)
	require.Empty(t, got.body)
}

func TestDomainsNotFoundMatchesErrNotFound(t *testing.T) {
	notFound := func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "domain not found"))
	}
	domains := func(t *testing.T) storage.Domains {
		c, _ := serve(t, "/", notFound)
		return c.Domains()
	}

	_, err := domains(t).List(t.Context(), "gone")
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = domains(t).Claim(t.Context(), "gone", storage.ClaimDomainRequest{Domain: "eng.acme.com"})
	require.ErrorIs(t, err, remote.ErrNotFound)

	_, err = domains(t).Verify(t.Context(), "acme", domainID)
	require.ErrorIs(t, err, remote.ErrNotFound)

	err = domains(t).Revoke(t.Context(), "acme", domainID)
	require.ErrorIs(t, err, remote.ErrNotFound)
}

func TestDomainsRefuseBadSlugsWithNothingSent(t *testing.T) {
	for _, slug := range []string{"..", ".", "", "a/b", "%2e%2e", "a?b", "a#b"} {
		t.Run(slug, func(t *testing.T) {
			c, got := serve(t, "/", raw(http.StatusOK, envelope(pendingDomain)))
			domains := c.Domains()

			_, err := domains.List(t.Context(), slug)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = domains.Claim(t.Context(), slug, storage.ClaimDomainRequest{Domain: "eng.acme.com"})
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			_, err = domains.Verify(t.Context(), slug, domainID)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			err = domains.Revoke(t.Context(), slug, domainID)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)

			require.Zero(t, got.calls)
		})
	}
}
