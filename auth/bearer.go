package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/token"
)

// Bearer is the [Credentials] of one identity for one audience. It mints a
// fresh token on every [Bearer.Attach], so a long operation never sends a
// token older than [token.Lifetime], and when it was built with
// [WithCertificate] it sends the delegation certificate beside every token
// (docs/remote/auth.md#request-headers,
// docs/remote/auth.md#lifetime-clock-and-reuse).
//
// Build one with [NewBearer], or with an [Issuer]; the zero Bearer is not
// usable. A Bearer holds no mutable state of its own, so it is safe for
// concurrent use as long as its signer and clock are. It serializes its reads
// of the random source itself.
type Bearer struct {
	minter *token.Minter
	clock  func() time.Time

	// delegated reports whether cert is set. header is cert's
	// Atomic-Delegation value.
	delegated bool
	cert      delegation.Certificate
	header    string
}

// BearerOption configures a [Bearer].
type BearerOption func(*bearerConfig)

type bearerConfig struct {
	cert    delegation.Certificate
	hasCert bool
}

// WithCertificate makes the [Bearer] delegated: every token it mints names
// the certificate's delegator as its sub, the signer as its act.sub and the
// certificate as its dlg, and every request it attaches to carries the
// certificate in the [delegation.HeaderName] header. [NewBearer] refuses a
// certificate that is the zero Certificate, does not verify against its own
// delegatorKey, was issued to a delegate other than the signer, or has
// expired.
func WithCertificate(c delegation.Certificate) BearerOption {
	return func(cfg *bearerConfig) {
		cfg.cert = c
		cfg.hasCert = true
	}
}

// NewBearer returns a [Bearer] whose tokens are signed by signer for the
// server URL audience, issued at the instant clock returns, with ids read
// from random.
//
// Every argument is required, and nothing falls back to a default: NewBearer
// refuses a nil signer, an audience that [token.Audience] makes empty, a nil
// clock and a nil random. With [WithCertificate] the certificate is required
// too, and it must verify against its own delegatorKey
// ([delegation.Certificate.VerifySelfContained]), its delegate must be the
// signer, in both DID forms, and it must not have expired at clock's current
// instant. NewBearer names every problem in one error made with
// [errors.Join], each wrapping [ErrInvalidArgument]; a certificate issued to
// another delegate also wraps [delegation.ErrWrongDelegate], and an expired
// one [delegation.ErrExpired].
//
// The audience is always the caller's: [ProjectAudience] and
// [ManagementAudience] give the one the Rust client uses for each protocol.
func NewBearer(signer identity.Signer, audience string, clock func() time.Time, random io.Reader, opts ...BearerOption) (*Bearer, error) {
	var cfg bearerConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	var errs []error
	refuse := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalidArgument}, args...)...))
	}
	if signer == nil {
		refuse("the signer is required")
	}
	if token.Audience(audience) == "" {
		refuse("the audience is required")
	}
	if clock == nil {
		refuse("the clock is required")
	}
	if random == nil {
		refuse("the random source is required")
	}
	if len(errs) > 0 {
		// The certificate is checked against the signer and the clock, so
		// it cannot be checked without them.
		return nil, fmt.Errorf("auth: NewBearer: %w", errors.Join(errs...))
	}

	var minterOpts []token.MinterOption
	if cfg.hasCert {
		obo, err := checkCertificate(cfg.cert, signer.PublicKey(), clock())
		if err != nil {
			return nil, fmt.Errorf("auth: NewBearer: %w", err)
		}
		minterOpts = append(minterOpts, token.WithOnBehalfOf(obo))
	}
	m, err := token.NewMinter(signer, audience, clock, random, minterOpts...)
	if err != nil {
		return nil, fmt.Errorf("auth: NewBearer: %w: %w", ErrInvalidArgument, err)
	}
	b := &Bearer{minter: m, clock: clock}
	if cfg.hasCert {
		b.delegated = true
		b.cert = cfg.cert
		b.header = cfg.cert.HeaderValue()
	}
	return b, nil
}

