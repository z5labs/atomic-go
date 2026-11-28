package delegation

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
)

// Scope is what a delegation allows its delegate to do
// (docs/remote/auth.md#scope). Each pattern list restricts one dimension of
// a [ResourceRef], and an empty list leaves its dimension unrestricted.
//
// The zero Scope grants nothing, since it lists no permissions. [NewScope]
// builds one that grants at least [PermissionRead], as Atomic's own scopes
// do.
type Scope struct {
	// Permissions are the permissions granted. A permission is granted when
	// one listed here implies it ([Permission.Implies]).
	Permissions []Permission

	// Servers are the server URLs the delegation is valid against. They
	// are compared exactly, after [NormalizeServer], and never as globs.
	Servers []string

	// Workspaces are glob patterns ([Match]) for workspace slugs.
	Workspaces []string

	// Projects are glob patterns for project paths, such as "acme/*".
	Projects []string

	// Views are glob patterns for view names.
	Views []string

	// MaxChanges caps how many changes the delegate may create, or is nil
	// for no cap. Nothing in the client enforces it.
	MaxChanges *uint64

	// Description describes the scope for people, or is "" for none.
	Description string
}

// NewScope returns a scope granting perms, in order and without repeats,
// and restricting nothing else. With no perms it grants
// [PermissionRead], the floor Atomic gives a scope built with none.
func NewScope(perms ...Permission) Scope {
	out := make([]Permission, 0, max(len(perms), 1))
	for _, p := range perms {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = append(out, PermissionRead)
	}
	return Scope{Permissions: out}
}

// Grants reports whether some permission in s implies p.
func (s Scope) Grants(p Permission) bool {
	return slices.ContainsFunc(s.Permissions, func(have Permission) bool {
		return have.Implies(p)
	})
}

// Allows reports whether s grants p on r: s must [Scope.Grants] p, and each
// dimension r sets must be allowed by s's list for it. An empty list allows
// any value. A server is allowed when it equals a listed server after
// [NormalizeServer] on both sides; a workspace, project or view when it
// [Match]es a listed pattern.
//
// A dimension r leaves "" is not checked at all, so it is allowed whatever
// s lists for it. A caller building r from a request must therefore never
// leave a dimension "" because the request names an empty one: an empty
// project in a path is not "no project", and passing it as "" fails open.
// Reject such a request before asking Allows.
//
// Allows checks the scope alone. A certificate's expiry, its revocation and
// the delegator's own grants are checked elsewhere.
func (s Scope) Allows(p Permission, r ResourceRef) bool {
	if !s.Grants(p) {
		return false
	}
	if r.Server != "" && len(s.Servers) > 0 {
		want := NormalizeServer(r.Server)
		if !slices.ContainsFunc(s.Servers, func(server string) bool {
			return NormalizeServer(server) == want
		}) {
			return false
		}
	}
	for _, d := range []struct {
		value    string
		patterns []string
	}{
		{r.Workspace, s.Workspaces},
		{r.Project, s.Projects},
		{r.View, s.Views},
	} {
		if d.value != "" && !matchesAny(d.patterns, d.value) {
			return false
		}
	}
	return true
}

// matchesAny reports whether value matches one of patterns, or patterns is
// empty.
func matchesAny(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	return slices.ContainsFunc(patterns, func(pattern string) bool {
		return Match(pattern, value)
	})
}

// wireScope is a Scope as Atomic writes it: every list, even an empty one,
// and max_changes and description only when set.
type wireScope struct {
	Permissions []Permission `json:"permissions"`
	Servers     []string     `json:"servers"`
	Workspaces  []string     `json:"workspaces"`
	Projects    []string     `json:"projects"`
	Views       []string     `json:"views"`
	MaxChanges  *uint64      `json:"max_changes,omitempty"`
	Description string       `json:"description,omitempty"`
}

// MarshalJSON writes s with Atomic's snake_case member names, in the order
// permissions, servers, workspaces, projects, views, max_changes and
// description. All five lists are written, as [] when empty; max_changes is
// left out when nil and description when "". A permission that is not one
// of the eight constants is an error wrapping [ErrUnknownPermission].
func (s Scope) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireScope{
		Permissions: s.Permissions,
		Servers:     s.Servers,
		Workspaces:  s.Workspaces,
		Projects:    s.Projects,
		Views:       s.Views,
		MaxChanges:  s.MaxChanges,
		Description: s.Description,
	})
}

