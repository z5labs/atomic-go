package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/token"
)

// Issuer turns the identities in a [Store] into [Bearer] credentials, as the
// Rust client's attach_identity does (docs/remote/auth.md#attach_identity),
// except that it fails closed: every problem is reported before anything
// reaches the network, rather than leaving a request to go out without
// credentials. [Issuer.ForRemoteOrAnonymous] is the Rust client's fallback,
// for a caller that wants public reads to keep working.
//
// Build one with [NewIssuer]; the zero Issuer is not usable. An Issuer is
// safe for concurrent use as long as its store and clock are. Every Bearer it
// returns reads the Issuer's one random source, and the Issuer serializes
// those reads itself, so the Bearers may mint at once even when the source,
// such as a [bytes.Reader], is not safe for concurrent use.
type Issuer struct {
	store  Store
	clock  func() time.Time
	random *lockedReader
}

// lockedReader serializes the reads of the reader it wraps.
type lockedReader struct {
	mu sync.Mutex
	r  io.Reader
}

func (l *lockedReader) Read(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Read(p)
}

// NewIssuer returns an [Issuer] that reads identities, keys, certificates and
// revocations from store, and whose Bearers read the time from clock and
// their token ids from random.
//
// All three are required, and nothing falls back to a default. NewIssuer
// names every one it refused in one error made with [errors.Join], each
// wrapping [ErrInvalidArgument].
func NewIssuer(store Store, clock func() time.Time, random io.Reader) (*Issuer, error) {
	var errs []error
	missing := func(what string) {
		errs = append(errs, fmt.Errorf("%w: %s is required", ErrInvalidArgument, what))
	}
	if store == nil {
		missing("the store")
	}
	if clock == nil {
		missing("the clock")
	}
	if random == nil {
		missing("the random source")
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("auth: NewIssuer: %w", errors.Join(errs...))
	}
	return &Issuer{store: store, clock: clock, random: &lockedReader{r: random}}, nil
}

// IssueOption configures one call to an [Issuer]'s methods.
type IssueOption func(*issueConfig)

type issueConfig struct {
	explicit string
}

// WithExplicitDelegation hands a delegated identity the Atomic-Delegation
// header value header, as the Rust client reads one from the
// [EnvDelegation] variable. When header is not blank, it is the only
// certificate considered, as [delegation.Choose] describes; the store's
// certificates are not used even when it is unusable. It is ignored for an
// identity that is not delegated.
func WithExplicitDelegation(header string) IssueOption {
	return func(cfg *issueConfig) {
		cfg.explicit = header
	}
}

// ForIdentity returns the credentials of id, an identity in the Issuer's
// store, for audience, the server URL its tokens are for.
//
// It loads id's signer from the store, which must hold id's key. For a
// delegated identity ([identity.Type.IsDelegated]) it then chooses the
// certificate to present with [delegation.Choose], with the store's
// certificates for id, the explicit header from [WithExplicitDelegation],
// audience as the server the certificate must allow reads on, the clock's
// current instant, and the store's revocations; a stored certificate whose
// revocation cannot be read is treated as revoked. Any other identity, a
// standalone agent included, signs its own tokens and sends no certificate.
//
// An audience that [token.Audience] makes empty is an error wrapping
// [ErrInvalidArgument], returned before the store is read. A key that will
// not load is an error from the store's [KeyLoader], or one wrapping
// [ErrKeyUnavailable]. A delegated identity with no usable
// certificate is an error wrapping the *[delegation.SelectionError] that says
// why, and so [delegation.ErrNoUsableCertificate]. An error from the store is
// returned wrapped.
func (i *Issuer) ForIdentity(ctx context.Context, id identity.Identity, audience string, opts ...IssueOption) (*Bearer, error) {
	if err := checkAudience(audience); err != nil {
		return nil, fmt.Errorf("auth: ForIdentity: %w", err)
	}
	s, err := loadSigner(ctx, i.store, id)
	if err != nil {
		return nil, fmt.Errorf("auth: ForIdentity: loading the key of identity %q: %w", id.Name, err)
	}
	b, err := i.bearer(ctx, id, s, audience, opts)
	if err != nil {
		return nil, fmt.Errorf("auth: ForIdentity: %w", err)
	}
	return b, nil
}

