package delegation_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/identity"
)

// otherAgentSeed is the RFC 8032 §7.1 TEST 3 seed: a second agent of the
// same human.
const otherAgentSeed = "c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7"

const (
	storage = "https://atomic.storage"
	staging = "https://staging.example"
)

func otherAgent(t *testing.T) identity.Signer { return seedSigner(t, otherAgentSeed) }

// chooseNow is the time every choice is made at: a day after the golden
// certificate was issued.
func chooseNow() time.Time {
	return goldenIssued().Add(24 * time.Hour)
}

// storageScope is the scope of Atomic's own tests: read and push on
// storage, for projects under acme/.
func storageScope() delegation.Scope {
	s := delegation.NewScope(delegation.PermissionRead, delegation.PermissionPush)
	s.Servers = []string{storage}
	s.Projects = []string{"acme/*"}
	return s
}

// issueTo has the human issue delegate a certificate with scope, issued
// age before chooseNow and expiring life after it was issued.
func issueTo(t *testing.T, delegate identity.Signer, scope delegation.Scope, age, life time.Duration) delegation.Certificate {
	t.Helper()
	issued := chooseNow().Add(-age)
	c, err := delegation.Issue(context.Background(), human(t), delegation.Grant{
		DelegatorName: "alice",
		DelegateKey:   delegate.PublicKey(),
		DelegateName:  "alice+claude",
		Issued:        issued,
		Expires:       issued.Add(life),
		Scope:         scope,
	})
	require.NoError(t, err)
	return c
}

// issue has the human issue the agent a certificate.
func issue(t *testing.T, scope delegation.Scope, age, life time.Duration) delegation.Certificate {
	t.Helper()
	return issueTo(t, agent(t), scope, age, life)
}

const (
	day  = 24 * time.Hour
	week = 7 * day
)

// revokedSet is the map-backed revoked func the tests pass to Choose.
type revokedSet map[delegation.ID]bool

func (r revokedSet) revoked(id delegation.ID) bool { return r[id] }

func candidates(t *testing.T, stored ...delegation.Certificate) delegation.Candidates {
	t.Helper()
	return delegation.Candidates{
		Agent:  agent(t).PublicKey().ID(),
		Server: storage,
		Stored: stored,
	}
}

// requireSelection asserts that err is a *SelectionError for reason and
// returns it.
func requireSelection(t *testing.T, err error, reason delegation.SelectionReason) *delegation.SelectionError {
	t.Helper()
	require.ErrorIs(t, err, delegation.ErrNoUsableCertificate)
	var se *delegation.SelectionError
	require.ErrorAs(t, err, &se)
	require.Equal(t, reason, se.Reason, "%v", err)
	return se
}

func TestChooseNoneStored(t *testing.T) {
	_, err := delegation.Choose(candidates(t), chooseNow(), revokedSet{}.revoked)
	se := requireSelection(t, err, delegation.NoCertificates)
	require.Equal(t, storage, se.Server)
	require.NoError(t, se.Err)
	require.ErrorContains(t, err, "no stored certificate names this agent")
}

func TestChooseIgnoresAnotherDelegatesCertificate(t *testing.T) {
	theirs := issueTo(t, otherAgent(t), storageScope(), day, week)
	_, err := delegation.Choose(candidates(t, theirs), chooseNow(), revokedSet{}.revoked)
	requireSelection(t, err, delegation.NoCertificates)
}

func TestChooseIgnoresTamperedCertificate(t *testing.T) {
	good := issue(t, storageScope(), day, week)
	b, err := good.MarshalJSON()
	require.NoError(t, err)
	tampered, err := delegation.ParseCertificate([]byte(strings.Replace(string(b), `"push"]`, `"full"]`, 1)))
	require.NoError(t, err)
	require.Error(t, tampered.VerifySelfContained())

	_, err = delegation.Choose(candidates(t, tampered), chooseNow(), revokedSet{}.revoked)
	requireSelection(t, err, delegation.NoCertificates)
}

func TestChooseFindsTheAgentsCertificate(t *testing.T) {
	mine := issue(t, storageScope(), day, week)
	theirs := issueTo(t, otherAgent(t), delegation.NewScope(delegation.PermissionFull), 0, week)

	got, err := delegation.Choose(candidates(t, theirs, mine), chooseNow(), revokedSet{}.revoked)
	require.NoError(t, err)
	require.Equal(t, mine.ID(), got.ID())
}