// scopeMember returns the member a scope's member name sets, and false for
// a name a scope does not have. projects was once repository_patterns, and
// views was once view_patterns and before that stack_patterns.
func scopeMember(name string) (string, bool) {
	switch name {
	case "permissions", "servers", "workspaces", "projects", "views", "max_changes", "description":
		return name, true
	case "repository_patterns":
		return "projects", true
	case "view_patterns", "stack_patterns":
		return "views", true
	}
	return "", false
}

// UnmarshalJSON reads a scope object as Atomic does
// (docs/remote/auth.md#delegationscope). permissions is required, and each
// one must be a wire value ([Permission.UnmarshalText]). The other lists
// may be absent, which leaves them nil, as an empty list does, and
// projects and views may be
// written under their older names: repository_patterns, and view_patterns
// or stack_patterns. max_changes and description may be absent or null.
// Members a scope does not have are ignored.
//
// It refuses, with an error wrapping [ErrInvalidScope], input that is not
// exactly one JSON object, a member written twice, a member written under both its name
// and an older one (or under two older ones), a list that is null or holds
// anything but strings, a max_changes that is not an unsigned 64-bit
// integer, and a description that is not a string. s is unchanged on
// failure.
func (s *Scope) UnmarshalJSON(b []byte) error {
	var out Scope
	seen := make(map[string]string)
	dec := jsontext.NewDecoder(bytes.NewReader(b))
	tok, err := dec.ReadToken()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidScope, err)
	}
	if tok.Kind() != '{' {
		return fmt.Errorf("%w: not an object", ErrInvalidScope)
	}
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidScope, err)
		}
		name := tok.String()
		v, err := dec.ReadValue()
		if err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidScope, err)
		}
		member, ok := scopeMember(name)
		if !ok {
			continue
		}
		if prev, dup := seen[member]; dup {
			return fmt.Errorf("%w: %q and %q are the same member", ErrInvalidScope, prev, name)
		}
		seen[member] = name
		if err := out.decodeMember(member, v); err != nil {
			return fmt.Errorf("%w: member %q: %w", ErrInvalidScope, name, err)
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidScope, err)
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: data after the object", ErrInvalidScope)
	}
	if _, ok := seen["permissions"]; !ok {
		return fmt.Errorf("%w: member \"permissions\" is missing", ErrInvalidScope)
	}
	*s = out
	return nil
}

// decodeMember sets the member of s named member from v.
func (s *Scope) decodeMember(member string, v jsontext.Value) error {
	switch member {
	case "permissions":
		texts, err := decodeStrings(v)
		if err != nil {
			return err
		}
		var perms []Permission
		if len(texts) > 0 {
			perms = make([]Permission, len(texts))
		}
		for i, t := range texts {
			if err := perms[i].UnmarshalText([]byte(t)); err != nil {
				return err
			}
		}
		s.Permissions = perms
		return nil
	case "servers":
		return decodeList(v, &s.Servers)
	case "workspaces":
		return decodeList(v, &s.Workspaces)
	case "projects":
		return decodeList(v, &s.Projects)
	case "views":
		return decodeList(v, &s.Views)
	case "max_changes":
		if v.Kind() == 'n' {
			return nil
		}
		var n uint64
		if err := json.Unmarshal(v, &n); err != nil {
			return err
		}
		s.MaxChanges = &n
		return nil
	case "description":
		switch v.Kind() {
		case 'n':
			return nil
		case '"':
			return json.Unmarshal(v, &s.Description)
		}
		return errors.New("not a string")
	}
	panic("delegation: no decoder for scope member " + member)
}

func decodeList(v jsontext.Value, dst *[]string) error {
	list, err := decodeStrings(v)
	if err != nil {
		return err
	}
	*dst = list
	return nil
}

// decodeStrings decodes v as an array of strings, and an empty array as
// nil, so a scope round-trips to the value it was marshalled from. Unlike
// encoding/json, it refuses null, for the array and for each element, as
// Atomic does.
func decodeStrings(v jsontext.Value) ([]string, error) {
	if v.Kind() != '[' {
		return nil, errors.New("not an array")
	}
	var raw []jsontext.Value
	if err := json.Unmarshal(v, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, len(raw))
	for i, e := range raw {
		if e.Kind() != '"' {
			return nil, fmt.Errorf("element %d is not a string", i)
		}
		if err := json.Unmarshal(e, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
