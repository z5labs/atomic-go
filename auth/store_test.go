package auth_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/auth"
	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
)

// signer returns the signer whose seed is 32 copies of b.
func signer(t *testing.T, b byte) identity.Signer {
	t.Helper()
	s, err := identity.SignerFromSeed(bytes.Repeat([]byte{b}, 32))
	require.NoError(t, err)
	return s
}

// entry returns a keyring entry named name whose key is seeded from b.
func entry(t *testing.T, name string, b byte) auth.KeyringEntry {
	t.Helper()
	s := signer(t, b)
	return auth.KeyringEntry{
		Identity: identity.Identity{Name: name, Type: identity.TypeUser, PublicKey: s.PublicKey()},
		Signer:   s,
	}
}

// keyring builds a keyring with default def from entries.
func keyring(t *testing.T, def string, entries ...auth.KeyringEntry) *auth.Keyring {
	t.Helper()
	k, err := auth.NewKeyring(auth.KeyringContents{Default: def, Entries: entries})
	require.NoError(t, err)
	return k
}

const tenantURL = "https://aaron.atomic.storage/workspaces/w/projects/p/code"

func TestResolve(t *testing.T) {
	testCases := []struct {
		name    string
		def     string
		entries []string
		sel     auth.Selection
		want    string // "" for no identity
	}{
		{
			name:    "an exact match",
			def:     "aaron-claude",
			entries: []string{"Aaron", "aaron", "aaron-claude"},
			want:    "aaron",
		},
		{
			name:    "a case-insensitive match",
			def:     "aaron-claude",
			entries: []string{"other", "Aaron", "aaron-claude"},
			want:    "Aaron",
		},
		{
			name:    "an inferred name falls back to the default",
			def:     "aaron-claude",
			entries: []string{"other", "aaron-claude"},
			want:    "aaron-claude",
		},
		{
			name:    "an agent identity is inferred and falls back",
			def:     "aaron-claude",
			entries: []string{"aaron-claude"},
			sel:     auth.Selection{AgentIdentity: "missing-agent"},
			want:    "aaron-claude",
		},
		{
			name:    "an explicit name that exists",
			def:     "other",
			entries: []string{"other", "aaron-claude"},
			sel:     auth.Selection{Override: "AARON-CLAUDE"},
			want:    "aaron-claude",
		},
		{
			name:    "an explicit name that is missing never falls back",
			def:     "aaron-claude",
			entries: []string{"aaron-claude"},
			sel:     auth.Selection{Override: "foo"},
		},
		{
			name:    "an inferred miss and no default",
			entries: []string{"other"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var entries []auth.KeyringEntry
			for i, n := range tc.entries {
				entries = append(entries, entry(t, n, byte(i+1)))
			}
			k := keyring(t, tc.def, entries...)
			p, _ := project(t, tenantURL)

			got, ok, err := auth.Resolve(context.Background(), k, p, tc.sel)
			require.NoError(t, err)
			require.Equal(t, tc.want != "", ok)
			require.Equal(t, tc.want, got.Name)
		})
	}
}

func TestResolveNoNameUsesTheDefault(t *testing.T) {
	k := keyring(t, "me", entry(t, "me", 1))
	p, _ := project(t, "http://localhost:8080/workspaces/w/projects/p/code")

	got, ok, err := auth.Resolve(context.Background(), k, p, auth.Selection{})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "me", got.Name)
}

// failingDirectory is a Directory whose every read fails.
type failingDirectory struct{ err error }

func (d failingDirectory) Identities(context.Context) ([]identity.Identity, error) {
	return nil, d.err
}

func (d failingDirectory) DefaultIdentity(context.Context) (string, error) {
	return "", d.err
}

func TestResolveReturnsTheDirectorysError(t *testing.T) {
	boom := errors.New("boom")
	p, _ := project(t, tenantURL)

	_, ok, err := auth.Resolve(context.Background(), failingDirectory{boom}, p, auth.Selection{})
	require.ErrorIs(t, err, boom)
	require.False(t, ok)

	_, ok, err = auth.ResolveForManagement(context.Background(), failingDirectory{boom}, "aaron")
	require.ErrorIs(t, err, boom)
	require.False(t, ok)
}

