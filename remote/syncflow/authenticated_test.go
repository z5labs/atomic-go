package syncflow_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/auth"
	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/project/projecttest"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/syncflow"
	"github.com/z5labs/atomic-go/remote/tags"
	"github.com/z5labs/atomic-go/remote/views"
	"github.com/z5labs/atomic-go/token"
)

// authProjectURL is the project the authenticated pushes go to. Its first
// host label, alice, is the identity name auth.ChooseName infers for it.
const authProjectURL = "http://alice.localhost:8080/workspaces/w/projects/p/code"

// authStart is the instant the fixed clocks of the authenticated pushes
// read.
var authStart = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

// ExamplePush wires a push together at a program's edge: an identity from a
// keyring becomes a Bearer, auth.Transport puts it on every request bound for
// the project's origin, project.New builds the client on that transport, and
// syncflow.Push runs the push.
//
// Everything outside the process is a fake. remotetest.Transport serves an
// in-memory projecttest.Server, behind a gate that verifies each request's
// bearer token as a server does, where a program would use an
// *http.Transport dialling through host.IPNetwork.DialTCP. The clock is fixed
// and the token ids come from a seeded generator, so the run is the same
// every time.
func ExamplePush() {
	ctx := context.Background()
	clock := func() time.Time { return authStart }

	// alice's key. A program loads it from wherever it keeps its keys.
	alice, err := identity.SignerFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))
	if err != nil {
		fmt.Println(err)
		return
	}
	keyring, err := auth.NewKeyring(auth.KeyringContents{
		Default: "alice",
		Entries: []auth.KeyringEntry{{
			Identity: identity.Identity{Name: "alice", Type: identity.TypeUser, PublicKey: alice.PublicKey()},
			Signer:   alice,
		}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	proj, _, err := serverurl.ParseProject(authProjectURL)
	if err != nil {
		fmt.Println(err)
		return
	}
	audience := auth.ProjectAudience(proj)

	// The server: an in-memory project that knows alice's registered key.
	srv := projecttest.NewServer()
	gate := newBearerGate(srv, audience, clock, alice.PublicKey())

	// The program's edge: credentials, transport, client.
	issuer, err := auth.NewIssuer(keyring, clock, rand.NewChaCha8([32]byte{}))
	if err != nil {
		fmt.Println(err)
		return
	}
	bearer, err := issuer.ForRemote(ctx, proj, auth.Selection{}, audience)
	if err != nil {
		fmt.Println(err)
		return
	}
	transport, err := auth.NewTransport(remotetest.Transport(gate), bearer, proj.Origin())
	if err != nil {
		fmt.Println(err)
		return
	}
	client, err := project.New(proj, &http.Client{Transport: transport}, slog.New(slog.DiscardHandler), "0.22.1")
	if err != nil {
		fmt.Println(err)
		return
	}

	// Push dev, a shared view logging two changes. A program reads the
	// view and its change files from its own repository.
	files := changeFiles{}
	dev := views.Manifest{Name: "dev", Scope: ident.ScopeShared}
	for _, name := range []string{"c1", "c2"} {
		b := []byte("change file " + name)
		h := ident.Sum(b)
		files[h] = b
		dev.Changes = append(dev.Changes, h)
	}
	dev.State = dev.Fold()
	res, err := syncflow.Push(ctx, client, []syncflow.LocalView{{Manifest: dev}}, files)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pushed %d changes: stored %d objects, moved %d ref\n",
		res.PushedChanges, res.Summary.Stored, res.Summary.RefsMoved)
	accepted := gate.Accepted()
	jtis := make(map[string]bool)
	for _, a := range accepted {
		jtis[a.Token.Claims().Jti] = true
	}
	fmt.Printf("%d requests authenticated, %d token ids, %d refused\n",
		len(accepted), len(jtis), len(gate.Refused()))
	// Output:
	// pushed 2 changes: stored 3 objects, moved 1 ref
	// 3 requests authenticated, 3 token ids, 0 refused
}

func TestAuthenticatedPush(t *testing.T) {
	user := seededSigner(t, 1)
	agent := seededSigner(t, 2)
	userID := identity.Identity{Name: "alice", Type: identity.TypeUser, PublicKey: user.PublicKey()}
	agentID := identity.Identity{Name: "alice-claude", Type: identity.TypeDelegated, PublicKey: agent.PublicKey()}

	proj, _, err := serverurl.ParseProject(authProjectURL)
	require.NoError(t, err)
	audience := auth.ProjectAudience(proj)

	scope := delegation.NewScope(delegation.PermissionRead, delegation.PermissionPush)
	scope.Servers = []string{audience}
	cert, err := delegation.Issue(t.Context(), user, delegation.Grant{
		DelegatorName: "alice",
		DelegateKey:   agent.PublicKey(),
		DelegateName:  "alice-claude",
		Issued:        authStart.Add(-time.Hour),
		Expires:       authStart.Add(time.Hour),
		Scope:         scope,
	})
	require.NoError(t, err)

	keyring, err := auth.NewKeyring(auth.KeyringContents{
		Default: "alice",
		Entries: []auth.KeyringEntry{
			{Identity: userID, Signer: user},
			{Identity: agentID, Signer: agent},
		},
		Certificates: []delegation.Certificate{cert},
	})
	require.NoError(t, err)

	testCases := []struct {
		name string
		sel  auth.Selection
		// signer is the key that signs every token, and subject the one
		// every token is for.
		signer, subject identity.PublicKey
		delegated       bool
	}{
		{
			name:    "user inferred from the host",
			sel:     auth.Selection{},
			signer:  user.PublicKey(),
			subject: user.PublicKey(),
		},
		{
			name:      "delegated agent",
			sel:       auth.Selection{AgentIdentity: "alice-claude"},
			signer:    agent.PublicKey(),
			subject:   user.PublicKey(),
			delegated: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clock := func() time.Time { return authStart }
			srv := projecttest.NewServer()
			gate := newBearerGate(srv, audience, clock, user.PublicKey(), agent.PublicKey())

			issuer, err := auth.NewIssuer(keyring, clock, rand.NewChaCha8([32]byte{7}))
			require.NoError(t, err)
			bearer, err := issuer.ForRemote(t.Context(), proj, tc.sel, audience)
			require.NoError(t, err)
			c := authenticatedClient(t, proj, gate, bearer)

			res, err := syncflow.Push(t.Context(), c, devChain(), sourceOf("c1", "c2"))
			require.NoError(t, err)
			require.Equal(t, 2, res.PushedChanges)
			_, ok := srv.Ref("dev")
			require.True(t, ok)

			// Every request reached the server through the gate, and the
			// gate refused none of them.
			require.Empty(t, gate.Refused())
			accepted := gate.Accepted()
			require.Equal(t, []request{getCode, postCode, getCode}, requests(srv))
			require.Len(t, accepted, len(srv.Requests()))

			jtis := make(map[string]bool)
			for _, a := range accepted {
				claims := a.Token.Claims()
				require.NoError(t, a.Token.Verify(tc.signer, audience, authStart))
				require.Equal(t, tc.subject.String(), claims.Sub)
				require.False(t, jtis[claims.Jti], "token id %s sent twice", claims.Jti)
				jtis[claims.Jti] = true

				if !tc.delegated {
					require.Nil(t, claims.Act)
					require.Empty(t, a.Delegation)
					continue
				}
				require.NotNil(t, claims.Act)
				require.Equal(t, tc.signer.String(), claims.Act.Sub)
				decoded, err := delegation.DecodeHeader(a.Delegation)
				require.NoError(t, err)
				require.Equal(t, cert.ID(), decoded.ID())
				require.Equal(t, cert.HeaderValue(), decoded.HeaderValue())
				require.Equal(t, cert.ID().URN(), claims.Dlg)
			}
		})
	}

	t.Run("anonymous is refused", func(t *testing.T) {
		clock := func() time.Time { return authStart }
		srv := projecttest.NewServer()
		gate := newBearerGate(srv, audience, clock, user.PublicKey(), agent.PublicKey())
		c := authenticatedClient(t, proj, gate, auth.NoCredentials{})

		_, err := syncflow.Push(t.Context(), c, devChain(), sourceOf("c1", "c2"))
		var se *syncflow.StageError
		require.ErrorAs(t, err, &se)
		require.Equal(t, syncflow.StageAdvertise, se.Stage)
		require.ErrorIs(t, err, remote.ErrUnauthorized)
		require.Empty(t, srv.Requests())
		require.Empty(t, gate.Accepted())
		require.Len(t, gate.Refused(), 1)
	})

	t.Run("a key the server has not registered is refused", func(t *testing.T) {
		clock := func() time.Time { return authStart }
		srv := projecttest.NewServer()
		gate := newBearerGate(srv, audience, clock, agent.PublicKey())

		issuer, err := auth.NewIssuer(keyring, clock, rand.NewChaCha8([32]byte{7}))
		require.NoError(t, err)
		bearer, err := issuer.ForRemote(t.Context(), proj, auth.Selection{}, audience)
		require.NoError(t, err)
		c := authenticatedClient(t, proj, gate, bearer)

		_, err = syncflow.Push(t.Context(), c, devChain(), sourceOf("c1", "c2"))
		require.ErrorIs(t, err, remote.ErrUnauthorized)
		require.Empty(t, srv.Requests())
		refused := gate.Refused()
		require.Len(t, refused, 1)
		require.ErrorContains(t, refused[0], "kid "+user.PublicKey().String()+" is not a registered key")
	})
}

