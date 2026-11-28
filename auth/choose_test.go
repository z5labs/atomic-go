package auth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/auth"
	"github.com/z5labs/atomic-go/remote/serverurl"
)

// project parses raw as a stored project URL and returns it with the
// username its userinfo carries, as a caller fills in Selection.URLUser.
func project(t *testing.T, raw string) (serverurl.Project, string) {
	t.Helper()
	p, user, err := serverurl.ParseProject(raw)
	require.NoError(t, err)
	if user == nil {
		return p, ""
	}
	return p, user.Username()
}

func TestChooseName(t *testing.T) {
	storage := auth.Profile{Host: "atomic.storage", Identity: "aaron"}
	staging := auth.Profile{Host: "staging.atomic.storage", Identity: "stager"}

	testCases := []struct {
		name string
		url  string
		sel  auth.Selection
		want auth.Choice
	}{
		{
			name: "the override wins over everything",
			url:  "https://bob@alice.atomic.storage/workspaces/w/projects/p/code",
			sel: auth.Selection{
				Override:      "Aaron",
				AgentIdentity: "agent",
				Profiles:      []auth.Profile{storage},
			},
			want: auth.Choice{Name: "Aaron", Source: auth.SourceOverride},
		},
		{
			name: "the agent identity is trimmed",
			url:  "https://bob@alice.atomic.storage/workspaces/w/projects/p/code",
			sel:  auth.Selection{AgentIdentity: "  ci-agent\n"},
			want: auth.Choice{Name: "ci-agent", Source: auth.SourceAgentIdentity},
		},
		{
			name: "a blank agent identity is skipped",
			url:  "https://alice.atomic.storage/workspaces/w/projects/p/code",
			sel:  auth.Selection{AgentIdentity: " \t "},
			want: auth.Choice{Name: "alice", Source: auth.SourceHostLabel},
		},
		{
			name: "the URL user wins over a profile and the host label",
			url:  "http://bob@alice.localhost:8080/workspaces/w/projects/p/code",
			sel:  auth.Selection{Profiles: []auth.Profile{{Host: "localhost", Identity: "local"}}},
			want: auth.Choice{Name: "bob", Source: auth.SourceURLUser},
		},
		{
			name: "a profile wins over the host label",
			url:  "https://alice.atomic.storage/workspaces/w/projects/p/code",
			sel:  auth.Selection{Profiles: []auth.Profile{storage}},
			want: auth.Choice{Name: "aaron", Source: auth.SourceProfile},
		},
		{
			name: "the longest suffix wins over an active profile",
			url:  "https://x.staging.atomic.storage/workspaces/w/projects/p/code",
			sel: auth.Selection{Profiles: []auth.Profile{
				{Host: "atomic.storage", Identity: "aaron", Active: true},
				staging,
			}},
			want: auth.Choice{Name: "stager", Source: auth.SourceProfile},
		},
		{
			name: "a tie goes to the active profile",
			url:  "https://alice.atomic.storage/workspaces/w/projects/p/code",
			sel: auth.Selection{Profiles: []auth.Profile{
				{Host: "atomic.storage", Identity: "legacy"},
				{Host: "atomic.storage", Identity: "named", Active: true},
				{Host: "atomic.storage", Identity: "later"},
			}},
			want: auth.Choice{Name: "named", Source: auth.SourceProfile},
		},
		{
			name: "a tie between inactive profiles goes to the first",
			url:  "https://alice.atomic.storage/workspaces/w/projects/p/code",
			sel: auth.Selection{Profiles: []auth.Profile{
				{Host: "atomic.storage", Identity: "first"},
				{Host: "atomic.storage", Identity: "second"},
			}},
			want: auth.Choice{Name: "first", Source: auth.SourceProfile},
		},
		{
			name: "the agent identity is preferred over the profile's identity",
			url:  "https://alice.atomic.storage/workspaces/w/projects/p/code",
			sel: auth.Selection{Profiles: []auth.Profile{
				{Host: "atomic.storage", Identity: "aaron", AgentIdentity: "aaron+claude"},
			}},
			want: auth.Choice{Name: "aaron+claude", Source: auth.SourceProfile},
		},
		{
			name: "the server host itself matches",
			url:  "https://atomic.storage/workspaces/w/projects/p/code",
			sel:  auth.Selection{Profiles: []auth.Profile{storage}},
			want: auth.Choice{Name: "aaron", Source: auth.SourceProfile},
		},
		{
			name: "a suffix that is not on a dot boundary does not match",
			url:  "https://alice.notatomic.storage/workspaces/w/projects/p/code",
			sel:  auth.Selection{Profiles: []auth.Profile{{Host: "atomic.storage", Identity: "aaron"}}},
			want: auth.Choice{Name: "alice", Source: auth.SourceHostLabel},
		},
		{
			name: "profile hosts are lowercased and their ports ignored",
			url:  "http://alice.localhost:8080/workspaces/w/projects/p/code",
			sel:  auth.Selection{Profiles: []auth.Profile{{Host: "LocalHost:9090", Identity: "local"}}},
			want: auth.Choice{Name: "local", Source: auth.SourceProfile},
		},
		{
			name: "a profile with no identity binds nothing",
			url:  "https://alice.atomic.storage/workspaces/w/projects/p/code",
			sel:  auth.Selection{Profiles: []auth.Profile{{Host: "atomic.storage", AgentIdentity: "agent"}}},
			want: auth.Choice{Name: "alice", Source: auth.SourceHostLabel},
		},
		{
			name: "the host label",
			url:  "https://alice.atomic.storage/workspaces/w/projects/p/code",
			want: auth.Choice{Name: "alice", Source: auth.SourceHostLabel},
		},
		{
			name: "the host label without the port",
			url:  "http://alice.localhost:8080/workspaces/w/projects/p/code",
			want: auth.Choice{Name: "alice", Source: auth.SourceHostLabel},
		},
		{
			name: "localhost gives no name",
			url:  "http://localhost:8080/workspaces/w/projects/p/code",
			want: auth.Choice{},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p, user := project(t, tc.url)
			sel := tc.sel
			sel.URLUser = user
			got := auth.ChooseName(p, sel)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.want.Source == auth.SourceOverride, got.Explicit())
		})
	}
}

func TestChooseNameZeroProject(t *testing.T) {
	require.Equal(t, auth.Choice{}, auth.ChooseName(serverurl.Project{}, auth.Selection{}))
	require.Equal(t,
		auth.Choice{Name: "bob", Source: auth.SourceURLUser},
		auth.ChooseName(serverurl.Project{}, auth.Selection{URLUser: "bob"}))
}

func TestNameSourceString(t *testing.T) {
	require.Equal(t, "none", auth.SourceNone.String())
	require.Equal(t, "override", auth.SourceOverride.String())
	require.Equal(t, "agent identity", auth.SourceAgentIdentity.String())
	require.Equal(t, "URL user", auth.SourceURLUser.String())
	require.Equal(t, "profile", auth.SourceProfile.String())
	require.Equal(t, "host label", auth.SourceHostLabel.String())
	require.Equal(t, "NameSource(42)", auth.NameSource(42).String())
}

func TestEnvNames(t *testing.T) {
	require.Equal(t, "ATOMIC_AGENT_IDENTITY", auth.EnvAgentIdentity)
	require.Equal(t, "ATOMIC_DELEGATION", auth.EnvDelegation)
}