func TestChooseServerIsNormalized(t *testing.T) {
	mine := issue(t, storageScope(), day, week)
	c := candidates(t, mine)
	c.Server = " HTTPS://Atomic.Storage/ "

	got, err := delegation.Choose(c, chooseNow(), revokedSet{}.revoked)
	require.NoError(t, err)
	require.Equal(t, mine.ID(), got.ID())
}

func TestChooseNewestWins(t *testing.T) {
	older := issue(t, delegation.NewScope(delegation.PermissionRead), 3*day, week)
	newer := issue(t, storageScope(), day, week)

	for _, order := range [][]delegation.Certificate{{older, newer}, {newer, older}} {
		got, err := delegation.Choose(candidates(t, order...), chooseNow(), revokedSet{}.revoked)
		require.NoError(t, err)
		require.Equal(t, newer.ID(), got.ID())
	}
}

func TestChooseNewestRevokedUsesAnOlderOne(t *testing.T) {
	older := issue(t, storageScope(), 3*day, week)
	newer := issue(t, storageScope(), day, week)

	got, err := delegation.Choose(candidates(t, newer, older), chooseNow(), revokedSet{newer.ID(): true}.revoked)
	require.NoError(t, err)
	require.Equal(t, older.ID(), got.ID())
}

func TestChooseSkipsUnusableOlderAndNewer(t *testing.T) {
	expired := issue(t, storageScope(), 2*week, week)
	usable := issue(t, storageScope(), 3*day, week)
	staged := issue(t, delegation.Scope{Permissions: []delegation.Permission{delegation.PermissionRead}, Servers: []string{staging}}, day, week)

	got, err := delegation.Choose(candidates(t, expired, staged, usable), chooseNow(), revokedSet{}.revoked)
	require.NoError(t, err)
	require.Equal(t, usable.ID(), got.ID())
}