// authenticatedClient returns a project client for proj whose requests go
// through an auth.Transport attaching creds, to gate in memory.
func authenticatedClient(t *testing.T, proj serverurl.Project, gate http.Handler, creds auth.Credentials) *project.Client {
	t.Helper()
	tr, err := auth.NewTransport(remotetest.Transport(gate), creds, proj.Origin())
	require.NoError(t, err)
	c, err := project.New(proj, &http.Client{Transport: tr}, remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)
	return c
}

// seededSigner returns the signer whose seed is 32 bytes of b.
func seededSigner(t *testing.T, b byte) identity.Signer {
	t.Helper()
	s, err := identity.SignerFromSeed(bytes.Repeat([]byte{b}, ed25519.SeedSize))
	require.NoError(t, err)
	return s
}

// changeFiles is a syncflow.PackSource holding change files by hash, and no
// sidecars or tags.
type changeFiles map[ident.Hash][]byte

func (f changeFiles) Change(_ context.Context, h ident.Hash) ([]byte, error) {
	b, ok := f[h]
	if !ok {
		return nil, fmt.Errorf("no change %s", h)
	}
	return b, nil
}

func (changeFiles) SidecarsCovering(context.Context, ident.Hash) ([]syncflow.Sidecar, error) {
	return nil, nil
}

