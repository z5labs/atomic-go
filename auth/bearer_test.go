package auth_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/auth"
	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/token"
)

// The secret keys of RFC 8032 §7.1, TEST 1 and TEST 2.
const (
	userSeed  = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	agentSeed = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

const (
	projectURL = "http://alice.localhost:8080/workspaces/w/projects/p/code"
	audience   = "http://localhost:8080"
)

// start is the instant every fixed clock starts at.
var start = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

// seeded returns the signer whose seed is the hex seed.
func seeded(t *testing.T, seed string) identity.Signer {
	t.Helper()
	b, err := hex.DecodeString(seed)
	require.NoError(t, err)
	s, err := identity.SignerFromSeed(b)
	require.NoError(t, err)
	return s
}

// clock is a fixed clock that a test can move forward.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: start} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// random returns a source of n token ids, each 16 bytes holding its index, so
// that no two ids it gives are the same.
func random(n int) *bytes.Reader {
	b := make([]byte, 16*n)
	for i := range n {
		binary.BigEndian.PutUint32(b[16*i:], uint32(i+1))
	}
	return bytes.NewReader(b)
}

// fixture is the user, the delegated agent and the certificate the user
// issued it, valid for an hour from start and scoped to audience.
type fixture struct {
	user, agent           identity.Signer
	userID, agentID       identity.Identity
	cert                  delegation.Certificate
	clock                 *clock
	userEntry, agentEntry auth.KeyringEntry
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	user := seeded(t, userSeed)
	agent := seeded(t, agentSeed)
	scope := delegation.NewScope(delegation.PermissionRead, delegation.PermissionPush)
	scope.Servers = []string{audience}
	cert, err := delegation.Issue(context.Background(), user, delegation.Grant{
		DelegatorName: "alice",
		DelegateKey:   agent.PublicKey(),
		DelegateName:  "alice-claude",
		Issued:        start.Add(-time.Hour),
		Expires:       start.Add(time.Hour),
		Scope:         scope,
	})
	require.NoError(t, err)
	userID := identity.Identity{Name: "alice", Type: identity.TypeUser, PublicKey: user.PublicKey()}
	agentID := identity.Identity{Name: "alice-claude", Type: identity.TypeDelegated, PublicKey: agent.PublicKey()}
	return fixture{
		user:       user,
		agent:      agent,
		userID:     userID,
		agentID:    agentID,
		cert:       cert,
		clock:      newClock(),
		userEntry:  auth.KeyringEntry{Identity: userID, Signer: user},
		agentEntry: auth.KeyringEntry{Identity: agentID, Signer: agent},
	}
}

// keyring holds the user and the agent, and certs.
func (f fixture) keyring(t *testing.T, certs []delegation.Certificate, revoked ...delegation.ID) *auth.Keyring {
	t.Helper()
	k, err := auth.NewKeyring(auth.KeyringContents{
		Default:      "alice",
		Entries:      []auth.KeyringEntry{f.userEntry, f.agentEntry},
		Certificates: certs,
		Revoked:      revoked,
	})
	require.NoError(t, err)
	return k
}

// unsent returns a request that is never sent.
func unsent() *http.Request {
	return httptest.NewRequest(http.MethodGet, projectURL+"/changes", nil)
}

// bearerToken parses the token req's Authorization header carries.
func bearerToken(t *testing.T, req *http.Request) token.Token {
	t.Helper()
	h := req.Header.Get("Authorization")
	raw, ok := strings.CutPrefix(h, "Bearer ")
	require.True(t, ok, "Authorization is %q", h)
	tok, err := token.Parse(raw)
	require.NoError(t, err)
	return tok
}

func TestBearerAttachesAUserToken(t *testing.T) {
	f := newFixture(t)
	b, err := auth.NewBearer(f.user, audience, f.clock.Now, random(4))
	require.NoError(t, err)

	req := unsent()
	require.NoError(t, b.Attach(req))

	tok := bearerToken(t, req)
	require.NoError(t, tok.Verify(f.user.PublicKey(), audience, f.clock.Now()))
	require.Equal(t, f.user.PublicKey().String(), tok.Header().Kid)
	claims := tok.Claims()
	require.Equal(t, f.user.PublicKey().String(), claims.Sub)
	require.Equal(t, audience, claims.Aud)
	require.Nil(t, claims.Act)
	require.Empty(t, claims.Dlg)
	require.Empty(t, req.Header.Values(delegation.HeaderName))
}

func TestBearerAttachesADelegatedTokenAndItsCertificate(t *testing.T) {
	f := newFixture(t)
	b, err := auth.NewBearer(f.agent, audience, f.clock.Now, random(4), auth.WithCertificate(f.cert))
	require.NoError(t, err)

	req := unsent()
	require.NoError(t, b.Attach(req))

	tok := bearerToken(t, req)
	require.NoError(t, tok.Verify(f.agent.PublicKey(), audience, f.clock.Now()))
	require.Equal(t, f.agent.PublicKey().String(), tok.Header().Kid)
	claims := tok.Claims()
	require.Equal(t, f.user.PublicKey().String(), claims.Sub)
	require.NotNil(t, claims.Act)
	require.Equal(t, f.agent.PublicKey().String(), claims.Act.Sub)
	require.Equal(t, f.cert.ID().URN(), claims.Dlg)

	got, err := delegation.DecodeHeader(req.Header.Get(delegation.HeaderName))
	require.NoError(t, err)
	require.Equal(t, f.cert.ID(), got.ID())
	require.Equal(t, f.cert.HeaderValue(), got.HeaderValue())
	require.NoError(t, got.Verify(f.user.PublicKey()))
}

func TestBearerMintsAFreshTokenPerAttach(t *testing.T) {
	f := newFixture(t)
	b, err := auth.NewBearer(f.user, audience, f.clock.Now, random(4))
	require.NoError(t, err)

	first, second := unsent(), unsent()
	require.NoError(t, b.Attach(first))
	f.clock.Advance(10 * time.Minute)
	require.NoError(t, b.Attach(second))

	a, c := bearerToken(t, first).Claims(), bearerToken(t, second).Claims()
	require.NotEqual(t, a.Jti, c.Jti)
	require.Equal(t, start.Unix(), a.Iat)
	require.Equal(t, start.Add(10*time.Minute).Unix(), c.Iat)
	// The second token is valid after the first has expired.
	require.NoError(t, bearerToken(t, second).Verify(f.user.PublicKey(), audience, f.clock.Now()))
}

func TestBearerRefusesACertificatePastItsExpiry(t *testing.T) {
	f := newFixture(t)
	b, err := auth.NewBearer(f.agent, audience, f.clock.Now, random(4), auth.WithCertificate(f.cert))
	require.NoError(t, err)

	f.clock.Advance(time.Hour)
	req := unsent()
	require.NoError(t, b.Attach(req), "a certificate is valid at exactly its expiry")

	f.clock.Advance(time.Second)
	req = unsent()
	err = b.Attach(req)
	require.ErrorIs(t, err, delegation.ErrExpired)
	require.Empty(t, req.Header.Values("Authorization"))
	require.Empty(t, req.Header.Values(delegation.HeaderName))

	_, err = b.Token(context.Background())
	require.ErrorIs(t, err, delegation.ErrExpired)
}

func TestBearerTokenHonoursTheContext(t *testing.T) {
	f := newFixture(t)
	b, err := auth.NewBearer(f.user, audience, f.clock.Now, random(4))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = b.Token(ctx)
	require.ErrorIs(t, err, context.Canceled)

	req := unsent().WithContext(ctx)
	require.ErrorIs(t, b.Attach(req), context.Canceled)
	require.Empty(t, req.Header.Values("Authorization"))
}

func TestBearerAttachReplacesExistingHeaders(t *testing.T) {
	f := newFixture(t)
	b, err := auth.NewBearer(f.agent, audience, f.clock.Now, random(4), auth.WithCertificate(f.cert))
	require.NoError(t, err)

	req := unsent()
	req.Header.Add("Authorization", "Bearer stale")
	req.Header.Add(delegation.HeaderName, "stale")
	require.NoError(t, b.Attach(req))
	require.Len(t, req.Header.Values("Authorization"), 1)
	require.Equal(t, []string{f.cert.HeaderValue()}, req.Header.Values(delegation.HeaderName))
}

func TestNewBearerRequiresEveryArgument(t *testing.T) {
	_, err := auth.NewBearer(nil, " / ", nil, nil)
	require.ErrorIs(t, err, auth.ErrInvalidArgument)
	for _, what := range []string{"signer", "audience", "clock", "random source"} {
		require.ErrorContains(t, err, what)
	}
}

func TestNewBearerRefusesACertificate(t *testing.T) {
	f := newFixture(t)
	expired, err := delegation.Issue(context.Background(), f.user, delegation.Grant{
		DelegatorName: "alice",
		DelegateKey:   f.agent.PublicKey(),
		DelegateName:  "alice-claude",
		Issued:        start.Add(-2 * time.Hour),
		Expires:       start.Add(-time.Hour),
		Scope:         delegation.NewScope(delegation.PermissionRead),
	})
	require.NoError(t, err)

	testCases := []struct {
		name   string
		signer identity.Signer
		cert   delegation.Certificate
		is     error
	}{
		{name: "the zero certificate", signer: f.agent, cert: delegation.Certificate{}},
		{name: "a certificate issued to another delegate", signer: f.user, cert: f.cert, is: delegation.ErrWrongDelegate},
		{name: "an expired certificate", signer: f.agent, cert: expired, is: delegation.ErrExpired},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := auth.NewBearer(tc.signer, audience, f.clock.Now, random(1), auth.WithCertificate(tc.cert))
			require.ErrorIs(t, err, auth.ErrInvalidArgument)
			if tc.is != nil {
				require.ErrorIs(t, err, tc.is)
			}
		})
	}
}