func TestChooseNewestDecidesTheReason(t *testing.T) {
	stagingScope := storageScope()
	stagingScope.Servers = []string{staging, "https://other.example"}

	// Each case's older certificate is unusable for a different reason from
	// its newest, so the reason can only have come from the newest.
	tests := []struct {
		name    string
		newest  func(t *testing.T) delegation.Certificate
		older   func(t *testing.T) delegation.Certificate
		revoked func(newest, older delegation.Certificate) revokedSet
		reason  delegation.SelectionReason
		servers []string
		errIs   error
		message string
	}{
		{
			name:    "revoked",
			newest:  func(t *testing.T) delegation.Certificate { return issue(t, storageScope(), day, week) },
			older:   func(t *testing.T) delegation.Certificate { return issue(t, storageScope(), 2*week, week) },
			revoked: func(n, _ delegation.Certificate) revokedSet { return revokedSet{n.ID(): true} },
			reason:  delegation.NewestRevoked,
			message: "the newest certificate has been revoked",
		},
		{
			name: "revoked and expired",
			newest: func(t *testing.T) delegation.Certificate {
				return issue(t, storageScope(), 2*week, week)
			},
			older:   func(t *testing.T) delegation.Certificate { return issue(t, stagingScope, 3*week, 4*week) },
			revoked: func(n, _ delegation.Certificate) revokedSet { return revokedSet{n.ID(): true} },
			reason:  delegation.NewestRevoked,
			message: "has been revoked",
		},
		{
			name:    "expired",
			newest:  func(t *testing.T) delegation.Certificate { return issue(t, storageScope(), 2*week, week) },
			older:   func(t *testing.T) delegation.Certificate { return issue(t, storageScope(), 3*week, 4*week) },
			revoked: func(_, o delegation.Certificate) revokedSet { return revokedSet{o.ID(): true} },
			reason:  delegation.NewestExpired,
			errIs:   delegation.ErrExpired,
			message: "the newest certificate has expired",
		},
		{
			name:    "not valid for server",
			newest:  func(t *testing.T) delegation.Certificate { return issue(t, stagingScope, day, week) },
			older:   func(t *testing.T) delegation.Certificate { return issue(t, storageScope(), 2*week, week) },
			revoked: func(delegation.Certificate, delegation.Certificate) revokedSet { return revokedSet{} },
			reason:  delegation.NotValidForServer,
			servers: []string{staging, "https://other.example"},
			message: "does not allow read on " + storage + "; it is scoped to " + staging + ", https://other.example",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newest, older := tt.newest(t), tt.older(t)
			_, err := delegation.Choose(candidates(t, older, newest), chooseNow(), tt.revoked(newest, older).revoked)
			se := requireSelection(t, err, tt.reason)
			require.Equal(t, storage, se.Server)
			require.Equal(t, tt.servers, se.Servers)
			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
			} else {
				require.NoError(t, se.Err)
			}
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func TestChoosePushOnlyIsNotValidForServer(t *testing.T) {
	scope := storageScope()
	scope.Permissions = []delegation.Permission{delegation.PermissionPush}
	pushOnly := issue(t, scope, day, week)

	_, err := delegation.Choose(candidates(t, pushOnly), chooseNow(), revokedSet{}.revoked)
	se := requireSelection(t, err, delegation.NotValidForServer)
	require.Equal(t, []string{storage}, se.Servers)
}

func TestChoosePushOnlyUnscopedNamesNoServer(t *testing.T) {
	pushOnly := issue(t, delegation.NewScope(delegation.PermissionPush), day, week)
	c := candidates(t, pushOnly)
	c.Server = ""

	_, err := delegation.Choose(c, chooseNow(), revokedSet{}.revoked)
	se := requireSelection(t, err, delegation.NotValidForServer)
	require.Empty(t, se.Servers)
	require.ErrorContains(t, err, "not scoped to any server and grants nothing that implies read")
}

func TestChooseAdminImpliesRead(t *testing.T) {
	scope := storageScope()
	scope.Permissions = []delegation.Permission{delegation.PermissionAdmin}
	admin := issue(t, scope, day, week)

	got, err := delegation.Choose(candidates(t, admin), chooseNow(), revokedSet{}.revoked)
	require.NoError(t, err)
	require.Equal(t, admin.ID(), got.ID())
}

func TestChooseExplicit(t *testing.T) {
	stored := issue(t, storageScope(), day, week)

	t.Run("wins over stored", func(t *testing.T) {
		explicit := issue(t, storageScope(), 3*day, week)
		c := candidates(t, stored)
		c.Explicit = "\n " + explicit.HeaderValue() + " \n"

		got, err := delegation.Choose(c, chooseNow(), revokedSet{}.revoked)
		require.NoError(t, err)
		require.Equal(t, explicit.ID(), got.ID())
	})

	t.Run("for another server is still chosen", func(t *testing.T) {
		scope := storageScope()
		scope.Servers = []string{staging}
		explicit := issue(t, scope, 2*day, week)
		c := candidates(t, stored)
		c.Explicit = explicit.HeaderValue()

		got, err := delegation.Choose(c, chooseNow(), revokedSet{}.revoked)
		require.NoError(t, err)
		require.Equal(t, explicit.ID(), got.ID())
	})

	t.Run("is not checked for revocation", func(t *testing.T) {
		explicit := issue(t, storageScope(), day, week)
		c := candidates(t)
		c.Explicit = explicit.HeaderValue()

		got, err := delegation.Choose(c, chooseNow(), revokedSet{explicit.ID(): true}.revoked)
		require.NoError(t, err)
		require.Equal(t, explicit.ID(), got.ID())
	})

	t.Run("blank is ignored", func(t *testing.T) {
		c := candidates(t, stored)
		c.Explicit = " \t\n"

		got, err := delegation.Choose(c, chooseNow(), revokedSet{}.revoked)
		require.NoError(t, err)
		require.Equal(t, stored.ID(), got.ID())
	})
}

func TestChooseExplicitUnusable(t *testing.T) {
	good := issue(t, storageScope(), day, week)
	b, err := good.MarshalJSON()
	require.NoError(t, err)
	tampered := base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(b), `"push"]`, `"full"]`, 1)))

	tests := []struct {
		name    string
		header  string
		errIs   error
		message string
	}{
		{
			name:   "does not decode",
			header: "not-a-grant!",
			errIs:  delegation.ErrInvalid,
		},
		{
			name:   "tampered",
			header: tampered,
			errIs:  delegation.ErrVerify,
		},
		{
			name:    "expired",
			header:  issue(t, storageScope(), 2*week, week).HeaderValue(),
			errIs:   delegation.ErrExpired,
			message: "expired at",
		},
		{
			name:    "for another delegate",
			header:  issueTo(t, otherAgent(t), storageScope(), day, week).HeaderValue(),
			message: "was issued to " + otherAgent(t).PublicKey().AtomicDID().String(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A usable stored certificate is never fallen back to.
			c := candidates(t, good)
			c.Explicit = tt.header

			got, err := delegation.Choose(c, chooseNow(), revokedSet{}.revoked)
			se := requireSelection(t, err, delegation.ExplicitUnusable)
			require.Equal(t, delegation.Certificate{}, got)
			require.Error(t, se.Err)
			if tt.errIs != nil {
				require.ErrorIs(t, err, tt.errIs)
			}
			require.ErrorContains(t, err, "the explicit certificate is unusable")
			require.ErrorContains(t, err, tt.message)
			require.NotContains(t, err.Error(), tt.header)
		})
	}
}