func TestResolveForManagement(t *testing.T) {
	human := entry(t, "aaron", 1)
	agent := entry(t, "aaron+claude", 2)
	agent.Identity.Type = identity.TypeDelegated
	k := keyring(t, "aaron", human, agent)
	ctx := context.Background()

	got, ok, err := auth.ResolveForManagement(ctx, k, "")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "aaron", got.Name, "the default")

	got, ok, err = auth.ResolveForManagement(ctx, k, "aaron+claude")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "aaron+claude", got.Name, "the profile's identity")

	_, ok, err = auth.ResolveForManagement(ctx, k, "Aaron")
	require.NoError(t, err)
	require.False(t, ok, "matched exactly, with no fallback")

	_, ok, err = auth.ResolveForManagement(ctx, keyring(t, "", human), "")
	require.NoError(t, err)
	require.False(t, ok, "no default")
}

func TestCheckPushCredentials(t *testing.T) {
	ctx := context.Background()
	p, _ := project(t, tenantURL)

	t.Run("passes when the identity resolves and its key loads", func(t *testing.T) {
		k := keyring(t, "", entry(t, "aaron", 1))
		require.NoError(t, auth.CheckPushCredentials(ctx, k, p, auth.Selection{}))
	})

	testCases := []struct {
		name     string
		store    func(t *testing.T) auth.PushStore
		sel      auth.Selection
		issue    auth.CredentialIssue
		identity string
	}{
		{
			name:  "no identity and no default",
			store: func(t *testing.T) auth.PushStore { return keyring(t, "", entry(t, "other", 1)) },
			issue: auth.NoIdentity,
		},
		{
			name:     "an explicit identity that is missing",
			store:    func(t *testing.T) auth.PushStore { return keyring(t, "aaron", entry(t, "aaron", 1)) },
			sel:      auth.Selection{Override: "foo"},
			issue:    auth.ExplicitIdentityNotFound,
			identity: "foo",
		},
		{
			name: "a nil Signer",
			store: func(t *testing.T) auth.PushStore {
				e := entry(t, "aaron", 1)
				e.Signer = nil
				return keyring(t, "", e)
			},
			issue:    auth.KeypairUnavailable,
			identity: "aaron",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := auth.CheckPushCredentials(ctx, tc.store(t), p, tc.sel)
			require.ErrorIs(t, err, auth.ErrAuthenticationFailed)
			var ae *auth.AuthenticationError
			require.ErrorAs(t, err, &ae)
			require.Equal(t, p.String(), ae.Remote)
			require.Equal(t, tc.issue, ae.Issue)
			require.Equal(t, tc.identity, ae.Name)
		})
	}

	t.Run("a nil Signer's error is kept", func(t *testing.T) {
		e := entry(t, "aaron", 1)
		e.Signer = nil
		err := auth.CheckPushCredentials(ctx, keyring(t, "", e), p, auth.Selection{})
		require.ErrorIs(t, err, auth.ErrKeyUnavailable)
	})
}

// loaderFunc is a PushStore over a keyring's directory whose keys come from
// load.
type loaderFunc struct {
	*auth.Keyring
	load func(ctx context.Context, id identity.ID) (identity.Signer, error)
}

func (l loaderFunc) Signer(ctx context.Context, id identity.ID) (identity.Signer, error) {
	return l.load(ctx, id)
}