func (changeFiles) Tags(context.Context, string) ([]tags.Record, error) {
	return nil, nil
}

// bearerGate is the server's side of authentication, in front of the
// handler it guards. It passes a request on only when its bearer token
// verifies, with token.Verify, against a registered key for the gate's
// audience at the gate's clock, as docs/remote/auth.md describes under
// What the server is expected to check. A delegated token must also come
// with the Atomic-Delegation certificate it names, verified against the
// delegator's registered key, issued to the token's signer, valid at the
// gate's clock, and scoped to allow the request on the gate's audience:
// read for a GET or HEAD, push for anything else. The scope's workspace,
// project and view lists are not checked. Any other request is answered 401
// and goes no further.
type bearerGate struct {
	next     http.Handler
	audience string
	clock    func() time.Time

	// keys are the registered keys, by their base32 text, the form a
	// token's kid and sub take.
	keys map[string]identity.PublicKey

	mu       sync.Mutex
	accepted []acceptedRequest
	refused  []error
}

// acceptedRequest is what a bearerGate saw on a request it passed on.
type acceptedRequest struct {
	// Token is the request's bearer token.
	Token token.Token

	// Delegation is the request's Atomic-Delegation header, or "" when it
	// had none.
	Delegation string
}

// newBearerGate returns a bearerGate in front of next that knows the
// registered keys.
func newBearerGate(next http.Handler, audience string, clock func() time.Time, registered ...identity.PublicKey) *bearerGate {
	keys := make(map[string]identity.PublicKey, len(registered))
	for _, k := range registered {
		keys[k.String()] = k
	}
	return &bearerGate{next: next, audience: audience, clock: clock, keys: keys}
}

