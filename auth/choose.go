package auth

import (
	"net"
	"strconv"
	"strings"

	"github.com/z5labs/atomic-go/remote/serverurl"
)

// The environment variables the Rust client reads its agent's identity
// from. Nothing in this package reads them: only the program's edge reads the
// environment, and it passes the values in, EnvAgentIdentity's as
// [Selection.AgentIdentity] and EnvDelegation's as the explicit certificate
// that [delegation.Candidates] carries.
const (
	// EnvAgentIdentity names the identity to authenticate as on a machine
	// with no profile to bind it, such as a CI runner
	// (docs/remote/auth.md#choosing-an-identity).
	EnvAgentIdentity = "ATOMIC_AGENT_IDENTITY"

	// EnvDelegation holds an Atomic-Delegation header value handed to an
	// agent out of band (docs/remote/auth.md#choosing-a-certificate).
	EnvDelegation = "ATOMIC_DELEGATION"
)

// Profile is one configured server profile that binds an identity, as
// [ChooseName] reads it.
type Profile struct {
	// Host is the host of the profile's server URL, such as
	// "atomic.storage". It is compared without regard to case, and a port
	// on it is ignored.
	Host string

	// Identity is the name of the human identity bound to the profile. A
	// profile with no Identity binds nothing and is skipped, as the Rust
	// client skips it.
	Identity string

	// AgentIdentity is the name of the agent identity bound to the
	// profile, or "" when it has none. It is preferred over Identity for
	// the project protocol, and never used for management
	// ([ResolveForManagement]).
	AgentIdentity string

	// Active reports whether this is the active profile, the one the
	// configuration's default server selects. It wins a tie between
	// profiles whose hosts match equally well.
	Active bool
}

// Selection is everything [ChooseName] chooses an identity name from, other
// than the project URL itself.
type Selection struct {
	// Override is the name the caller asked for explicitly, as with the
	// Rust client's --identity, or "" for none. It is the only explicit
	// source: a name from anywhere else may fall back to the default
	// identity.
	Override string

	// AgentIdentity is the value of the EnvAgentIdentity variable, or ""
	// when it is unset. It is used only when it is not blank after
	// trimming white space, and is used trimmed.
	AgentIdentity string

	// URLUser is the username in the remote URL's userinfo, as in
	// http://bob@alice.localhost:8080/..., or "" for none.
	// [serverurl.ParseProject] returns it apart from the Project.
	URLUser string

	// Profiles are the configured server profiles that bind an identity,
	// in configuration order.
	Profiles []Profile
}

// NameSource says where [ChooseName] found an identity name.
type NameSource int

// The sources of an identity name, in the order [ChooseName] tries them.
const (
	// SourceNone means no name was found.
	SourceNone NameSource = iota

	// SourceOverride is [Selection.Override].
	SourceOverride

	// SourceAgentIdentity is [Selection.AgentIdentity].
	SourceAgentIdentity

	// SourceURLUser is [Selection.URLUser].
	SourceURLUser

	// SourceProfile is the best-matching [Profile].
	SourceProfile

	// SourceHostLabel is the first DNS label of the project's host.
	SourceHostLabel
)

// String returns the source's name, such as "override".
func (s NameSource) String() string {
	switch s {
	case SourceNone:
		return "none"
	case SourceOverride:
		return "override"
	case SourceAgentIdentity:
		return "agent identity"
	case SourceURLUser:
		return "URL user"
	case SourceProfile:
		return "profile"
	case SourceHostLabel:
		return "host label"
	default:
		return "NameSource(" + strconv.Itoa(int(s)) + ")"
	}
}

// Choice is the identity name [ChooseName] chose, and where it came from.
// The zero Choice is no name at all.
type Choice struct {
	// Name is the identity name, or "" when Source is SourceNone.
	Name string

	// Source is where Name came from.
	Source NameSource
}

// Explicit reports whether the name was asked for explicitly. An explicit
// name that matches no local identity is an error; any other name falls back
// to the default identity ([Resolve]).
func (c Choice) Explicit() bool {
	return c.Source == SourceOverride
}

// ChooseName chooses the identity name to authenticate to p as, as the Rust
// client does (docs/remote/auth.md#choosing-an-identity), taking the first
// of:
//
//  1. sel.Override, when it is not "";
//  2. sel.AgentIdentity, trimmed, when it is not blank;
//  3. sel.URLUser, when it is not "";
//  4. of the profiles whose host is p's host or a dot-boundary suffix of it,
//     the one with the longest host, the active one winning a tie and the
//     first in sel.Profiles winning a tie between profiles equally active;
//     its AgentIdentity when it has one, else its Identity;
//  5. the first DNS label of p's host, as alice from alice.atomic.storage.
//
// Hosts are compared in lower case and without their ports. A host with no
// dot, such as localhost, has no first label, so ChooseName can return the
// zero Choice. It is a pure function: it looks nothing up, so the name it
// returns may match no identity at all.
func ChooseName(p serverurl.Project, sel Selection) Choice {
	if sel.Override != "" {
		return Choice{Name: sel.Override, Source: SourceOverride}
	}
	if name := strings.TrimSpace(sel.AgentIdentity); name != "" {
		return Choice{Name: name, Source: SourceAgentIdentity}
	}
	if sel.URLUser != "" {
		return Choice{Name: sel.URLUser, Source: SourceURLUser}
	}
	host := ""
	if u := p.URL(); u != nil {
		host = strings.ToLower(u.Hostname())
	}
	if host == "" {
		return Choice{}
	}
	if name, ok := matchProfile(host, sel.Profiles); ok {
		return Choice{Name: name, Source: SourceProfile}
	}
	if label, _, found := strings.Cut(host, "."); found && label != "" {
		return Choice{Name: label, Source: SourceHostLabel}
	}
	return Choice{}
}

// matchProfile returns the identity bound by the profile whose host is the
// longest dot-boundary suffix of host, which must already be lower case.
func matchProfile(host string, profiles []Profile) (string, bool) {
	best := -1
	bestLen := 0
	for i, p := range profiles {
		if p.Identity == "" {
			continue
		}
		ph := profileHost(p.Host)
		if ph == "" || (host != ph && !strings.HasSuffix(host, "."+ph)) {
			continue
		}
		if best < 0 || len(ph) > bestLen || (len(ph) == bestLen && p.Active && !profiles[best].Active) {
			best, bestLen = i, len(ph)
		}
	}
	if best < 0 {
		return "", false
	}
	if p := profiles[best]; p.AgentIdentity != "" {
		return p.AgentIdentity, true
	}
	return profiles[best].Identity, true
}

// profileHost lowercases a profile's host and drops any port from it.
func profileHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(h, "[]")
}