func TestCheckPushCredentialsKeyLoader(t *testing.T) {
	p, _ := project(t, tenantURL)
	k := keyring(t, "", entry(t, "aaron", 1))

	t.Run("a signer for another key is unavailable", func(t *testing.T) {
		other := signer(t, 9)
		store := loaderFunc{k, func(context.Context, identity.ID) (identity.Signer, error) { return other, nil }}
		err := auth.CheckPushCredentials(context.Background(), store, p, auth.Selection{})
		var ae *auth.AuthenticationError
		require.ErrorAs(t, err, &ae)
		require.Equal(t, auth.KeypairUnavailable, ae.Issue)
		require.ErrorIs(t, err, auth.ErrKeyUnavailable)
	})

	t.Run("a nil signer with no error is unavailable", func(t *testing.T) {
		store := loaderFunc{k, func(context.Context, identity.ID) (identity.Signer, error) { return nil, nil }}
		err := auth.CheckPushCredentials(context.Background(), store, p, auth.Selection{})
		var ae *auth.AuthenticationError
		require.ErrorAs(t, err, &ae)
		require.Equal(t, auth.KeypairUnavailable, ae.Issue)
	})

	t.Run("a cancelled context is not a missing key", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		store := loaderFunc{k, func(ctx context.Context, _ identity.ID) (identity.Signer, error) {
			cancel()
			return nil, ctx.Err()
		}}
		err := auth.CheckPushCredentials(ctx, store, p, auth.Selection{})
		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, auth.ErrAuthenticationFailed)
	})
}

func TestAuthenticationErrorText(t *testing.T) {
	const remote = "https://aaron.atomic.storage/workspaces/w/projects/p/code"
	const register = " Register your identity with the server first: `atomic identity register <server-url>`."

	testCases := []struct {
		issue auth.CredentialIssue
		name  string
		want  string
	}{
		{
			issue: auth.NoIdentity,
			want: "Not authenticated for this remote: could not determine an identity from the remote URL, " +
				"no --identity was given, and no default identity is set. Pass --identity <NAME> " +
				"(see `atomic identity list`) or set a default with `atomic identity new <name> --set-default`." + register,
		},
		{
			issue: auth.ExplicitIdentityNotFound,
			name:  "foo",
			want: "Not authenticated for this remote: --identity 'foo' does not match any identity on this machine " +
				"(see `atomic identity list`)." + register,
		},
		{
			issue: auth.KeypairUnavailable,
			name:  "alice",
			want:  "Not authenticated for this remote: could not load the signing key for identity 'alice'." + register,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.issue.String(), func(t *testing.T) {
			require.Equal(t, tc.want, tc.issue.Message(tc.name))
			err := &auth.AuthenticationError{Remote: remote, Issue: tc.issue, Name: tc.name}
			require.Equal(t, "Authentication failed for remote '"+remote+": "+tc.want+"'", err.Error())
			require.ErrorIs(t, err, auth.ErrAuthenticationFailed)
		})
	}
	require.Equal(t, "CredentialIssue(0)", auth.CredentialIssue(0).String())
}

func TestAuthenticationErrorUnwrap(t *testing.T) {
	cause := errors.New("locked")
	err := &auth.AuthenticationError{Remote: "r", Issue: auth.KeypairUnavailable, Name: "a", Err: cause}
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, auth.ErrAuthenticationFailed)
	require.NotErrorIs(t, err, auth.ErrKeyUnavailable)
}

func TestNewKeyringRefuses(t *testing.T) {
	a := entry(t, "a", 1)
	b := entry(t, "b", 2)

	sameName := entry(t, "a", 3)
	sameKey := entry(t, "c", 1)
	mismatched := entry(t, "d", 4)
	mismatched.Signer = signer(t, 5)
	noName := entry(t, "", 6)
	noKey := auth.KeyringEntry{Identity: identity.Identity{Name: "e"}}

	testCases := []struct {
		name     string
		contents auth.KeyringContents
	}{
		{"a duplicate name", auth.KeyringContents{Entries: []auth.KeyringEntry{a, sameName}}},
		{"a duplicate key", auth.KeyringContents{Entries: []auth.KeyringEntry{a, sameKey}}},
		{"a mismatched key", auth.KeyringContents{Entries: []auth.KeyringEntry{a, mismatched}}},
		{"no name", auth.KeyringContents{Entries: []auth.KeyringEntry{noName}}},
		{"no key", auth.KeyringContents{Entries: []auth.KeyringEntry{noKey}}},
		{"an unknown default", auth.KeyringContents{Default: "z", Entries: []auth.KeyringEntry{a, b}}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			k, err := auth.NewKeyring(tc.contents)
			require.ErrorIs(t, err, auth.ErrInvalidArgument)
			require.Nil(t, k)
		})
	}
}