// ServeHTTP checks r's credentials and serves it with the guarded handler
// when they hold, or answers 401.
func (g *bearerGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a, err := g.check(r)
	g.mu.Lock()
	if err != nil {
		g.refused = append(g.refused, err)
	} else {
		g.accepted = append(g.accepted, a)
	}
	g.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	g.next.ServeHTTP(w, r)
}

// check verifies r's credentials, as bearerGate describes.
func (g *bearerGate) check(r *http.Request) (acceptedRequest, error) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return acceptedRequest{}, errors.New("no bearer token")
	}
	tok, err := token.Parse(raw)
	if err != nil {
		return acceptedRequest{}, err
	}
	signer, ok := g.keys[tok.Header().Kid]
	if !ok {
		return acceptedRequest{}, fmt.Errorf("kid %s is not a registered key", tok.Header().Kid)
	}
	now := g.clock()
	if err := tok.Verify(signer, g.audience, now); err != nil {
		return acceptedRequest{}, err
	}
	a := acceptedRequest{Token: tok, Delegation: r.Header.Get(delegation.HeaderName)}
	claims := tok.Claims()
	if claims.Act == nil {
		return a, nil
	}

	delegator, ok := g.keys[claims.Sub]
	if !ok {
		return acceptedRequest{}, fmt.Errorf("delegator %s is not a registered key", claims.Sub)
	}
	cert, err := delegation.DecodeHeader(a.Delegation)
	if err != nil {
		return acceptedRequest{}, fmt.Errorf("reading the %s header: %w", delegation.HeaderName, err)
	}
	if err := cert.Verify(delegator); err != nil {
		return acceptedRequest{}, fmt.Errorf("the certificate does not verify against the delegator: %w", err)
	}
	if cert.ID().URN() != claims.Dlg {
		return acceptedRequest{}, fmt.Errorf("the token names certificate %s, not %s", claims.Dlg, cert.ID().URN())
	}
	if !cert.AgentDelegation().DelegateKey.Matches(signer) {
		return acceptedRequest{}, fmt.Errorf("certificate %s is not issued to %s", cert.ID(), signer)
	}
	if cert.Expired(now) {
		return acceptedRequest{}, fmt.Errorf("certificate %s has expired", cert.ID())
	}
	if issued := cert.AgentDelegation().Issued; now.Before(issued) {
		return acceptedRequest{}, fmt.Errorf("certificate %s is not valid until %s", cert.ID(), issued.Format(time.RFC3339))
	}
	perm := permissionFor(r.Method)
	if !cert.Allows(perm, delegation.ResourceRef{Server: g.audience}, now) {
		return acceptedRequest{}, fmt.Errorf("certificate %s does not allow %s on %s", cert.ID(), perm, g.audience)
	}
	return a, nil
}

// permissionFor returns the permission a delegated request with method
// needs: read for a GET or HEAD, which advertise and pull, and push for
// anything else.
func permissionFor(method string) delegation.Permission {
	if method == http.MethodGet || method == http.MethodHead {
		return delegation.PermissionRead
	}
	return delegation.PermissionPush
}

// Accepted returns the requests the gate passed on, in the order it saw
// them.
func (g *bearerGate) Accepted() []acceptedRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]acceptedRequest(nil), g.accepted...)
}

