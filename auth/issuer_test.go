package auth_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/auth"
	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/remote/remotetest"
)

// issuer returns an Issuer over store with f's clock.
func (f fixture) issuer(t *testing.T, store auth.Store) *auth.Issuer {
	t.Helper()
	i, err := auth.NewIssuer(store, f.clock.Now, random(8))
	require.NoError(t, err)
	return i
}

func TestNewIssuerRequiresEveryArgument(t *testing.T) {
	_, err := auth.NewIssuer(nil, nil, nil)
	require.ErrorIs(t, err, auth.ErrInvalidArgument)
	for _, what := range []string{"store", "clock", "random source"} {
		require.ErrorContains(t, err, what)
	}
}

func TestIssuerForIdentityUser(t *testing.T) {
	f := newFixture(t)
	i := f.issuer(t, f.keyring(t, []delegation.Certificate{f.cert}))

	b, err := i.ForIdentity(context.Background(), f.userID, audience)
	require.NoError(t, err)

	req := unsent()
	require.NoError(t, b.Attach(req))
	tok := bearerToken(t, req)
	require.NoError(t, tok.Verify(f.user.PublicKey(), audience, f.clock.Now()))
	require.Nil(t, tok.Claims().Act)
	require.Empty(t, req.Header.Values(delegation.HeaderName))
}

func TestIssuerForIdentityDelegated(t *testing.T) {
	f := newFixture(t)
	i := f.issuer(t, f.keyring(t, []delegation.Certificate{f.cert}))

	b, err := i.ForIdentity(context.Background(), f.agentID, audience)
	require.NoError(t, err)

	req := unsent()
	require.NoError(t, b.Attach(req))
	tok := bearerToken(t, req)
	require.NoError(t, tok.Verify(f.agent.PublicKey(), audience, f.clock.Now()))
	require.Equal(t, f.user.PublicKey().String(), tok.Claims().Sub)
	require.Equal(t, f.cert.ID().URN(), tok.Claims().Dlg)
	require.Equal(t, f.cert.HeaderValue(), req.Header.Get(delegation.HeaderName))
}

func TestIssuerForIdentityChoosesForTheAudience(t *testing.T) {
	f := newFixture(t)
	i := f.issuer(t, f.keyring(t, []delegation.Certificate{f.cert}))

	// The certificate is scoped to audience alone.
	_, err := i.ForIdentity(context.Background(), f.agentID, "https://atomic.storage")
	require.ErrorIs(t, err, delegation.ErrNoUsableCertificate)
	var sel *delegation.SelectionError
	require.ErrorAs(t, err, &sel)
	require.Equal(t, delegation.NotValidForServer, sel.Reason)
}

func TestIssuerForIdentityHonoursRevocation(t *testing.T) {
	f := newFixture(t)
	i := f.issuer(t, f.keyring(t, []delegation.Certificate{f.cert}, f.cert.ID()))

	_, err := i.ForIdentity(context.Background(), f.agentID, audience)
	require.ErrorIs(t, err, delegation.ErrNoUsableCertificate)
	var sel *delegation.SelectionError
	require.ErrorAs(t, err, &sel)
	require.Equal(t, delegation.NewestRevoked, sel.Reason)
}

// failingRevocations is a Store whose revocation list cannot be read.
type failingRevocations struct {
	*auth.Keyring
	err error
}

func (s failingRevocations) Revoked(context.Context, delegation.ID) (bool, error) {
	return false, s.err
}

func TestIssuerForIdentityFailsWhenRevocationCannotBeRead(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("boom")
	i := f.issuer(t, failingRevocations{Keyring: f.keyring(t, []delegation.Certificate{f.cert}), err: boom})

	_, err := i.ForIdentity(context.Background(), f.agentID, audience)
	require.ErrorIs(t, err, boom)
}

func TestIssuerForIdentityUsesTheExplicitDelegation(t *testing.T) {
	f := newFixture(t)
	i := f.issuer(t, f.keyring(t, nil))

	_, err := i.ForIdentity(context.Background(), f.agentID, audience)
	require.ErrorIs(t, err, delegation.ErrNoUsableCertificate)

	b, err := i.ForIdentity(context.Background(), f.agentID, audience, auth.WithExplicitDelegation(f.cert.HeaderValue()))
	require.NoError(t, err)
	req := unsent()
	require.NoError(t, b.Attach(req))
	require.Equal(t, f.cert.HeaderValue(), req.Header.Get(delegation.HeaderName))

	// An explicit certificate is the only one considered.
	i = f.issuer(t, f.keyring(t, []delegation.Certificate{f.cert}))
	_, err = i.ForIdentity(context.Background(), f.agentID, audience, auth.WithExplicitDelegation("not a certificate"))
	require.ErrorIs(t, err, delegation.ErrNoUsableCertificate)
	var sel *delegation.SelectionError
	require.ErrorAs(t, err, &sel)
	require.Equal(t, delegation.ExplicitUnusable, sel.Reason)
}

func TestIssuerForIdentityIgnoresTheExplicitDelegationForAUser(t *testing.T) {
	f := newFixture(t)
	i := f.issuer(t, f.keyring(t, nil))

	b, err := i.ForIdentity(context.Background(), f.userID, audience, auth.WithExplicitDelegation(f.cert.HeaderValue()))
	require.NoError(t, err)
	req := unsent()
	require.NoError(t, b.Attach(req))
	require.Empty(t, req.Header.Values(delegation.HeaderName))
}