func TestKeyringIsACopy(t *testing.T) {
	ctx := context.Background()
	entries := []auth.KeyringEntry{entry(t, "a", 1), entry(t, "b", 2)}
	k := keyring(t, "a", entries...)
	entries[0].Identity.Name = "changed"

	ids, err := k.Identities(ctx)
	require.NoError(t, err)
	require.Equal(t, "a", ids[0].Name)
	ids[1].Name = "changed"

	ids, err = k.Identities(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, []string{ids[0].Name, ids[1].Name})
}

func TestKeyringSigner(t *testing.T) {
	ctx := context.Background()
	a := entry(t, "a", 1)
	k := keyring(t, "", a)

	s, err := k.Signer(ctx, a.Identity.PublicKey.ID())
	require.NoError(t, err)
	require.Equal(t, a.Identity.PublicKey, s.PublicKey())

	_, err = k.Signer(ctx, signer(t, 7).PublicKey().ID())
	require.ErrorIs(t, err, auth.ErrKeyUnavailable)
}

func TestKeyringHonoursTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := entry(t, "a", 1)
	k := keyring(t, "a", a)

	_, err := k.Identities(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = k.DefaultIdentity(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = k.Signer(ctx, a.Identity.PublicKey.ID())
	require.ErrorIs(t, err, context.Canceled)
	_, err = k.Certificates(ctx, a.Identity.PublicKey.ID())
	require.ErrorIs(t, err, context.Canceled)
	_, err = k.Revoked(ctx, delegation.ID{})
	require.ErrorIs(t, err, context.Canceled)
}

// certificateFor has human issue delegate a certificate at issued.
func certificateFor(t *testing.T, human, delegate identity.Signer, issued time.Time) delegation.Certificate {
	t.Helper()
	c, err := delegation.Issue(context.Background(), human, delegation.Grant{
		DelegatorName: "aaron",
		DelegateKey:   delegate.PublicKey(),
		DelegateName:  "aaron+claude",
		Issued:        issued,
		Scope:         delegation.NewScope(delegation.PermissionRead),
	})
	require.NoError(t, err)
	return c
}

func TestKeyringCertificatesAndRevocations(t *testing.T) {
	ctx := context.Background()
	human := signer(t, 1)
	agent := signer(t, 2)
	other := signer(t, 3)
	issued := time.Date(2026, 11, 24, 9, 30, 0, 0, time.UTC)

	first := certificateFor(t, human, agent, issued)
	others := certificateFor(t, human, other, issued)
	second := certificateFor(t, human, agent, issued.Add(time.Hour))

	k, err := auth.NewKeyring(auth.KeyringContents{
		Certificates: []delegation.Certificate{first, others, second},
		Revoked:      []delegation.ID{second.ID(), second.ID()},
	})
	require.NoError(t, err)

	got, err := k.Certificates(ctx, agent.PublicKey().ID())
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []delegation.ID{first.ID(), second.ID()}, []delegation.ID{got[0].ID(), got[1].ID()})

	got, err = k.Certificates(ctx, other.PublicKey().ID())
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, others.ID(), got[0].ID())

	got, err = k.Certificates(ctx, human.PublicKey().ID())
	require.NoError(t, err)
	require.Empty(t, got)

	revoked, err := k.Revoked(ctx, second.ID())
	require.NoError(t, err)
	require.True(t, revoked)
	revoked, err = k.Revoked(ctx, first.ID())
	require.NoError(t, err)
	require.False(t, revoked)
}

// The Keyring is a Store, and so a PushStore.
var _ auth.Store = (*auth.Keyring)(nil)