// Refused returns why the gate refused each request it refused, in order.
func (g *bearerGate) Refused() []error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]error(nil), g.refused...)
}

// TestBearerGateRefusesBadDelegations sends delegated requests straight to a
// bearerGate, each broken in one way, so that every check the gate makes on a
// delegation is shown to refuse.
func TestBearerGateRefusesBadDelegations(t *testing.T) {
	user := seededSigner(t, 1)
	agent := seededSigner(t, 2)
	mallory := seededSigner(t, 3)
	other := seededSigner(t, 4)

	proj, _, err := serverurl.ParseProject(authProjectURL)
	require.NoError(t, err)
	audience := auth.ProjectAudience(proj)

	// issue returns delegator's certificate for delegate, valid from issued
	// to expires on servers, granting perms.
	issue := func(t *testing.T, delegator identity.Signer, delegate identity.PublicKey, issued, expires time.Time, servers []string, perms ...delegation.Permission) delegation.Certificate {
		t.Helper()
		scope := delegation.NewScope(perms...)
		scope.Servers = servers
		c, err := delegation.Issue(t.Context(), delegator, delegation.Grant{
			DelegatorName: "alice",
			DelegateKey:   delegate,
			DelegateName:  "alice-claude",
			Issued:        issued,
			Expires:       expires,
			Scope:         scope,
		})
		require.NoError(t, err)
		return c
	}
	here := []string{audience}
	readPush := []delegation.Permission{delegation.PermissionRead, delegation.PermissionPush}
	cert := issue(t, user, agent.PublicKey(), authStart.Add(-time.Hour), authStart.Add(time.Hour), here, readPush...)

	// signed returns a method request to the project carrying the token
	// and certificate of agent's Bearer for c, minted at authStart.
	signed := func(t *testing.T, method string, c delegation.Certificate) *http.Request {
		t.Helper()
		b, err := auth.NewBearer(agent, audience, func() time.Time { return authStart }, rand.NewChaCha8([32]byte{9}), auth.WithCertificate(c))
		require.NoError(t, err)
		req := httptest.NewRequest(method, authProjectURL, nil)
		require.NoError(t, b.Attach(req))
		return req
	}

	testCases := []struct {
		name string
		// request builds the request to send.
		request func(t *testing.T) *http.Request
		// registered are the keys the gate knows; nil means user and
		// agent. now is the gate's clock; zero means authStart.
		registered []identity.PublicKey
		now        time.Time
		// refusal is a substring of the gate's error, or "" for a
		// request the gate must pass on.
		refusal string
	}{
		{
			name:    "a valid delegation passes",
			request: func(t *testing.T) *http.Request { return signed(t, http.MethodPost, cert) },
		},
		{
			name:       "the delegator is not registered",
			request:    func(t *testing.T) *http.Request { return signed(t, http.MethodGet, cert) },
			registered: []identity.PublicKey{agent.PublicKey()},
			refusal:    "delegator " + user.PublicKey().String() + " is not a registered key",
		},
		{
			name: "no certificate",
			request: func(t *testing.T) *http.Request {
				req := signed(t, http.MethodGet, cert)
				req.Header.Del(delegation.HeaderName)
				return req
			},
			refusal: "reading the Atomic-Delegation header",
		},
		{
			name: "a garbled certificate",
			request: func(t *testing.T) *http.Request {
				req := signed(t, http.MethodGet, cert)
				req.Header.Set(delegation.HeaderName, "not+base64url/")
				return req
			},
			refusal: "reading the Atomic-Delegation header",
		},
		{
			name: "a certificate the delegator did not sign",
			request: func(t *testing.T) *http.Request {
				req := signed(t, http.MethodGet, cert)
				forged := issue(t, mallory, agent.PublicKey(), authStart.Add(-time.Hour), authStart.Add(time.Hour), here, readPush...)
				req.Header.Set(delegation.HeaderName, forged.HeaderValue())
				return req
			},
			refusal: "does not verify against the delegator",
		},
		{
			name: "the token names another certificate",
			request: func(t *testing.T) *http.Request {
				req := signed(t, http.MethodGet, cert)
				another := issue(t, user, agent.PublicKey(), authStart.Add(-2*time.Hour), authStart.Add(time.Hour), here, readPush...)
				req.Header.Set(delegation.HeaderName, another.HeaderValue())
				return req
			},
			refusal: "the token names certificate " + cert.ID().URN(),
		},
		{
			name: "a certificate issued to another agent",
			request: func(t *testing.T) *http.Request {
				// auth.NewBearer refuses this pairing, so the token is
				// signed by hand: agent's token, naming a certificate
				// user issued to other.
				theirs := issue(t, user, other.PublicKey(), authStart.Add(-time.Hour), authStart.Add(time.Hour), here, readPush...)
				id, err := token.NewID(rand.NewChaCha8([32]byte{10}))
				require.NoError(t, err)
				tok, err := token.Sign(t.Context(), agent, token.Params{
					Audience:   audience,
					IssuedAt:   authStart,
					ID:         id,
					OnBehalfOf: &token.OnBehalfOf{Delegator: user.PublicKey(), Delegation: theirs.ID().URN()},
				})
				require.NoError(t, err)
				req := httptest.NewRequest(http.MethodGet, authProjectURL, nil)
				req.Header.Set("Authorization", "Bearer "+tok.String())
				req.Header.Set(delegation.HeaderName, theirs.HeaderValue())
				return req
			},
			refusal: "is not issued to " + agent.PublicKey().String(),
		},
		{
			name: "an expired certificate",
			request: func(t *testing.T) *http.Request {
				brief := issue(t, user, agent.PublicKey(), authStart.Add(-time.Hour), authStart.Add(30*time.Second), here, readPush...)
				return signed(t, http.MethodGet, brief)
			},
			// The token, minted at authStart, is still within its
			// lifetime; only the certificate has run out.
			now:     authStart.Add(time.Minute),
			refusal: "has expired",
		},
		{
			name: "a certificate not yet issued",
			request: func(t *testing.T) *http.Request {
				early := issue(t, user, agent.PublicKey(), authStart.Add(time.Hour), authStart.Add(2*time.Hour), here, readPush...)
				return signed(t, http.MethodGet, early)
			},
			refusal: "is not valid until",
		},
		{
			name: "a read-only certificate pushing",
			request: func(t *testing.T) *http.Request {
				readOnly := issue(t, user, agent.PublicKey(), authStart.Add(-time.Hour), authStart.Add(time.Hour), here, delegation.PermissionRead)
				return signed(t, http.MethodPost, readOnly)
			},
			refusal: "does not allow push",
		},
		{
			name: "a certificate for another server",
			request: func(t *testing.T) *http.Request {
				elsewhere := issue(t, user, agent.PublicKey(), authStart.Add(-time.Hour), authStart.Add(time.Hour), []string{"https://atomic.storage"}, readPush...)
				return signed(t, http.MethodGet, elsewhere)
			},
			refusal: "does not allow read",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			registered := tc.registered
			if registered == nil {
				registered = []identity.PublicKey{user.PublicKey(), agent.PublicKey()}
			}
			now := tc.now
			if now.IsZero() {
				now = authStart
			}
			served := 0
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				served++
				w.WriteHeader(http.StatusNoContent)
			})
			gate := newBearerGate(next, audience, func() time.Time { return now }, registered...)

			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, tc.request(t))

			if tc.refusal == "" {
				require.Empty(t, gate.Refused())
				require.Equal(t, http.StatusNoContent, rec.Code)
				require.Equal(t, 1, served)
				require.Len(t, gate.Accepted(), 1)
				return
			}
			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.Zero(t, served)
			require.Empty(t, gate.Accepted())
			refused := gate.Refused()
			require.Len(t, refused, 1)
			require.ErrorContains(t, refused[0], tc.refusal)
		})
	}
}