// checkCertificate checks c as [NewBearer] documents, for the signer whose
// key is agent at now, and returns what a delegated token says about it.
func checkCertificate(c delegation.Certificate, agent identity.PublicKey, now time.Time) (token.OnBehalfOf, error) {
	if c.HeaderValue() == "" {
		return token.OnBehalfOf{}, fmt.Errorf("%w: the certificate is required", ErrInvalidArgument)
	}
	if err := c.VerifySelfContained(); err != nil {
		return token.OnBehalfOf{}, fmt.Errorf("%w: the certificate does not verify: %w", ErrInvalidArgument, err)
	}
	ad := c.AgentDelegation()
	var errs []error
	if !ad.Delegate.Matches(agent) || !ad.DelegateKey.Matches(agent) {
		errs = append(errs, fmt.Errorf("%w: %w: the certificate is issued to %s, not to the signer %s",
			ErrInvalidArgument, delegation.ErrWrongDelegate, ad.Delegate, agent.AtomicDID()))
	}
	if c.Expired(now) {
		errs = append(errs, fmt.Errorf("%w: %w: the certificate %s expired at %s",
			ErrInvalidArgument, delegation.ErrExpired, c.ID(), ad.Expires.Format(time.RFC3339)))
	}
	// VerifySelfContained has checked that delegatorKey is a did:key.
	delegator, ok := ad.DelegatorKey.PublicKey()
	if !ok {
		errs = append(errs, fmt.Errorf("%w: the certificate's delegatorKey is not a did:key", ErrInvalidArgument))
	}
	if len(errs) > 0 {
		return token.OnBehalfOf{}, errors.Join(errs...)
	}
	return token.OnBehalfOf{Delegator: delegator, Delegation: c.ID().URN()}, nil
}

// Token returns a newly minted token, issued at the clock's current instant
// and with a new id. Every call mints afresh; nothing is cached.
//
// A delegated Bearer first re-checks its certificate's expiry at that
// instant, and fails with an error wrapping [delegation.ErrExpired] once it
// has passed, so a long operation stops sending a certificate the server
// would refuse. It does not re-check revocation: the server does that, and
// knows of revocations this machine does not.
func (b *Bearer) Token(ctx context.Context) (token.Token, error) {
	if b.delegated && b.cert.Expired(b.clock()) {
		return token.Token{}, fmt.Errorf("auth: the delegation certificate %s expired at %s: %w",
			b.cert.ID(), b.cert.AgentDelegation().Expires.Format(time.RFC3339), delegation.ErrExpired)
	}
	t, err := b.minter.Mint(ctx)
	if err != nil {
		return token.Token{}, fmt.Errorf("auth: minting a token: %w", err)
	}
	return t, nil
}

// Attach sets req's Authorization header to "Bearer " followed by a token
// from [Bearer.Token], minted with req.Context(), and for a delegated Bearer
// its [delegation.HeaderName] header to the certificate's header encoding.
// Both replace any value already set. On failure, req is left unchanged.
func (b *Bearer) Attach(req *http.Request) error {
	t, err := b.Token(req.Context())
	if err != nil {
		return err
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Authorization", "Bearer "+t.String())
	if b.delegated {
		req.Header.Set(delegation.HeaderName, b.header)
	}
	return nil
}

// ProjectAudience returns the audience of the tokens the project protocol
// sends for p, push, pull and clone alike: [token.Audience] of
// [serverurl.Project.TokenApex], as the Rust client derives it
// (docs/remote/auth.md#audience). So http://alice.localhost:8080/... gives
// http://localhost:8080. It returns "" for the zero Project.
//
// TokenApex reproduces the Rust client exactly, including for a single-tenant
// server or an IP host, where the result is not the server's apex
// (docs/remote/transport.md#open-questions). A caller that knows better
// passes its own audience to [NewBearer] or the [Issuer].
func ProjectAudience(p serverurl.Project) string {
	u := p.TokenApex()
	if u == nil {
		return ""
	}
	return token.Audience(u.String())
}

// ManagementAudience returns the audience of the tokens the management API
// sends to s: [token.Audience] of [serverurl.Server.Apex], the server's
// configured URL. The Rust client mints management tokens for it on the apex
// client and on an org's client alike, so a request to
// https://alice.atomic.storage still carries a token for
// https://atomic.storage (docs/remote/auth.md#audience,
// docs/remote/endpoints/storage.md#request-headers). It returns "" for a
// Server whose Apex is nil.
func ManagementAudience(s serverurl.Server) string {
	u := s.Apex()
	if u == nil {
		return ""
	}
	return token.Audience(u.String())
}

// The Bearer is Credentials.
var _ Credentials = (*Bearer)(nil)