func TestChooseNeedsRevoked(t *testing.T) {
	_, err := delegation.Choose(candidates(t, issue(t, storageScope(), day, week)), chooseNow(), nil)
	require.Error(t, err)
	require.NotErrorIs(t, err, delegation.ErrNoUsableCertificate)
}

func TestSelectionReasonString(t *testing.T) {
	for reason, want := range map[delegation.SelectionReason]string{
		delegation.ExplicitUnusable:  "ExplicitUnusable",
		delegation.NoCertificates:    "NoCertificates",
		delegation.NewestRevoked:     "NewestRevoked",
		delegation.NewestExpired:     "NewestExpired",
		delegation.NotValidForServer: "NotValidForServer",
		0:                            "SelectionReason(0)",
	} {
		require.Equal(t, want, reason.String())
	}
}

func TestCheckPermission(t *testing.T) {
	cert := issue(t, storageScope(), day, week)
	acme := delegation.ResourceRef{Server: storage, Project: "acme/api"}

	t.Run("push is granted", func(t *testing.T) {
		require.NoError(t, delegation.CheckPermission(cert, delegation.PermissionPush, acme, chooseNow(), "alice+claude"))
	})

	t.Run("admin is refused", func(t *testing.T) {
		err := delegation.CheckPermission(cert, delegation.PermissionAdmin, acme, chooseNow(), "alice+claude")
		require.ErrorIs(t, err, delegation.ErrNotPermitted)
		var pe *delegation.PermissionError
		require.ErrorAs(t, err, &pe)
		require.Equal(t, &delegation.PermissionError{
			Identity:   "alice+claude",
			Permission: delegation.PermissionAdmin,
			Resource:   acme,
			Granted:    []delegation.Permission{delegation.PermissionRead, delegation.PermissionPush},
		}, pe)
		require.Equal(t, "atomic identity delegate alice+claude --can admin", pe.Remedy())
		require.EqualError(t, err, `delegation: not permitted: agent "alice+claude" may not admin on project acme/api; granted: read, push`)
	})

	t.Run("out-of-scope project", func(t *testing.T) {
		other := delegation.ResourceRef{Server: storage, Project: "other/api"}
		err := delegation.CheckPermission(cert, delegation.PermissionPush, other, chooseNow(), "alice+claude")
		var pe *delegation.PermissionError
		require.ErrorAs(t, err, &pe)
		require.Equal(t, []string{"acme/*"}, pe.Projects)
		require.False(t, pe.Expired)
		require.Equal(t, "atomic identity delegate alice+claude --can push", pe.Remedy())
		require.ErrorContains(t, err, "other/api")
		require.ErrorContains(t, err, "scoped to projects: acme/*")
	})

	t.Run("in-scope project reports no projects", func(t *testing.T) {
		err := delegation.CheckPermission(cert, delegation.PermissionRecord, acme, chooseNow(), "alice+claude")
		var pe *delegation.PermissionError
		require.ErrorAs(t, err, &pe)
		require.Nil(t, pe.Projects)
		require.NotContains(t, err.Error(), "scoped to projects")
	})

	t.Run("expired", func(t *testing.T) {
		err := delegation.CheckPermission(cert, delegation.PermissionPush, acme, chooseNow().Add(2*week), "alice+claude")
		var pe *delegation.PermissionError
		require.ErrorAs(t, err, &pe)
		require.True(t, pe.Expired)
		require.Nil(t, pe.Projects)
		require.ErrorContains(t, err, "the certificate has expired")
	})

	t.Run("granted is a copy", func(t *testing.T) {
		err := delegation.CheckPermission(cert, delegation.PermissionAdmin, acme, chooseNow(), "alice+claude")
		var pe *delegation.PermissionError
		require.ErrorAs(t, err, &pe)
		pe.Granted[0] = delegation.PermissionFull
		require.Equal(t, []delegation.Permission{delegation.PermissionRead, delegation.PermissionPush},
			cert.AgentDelegation().Scope.Permissions)
	})
}