func TestIssuerForIdentityFailsWithoutAKey(t *testing.T) {
	f := newFixture(t)
	k, err := auth.NewKeyring(auth.KeyringContents{
		Entries: []auth.KeyringEntry{{Identity: f.userID}},
	})
	require.NoError(t, err)
	i := f.issuer(t, k)

	_, err = i.ForIdentity(context.Background(), f.userID, audience)
	require.ErrorIs(t, err, auth.ErrKeyUnavailable)
}

func TestIssuerForRemote(t *testing.T) {
	f := newFixture(t)
	i := f.issuer(t, f.keyring(t, []delegation.Certificate{f.cert}))
	p, _ := project(t, projectURL)

	b, err := i.ForRemote(context.Background(), p, auth.Selection{Override: "alice-claude"}, auth.ProjectAudience(p))
	require.NoError(t, err)
	req := unsent()
	require.NoError(t, b.Attach(req))
	tok := bearerToken(t, req)
	require.NoError(t, tok.Verify(f.agent.PublicKey(), audience, f.clock.Now()))
	require.Equal(t, f.cert.ID().URN(), tok.Claims().Dlg)

	// With no name chosen, the default identity is used.
	b, err = i.ForRemote(context.Background(), p, auth.Selection{}, auth.ProjectAudience(p))
	require.NoError(t, err)
	req = unsent()
	require.NoError(t, b.Attach(req))
	require.NoError(t, bearerToken(t, req).Verify(f.user.PublicKey(), audience, f.clock.Now()))
}

func TestIssuerForRemoteFailsClosed(t *testing.T) {
	f := newFixture(t)
	p, _ := project(t, projectURL)

	t.Run("no certificate", func(t *testing.T) {
		i := f.issuer(t, f.keyring(t, nil))
		_, err := i.ForRemote(context.Background(), p, auth.Selection{Override: "alice-claude"}, audience)
		require.ErrorIs(t, err, delegation.ErrNoUsableCertificate)
		var sel *delegation.SelectionError
		require.ErrorAs(t, err, &sel)
		require.Equal(t, delegation.NoCertificates, sel.Reason)
	})

	t.Run("an explicit identity that is missing", func(t *testing.T) {
		i := f.issuer(t, f.keyring(t, nil))
		_, err := i.ForRemote(context.Background(), p, auth.Selection{Override: "bob"}, audience)
		var authErr *auth.AuthenticationError
		require.ErrorAs(t, err, &authErr)
		require.Equal(t, auth.ExplicitIdentityNotFound, authErr.Issue)
	})

	t.Run("no identity", func(t *testing.T) {
		k, err := auth.NewKeyring(auth.KeyringContents{})
		require.NoError(t, err)
		i := f.issuer(t, k)
		_, err = i.ForRemote(context.Background(), p, auth.Selection{}, audience)
		var authErr *auth.AuthenticationError
		require.ErrorAs(t, err, &authErr)
		require.Equal(t, auth.NoIdentity, authErr.Issue)
	})

	t.Run("a key that will not load", func(t *testing.T) {
		k, err := auth.NewKeyring(auth.KeyringContents{
			Default: "alice",
			Entries: []auth.KeyringEntry{{Identity: f.userID}},
		})
		require.NoError(t, err)
		i := f.issuer(t, k)
		_, err = i.ForRemote(context.Background(), p, auth.Selection{}, audience)
		var authErr *auth.AuthenticationError
		require.ErrorAs(t, err, &authErr)
		require.Equal(t, auth.KeypairUnavailable, authErr.Issue)
		require.ErrorIs(t, err, auth.ErrKeyUnavailable)
	})
}

func TestIssuerForRemoteOrAnonymous(t *testing.T) {
	f := newFixture(t)
	p, _ := project(t, projectURL)
	sel := auth.Selection{Override: "alice-claude"}

	t.Run("falls back with one warning", func(t *testing.T) {
		logs := remotetest.NewLogRecorder()
		i := f.issuer(t, f.keyring(t, nil))

		creds := i.ForRemoteOrAnonymous(context.Background(), p, sel, audience, logs.Logger())
		require.Equal(t, auth.NoCredentials{}, creds)

		records := logs.Records()
		require.Len(t, records, 1)
		require.Equal(t, slog.LevelWarn, records[0].Level)
		attrs := map[string]slog.Value{}
		records[0].Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value
			return true
		})
		require.Equal(t, p.String(), attrs["remote"].String())
		err, ok := attrs["error"].Any().(error)
		require.True(t, ok)
		require.ErrorIs(t, err, delegation.ErrNoUsableCertificate)

		req := unsent()
		require.NoError(t, creds.Attach(req))
		require.Empty(t, req.Header.Values("Authorization"))
	})

	t.Run("logs nothing when credentials are issued", func(t *testing.T) {
		logs := remotetest.NewLogRecorder()
		i := f.issuer(t, f.keyring(t, []delegation.Certificate{f.cert}))

		creds := i.ForRemoteOrAnonymous(context.Background(), p, sel, audience, logs.Logger())
		require.IsType(t, &auth.Bearer{}, creds)
		require.Empty(t, logs.Records())

		req := unsent()
		require.NoError(t, creds.Attach(req))
		require.NoError(t, bearerToken(t, req).Verify(f.agent.PublicKey(), audience, f.clock.Now()))
	})

	t.Run("requires a logger", func(t *testing.T) {
		i := f.issuer(t, f.keyring(t, nil))
		require.Panics(t, func() {
			i.ForRemoteOrAnonymous(context.Background(), p, sel, audience, nil)
		})
	})
}