func TestNewBearerRefusesATamperedCertificate(t *testing.T) {
	f := newFixture(t)
	doc := f.cert.Document()
	b, err := doc.MarshalJSON()
	require.NoError(t, err)
	tampered, err := delegation.ParseCertificate(bytes.Replace(b, []byte(`"alice-claude"`), []byte(`"mallory"`), 1))
	require.NoError(t, err)

	_, err = auth.NewBearer(f.agent, audience, f.clock.Now, random(1), auth.WithCertificate(tampered))
	require.ErrorIs(t, err, auth.ErrInvalidArgument)
	require.ErrorIs(t, err, delegation.ErrVerify)
}

// attached is what one goroutine's Attach gave.
type attached struct {
	authorization string
	err           error
}

func TestBearerIsSafeForConcurrentUse(t *testing.T) {
	const n = 32
	f := newFixture(t)
	b, err := auth.NewBearer(f.agent, audience, f.clock.Now, random(n), auth.WithCertificate(f.cert))
	require.NoError(t, err)

	results := make(chan attached, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := unsent()
			err := b.Attach(req)
			results <- attached{authorization: req.Header.Get("Authorization"), err: err}
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[string]bool, n)
	jtis := make(map[string]bool, n)
	for r := range results {
		require.NoError(t, r.err)
		require.False(t, seen[r.authorization], "a token was sent twice")
		seen[r.authorization] = true
		raw, ok := strings.CutPrefix(r.authorization, "Bearer ")
		require.True(t, ok)
		tok, err := token.Parse(raw)
		require.NoError(t, err)
		require.NoError(t, tok.Verify(f.agent.PublicKey(), audience, f.clock.Now()))
		jtis[tok.Claims().Jti] = true
	}
	require.Len(t, seen, n)
	require.Len(t, jtis, n)
}

func TestProjectAudience(t *testing.T) {
	p, _ := project(t, projectURL)
	require.Equal(t, "http://localhost:8080", auth.ProjectAudience(p))

	p, _ = project(t, "https://Alice.Atomic.Storage/workspaces/w/projects/p/code")
	require.Equal(t, "https://atomic.storage", auth.ProjectAudience(p))

	require.Empty(t, auth.ProjectAudience(serverurl.Project{}))
}

func TestManagementAudience(t *testing.T) {
	s, err := serverurl.ParseServer("https://Atomic.Storage/", false)
	require.NoError(t, err)
	require.Equal(t, "https://atomic.storage", auth.ManagementAudience(s))

	// An org's client sends its requests to the org's host, and still mints
	// its tokens for the apex.
	org, err := s.OrgBase("alice")
	require.NoError(t, err)
	require.Equal(t, "https://alice.atomic.storage", org.String())
	require.NotEqual(t, token.Audience(org.String()), auth.ManagementAudience(s))

	require.Empty(t, auth.ManagementAudience(serverurl.Server{}))
}