// ForRemote returns the credentials to send to p as, for audience: the
// identity [Resolve] resolves for p and sel, with the credentials
// [Issuer.ForIdentity] gives it.
//
// It fails closed. An audience that [token.Audience] makes empty is an error
// wrapping [ErrInvalidArgument], returned before the store is read. When no
// identity resolves or its key will not load, it
// fails with the [*AuthenticationError] that [CheckPushCredentials] would
// return; a delegated identity with no usable certificate fails as in
// ForIdentity. Nothing is sent either way: the caller decides whether to go
// on without credentials, which [Issuer.ForRemoteOrAnonymous] does.
func (i *Issuer) ForRemote(ctx context.Context, p serverurl.Project, sel Selection, audience string, opts ...IssueOption) (*Bearer, error) {
	if err := checkAudience(audience); err != nil {
		return nil, fmt.Errorf("auth: ForRemote: %w", err)
	}
	id, s, err := resolveSigner(ctx, i.store, p, sel)
	var authErr *AuthenticationError
	switch {
	case errors.As(err, &authErr):
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("auth: ForRemote: %w", err)
	}
	b, err := i.bearer(ctx, id, s, audience, opts)
	if err != nil {
		return nil, fmt.Errorf("auth: ForRemote: identity %q: %w", id.Name, err)
	}
	return b, nil
}

// ForRemoteOrAnonymous is [Issuer.ForRemote] with the Rust client's
// fallback (docs/remote/auth.md#attach_identity): when ForRemote fails, it
// logs one Warn record to log, naming the remote and the error, and returns
// [NoCredentials], so the requests go out anonymously and public reads still
// work. Otherwise it returns ForRemote's [*Bearer] and logs nothing.
//
// Like the Rust client, it falls back whatever the failure, a store that
// cannot be read included, since an anonymous read of a public project needs
// none of it. The two failures that are the caller's own are not fallen back
// from, because it has no error to return them in: a nil log, since the
// fallback is never silent, and an audience that [token.Audience] makes
// empty, such as [ProjectAudience] of the zero Project. Both panic.
func (i *Issuer) ForRemoteOrAnonymous(ctx context.Context, p serverurl.Project, sel Selection, audience string, log *slog.Logger, opts ...IssueOption) Credentials {
	if log == nil {
		panic("auth: ForRemoteOrAnonymous: the logger is required")
	}
	if err := checkAudience(audience); err != nil {
		panic("auth: ForRemoteOrAnonymous: " + err.Error())
	}
	b, err := i.ForRemote(ctx, p, sel, audience, opts...)
	if err != nil {
		log.WarnContext(ctx, "auth: sending requests to the remote without credentials",
			slog.String("remote", p.String()),
			slog.String("audience", audience),
			slog.Any("error", err))
		return NoCredentials{}
	}
	return b
}

// checkAudience refuses an audience that [token.Audience] makes empty, which
// [delegation.Choose] would read as "any server".
func checkAudience(audience string) error {
	if token.Audience(audience) == "" {
		return fmt.Errorf("%w: the audience is required", ErrInvalidArgument)
	}
	return nil
}

// bearer builds id's Bearer from its signer s, choosing a certificate when
// id is delegated.
func (i *Issuer) bearer(ctx context.Context, id identity.Identity, s identity.Signer, audience string, opts []IssueOption) (*Bearer, error) {
	var cfg issueConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if !id.Type.IsDelegated() {
		return NewBearer(s, audience, i.clock, i.random)
	}
	cert, err := i.choose(ctx, id, audience, cfg.explicit)
	if err != nil {
		return nil, err
	}
	return NewBearer(s, audience, i.clock, i.random, WithCertificate(cert))
}

// choose picks the certificate id presents to audience.
func (i *Issuer) choose(ctx context.Context, id identity.Identity, audience, explicit string) (delegation.Certificate, error) {
	agent := id.PublicKey.ID()
	stored, err := i.store.Certificates(ctx, agent)
	if err != nil {
		return delegation.Certificate{}, fmt.Errorf("reading the certificates of identity %q: %w", id.Name, err)
	}
	// Choose asks about revocation through a func that cannot fail, so the
	// store is read first. A certificate whose revocation was not read is
	// treated as revoked.
	known := make(map[delegation.ID]bool, len(stored))
	for _, c := range stored {
		r, err := i.store.Revoked(ctx, c.ID())
		if err != nil {
			return delegation.Certificate{}, fmt.Errorf("reading the revocation of certificate %s: %w", c.ID(), err)
		}
		known[c.ID()] = r
	}
	revoked := func(id delegation.ID) bool {
		r, ok := known[id]
		return r || !ok
	}
	cert, err := delegation.Choose(delegation.Candidates{
		Agent:    agent,
		Server:   audience,
		Explicit: explicit,
		Stored:   stored,
	}, i.clock(), revoked)
	if err != nil {
		return delegation.Certificate{}, fmt.Errorf("choosing a delegation certificate for identity %q: %w", id.Name, err)
	}
	return cert, nil
}
