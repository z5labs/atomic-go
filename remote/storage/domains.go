package storage

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// DomainAliasInfo is a domain an org claims, as the domain requests report
// it (docs/remote/endpoints/storage.md#domainaliasinfo).
//
// It is read from camelCase members only, as the Rust client reads it: it
// accepts no snake_case spellings. Every member but verificationToken is
// required; a member written twice is refused.
type DomainAliasInfo struct {
	// ID is the domain alias's ID: the ID [Domains.Verify] and
	// [Domains.Revoke] take.
	ID uuid.UUID `json:"id"`

	// OrgID is the ID of the org that claims the domain.
	OrgID uuid.UUID `json:"orgId"`

	// Domain is the domain, such as "eng.acme.com".
	Domain string `json:"domain"`

	// Status is the claim's status, such as "pending" or "verified".
	Status string `json:"status"`

	// VerificationMethod is how the claim is verified, such as "dns-txt"
	// or "dns-cname".
	VerificationMethod string `json:"verificationMethod"`

	// VerificationToken is the one-time token to publish in DNS, or nil
	// when the server sent none or null.
	VerificationToken *string `json:"verificationToken"`
}

// UnmarshalJSON decodes a domain alias with the required members
// [DomainAliasInfo] describes.
func (d *DomainAliasInfo) UnmarshalJSON(b []byte) error {
	type plain DomainAliasInfo
	return unmarshalAliased(b, (*plain)(d), nil, "id", "orgId", "domain", "status", "verificationMethod")
}

// ClaimDomainRequest is the body of [Domains.Claim]
// (docs/remote/endpoints/storage.md#claimdomainrequest).
type ClaimDomainRequest struct {
	// Domain is the domain to claim, such as "eng.acme.com".
	Domain string `json:"domain"`
}

// Domains sends the domain alias requests of the management API
// (docs/remote/endpoints/storage.md#organizations). Get one from
// [Client.Domains]; its zero value is not usable.
//
// Every request acts on one org. Nothing in the Rust client calls them, so
// which base they go to is assumed from their paths: an org base, as
// serverurl.Server.OrgBase returns it. This is not checked. The slug is
// sent as one path segment and refused, with an error wrapping
// [ErrInvalidArgument] and nothing sent, when it would address another
// path. A domain alias ID is sent in its hyphenated form.
//
// A failure is the server's status: a 403 matches [remote.ErrForbidden], a
// 404 [remote.ErrNotFound] and a 409 [remote.ErrConflict].
type Domains struct {
	c *Client
}

// Domains returns the domain alias requests sent through c.
func (c *Client) Domains() Domains {
	return Domains{c: c}
}

// List returns every domain the org slug names claims:
// GET /orgs/{slug}/domains.
func (d Domains) List(ctx context.Context, slug string) ([]DomainAliasInfo, error) {
	path, err := orgPath(slug, "/domains")
	if err != nil {
		return nil, err
	}
	return call[[]DomainAliasInfo](ctx, d.c, request{
		op:     "list_domains",
		method: http.MethodGet,
		path:   path,
	})
}

// Claim claims req.Domain for the org slug names and returns the claim:
// POST /orgs/{slug}/domains. The Rust client documents that a claim starts
// "pending" and is verified with [Domains.Verify] once its DNS records are
// in place.
func (d Domains) Claim(ctx context.Context, slug string, req ClaimDomainRequest) (DomainAliasInfo, error) {
	path, err := orgPath(slug, "/domains")
	if err != nil {
		return DomainAliasInfo{}, err
	}
	return call[DomainAliasInfo](ctx, d.c, request{
		op:     "claim_domain",
		method: http.MethodPost,
		path:   path,
		body:   req,
	})
}

// Verify asks the server to check the DNS records of the claim id and
// returns the claim: POST /orgs/{slug}/domains/{id}/verify. The Rust client
// documents that a claim whose records check out moves from "pending" to
// "verified". The request has no body but still carries the Content-Type
// every request does, as the Rust client's post_empty sends it.
func (d Domains) Verify(ctx context.Context, slug string, id uuid.UUID) (DomainAliasInfo, error) {
	path, err := orgPath(slug, "/domains/"+id.String()+"/verify")
	if err != nil {
		return DomainAliasInfo{}, err
	}
	return call[DomainAliasInfo](ctx, d.c, request{
		op:     "verify_domain",
		method: http.MethodPost,
		path:   path,
	})
}

// Revoke revokes the claim id of the org slug names:
// DELETE /orgs/{slug}/domains/{id}. Any 2xx response is success, whatever
// its body.
func (d Domains) Revoke(ctx context.Context, slug string, id uuid.UUID) error {
	path, err := orgPath(slug, "/domains/"+id.String())
	if err != nil {
		return err
	}
	_, err = call[struct{}](ctx, d.c, request{
		op:     "revoke_domain",
		method: http.MethodDelete,
		path:   path,
	})
	return err
}
