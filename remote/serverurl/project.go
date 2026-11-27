package serverurl

import (
	"fmt"
	"net/url"
	"strings"
)

// Project is the URL of one project's protocol endpoint, the URL every
// object and ref of the project moves through
// (docs/remote/transport.md#project-url). Build one with [NewProject] or
// [ParseProject].
//
// A Project never holds userinfo, a query or a fragment, so the request
// URLs derived from it carry none either.
//
// The zero value is not a usable Project: [Project.IsZero] reports it,
// [Project.String] returns "" and every method returning a URL returns nil,
// so a constructor taking a Project can refuse it.
type Project struct {
	u *url.URL
}

// NewProject returns the project URL that atomic project create, init and
// show build:
//
//	{orgBase}/workspaces/{workspace}/projects/{project}/code
//
// orgBase is normally the result of [Server.OrgBase]; one trailing '/' on
// its path is dropped. It must follow [ParseServer]'s rules.
//
// workspace and project are refused with [ErrSlug] when they are empty, '.'
// or '..', or hold '/', '\\', '?', '#', '%', an ASCII control or a non-ASCII
// byte: each of those would add a path segment, remove one, end the path, or
// reach the server as something other than the slug.
func NewProject(orgBase *url.URL, workspace, project string) (Project, error) {
	if orgBase == nil {
		return Project{}, fmt.Errorf("serverurl: %w: org base URL is nil", ErrHost)
	}
	u := clone(orgBase)
	if err := check(u); err != nil {
		return Project{}, err
	}
	if u.User != nil {
		return Project{}, urlError(ErrUserinfo, u)
	}
	if err := checkSlug("workspace", workspace); err != nil {
		return Project{}, err
	}
	if err := checkSlug("project", project); err != nil {
		return Project{}, err
	}
	setEscapedPath(u, strings.TrimSuffix(u.EscapedPath(), "/")+
		"/workspaces/"+url.PathEscape(workspace)+
		"/projects/"+url.PathEscape(project)+
		"/code")
	return Project{u: u}, nil
}

// ParseProject parses raw as a stored project URL, such as a repository's
// origin remote.
//
// Any path is accepted, the legacy /tenant/{t}/portfolio/{p}/project/{name}/code
// shape included, though [NewProject] never builds it. The scheme must be
// http or https ([ErrScheme]), and a query or fragment is refused
// ([ErrQueryOrFragment]). The scheme and host are lowercased, a default port
// is dropped and an empty path is written as "/".
//
// The host must be ASCII, and is otherwise refused with [ErrHost]. That is
// deliberately stricter than the Rust client, whose WHATWG parse accepts an
// internationalized host and sends it as punycode: this package never
// converts a host, so it refuses one it would have to.
//
// Userinfo, which the Rust client accepts as a way to choose an identity, is
// returned separately and is not part of the Project; it is nil when raw has
// none.
func ParseProject(raw string) (Project, *url.Userinfo, error) {
	u, err := parse(raw)
	if err != nil {
		return Project{}, nil, err
	}
	user := u.User
	u.User = nil
	if u.Path == "" {
		u.Path, u.RawPath = "/", ""
	}
	return Project{u: u}, user, nil
}

// IsZero reports whether p is the zero Project.
func (p Project) IsZero() bool {
	return p.u == nil
}

// String returns the project URL, or "" for the zero Project.
func (p Project) String() string {
	if p.u == nil {
		return ""
	}
	return p.u.String()
}

// URL returns a copy of the project URL, or nil for the zero Project.
func (p Project) URL() *url.URL {
	return clone(p.u)
}

// WithRawQuery returns the project URL with q as its query, verbatim: q is
// neither escaped nor checked, so the caller builds it by the rules on
// docs/remote/transport.md#building-request-urls. An empty q gives the URL
// with no query. It returns nil for the zero Project.
func (p Project) WithRawQuery(q string) *url.URL {
	u := clone(p.u)
	if u == nil {
		return nil
	}
	u.RawQuery = q
	return u
}

// Resource returns the URL of a resource beside /code under the project
// path, such as refs/views: the project URL with one trailing '/' removed,
// then a trailing /code removed, then '/' and path appended
// (docs/remote/transport.md#sibling-resources). Each segment of path is
// escaped as a path segment. It returns nil for the zero Project.
//
// path names a resource of the protocol, such as the constant "refs/views";
// it is not a slug and is not checked as one. A "." or ".." segment is kept
// and a server resolving it would address something outside the project, so
// never build path from input.
func (p Project) Resource(path string) *url.URL {
	u := clone(p.u)
	if u == nil {
		return nil
	}
	esc := strings.TrimSuffix(u.EscapedPath(), "/")
	esc = strings.TrimSuffix(esc, "/code")
	setEscapedPath(u, esc+"/"+escapeSegments(path))
	return u
}

// Origin returns the project URL's scheme, host and port, with no path: the
// base URL signer-key discovery uses
// (docs/remote/transport.md#management-api-base). It returns nil for the
// zero Project.
func (p Project) Origin() *url.URL {
	if p.u == nil {
		return nil
	}
	return &url.URL{Scheme: p.u.Scheme, Host: p.u.Host}
}

// TokenApex returns the apex that push, pull and clone mint their token
// for: the scheme, the host with everything up to and including its first
// '.' removed when it holds one, and the port; the path is dropped
// (docs/remote/transport.md#apex-host).
//
// It reproduces the Rust client exactly, including for hosts where the
// result is not the server's apex: a single-tenant server's project URL has
// no org label to remove, and an IPv4 host loses its first octet. Callers
// that know the audience a token is for pass it explicitly rather than rely
// on this. An IPv6 host holds no '.' and is kept whole, and so is a host
// whose only '.' is its last byte, such as "localhost.", rather than leave an
// empty host. It returns nil for the zero Project.
func (p Project) TokenApex() *url.URL {
	if p.u == nil {
		return nil
	}
	host := p.u.Host
	if !strings.HasPrefix(host, "[") {
		name, port := p.u.Hostname(), p.u.Port()
		if _, rest, ok := strings.Cut(name, "."); ok && rest != "" {
			host = rest
			if port != "" {
				host += ":" + port
			}
		}
	}
	return &url.URL{Scheme: p.u.Scheme, Host: host}
}

// Host returns the project URL's host, with its port if it has one. It
// returns "" for the zero Project.
func (p Project) Host() string {
	if p.u == nil {
		return ""
	}
	return p.u.Host
}

// RepoName returns the repository name the Rust client infers from the
// path: the segment before the first "code" or ".atomic" segment, otherwise
// the last segment that is neither, with a ".git" suffix removed
// (docs/remote/transport.md#base-url-and-repository-name). The segments are
// those of the escaped path, as in the Rust client, so an escaped '/' does
// not split a segment and the name is returned as escaped: "my%20repo", not
// "my repo". It returns "" when the path has no such segment, and for the
// zero Project.
func (p Project) RepoName() string {
	if p.u == nil {
		return ""
	}
	var segs []string
	for _, s := range strings.Split(p.u.EscapedPath(), "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	isMarker := func(s string) bool { return s == "code" || s == ".atomic" }

	name := ""
	for i, s := range segs {
		if isMarker(s) {
			if i > 0 {
				name = segs[i-1]
			}
			break
		}
	}
	if name == "" {
		for i := len(segs) - 1; i >= 0; i-- {
			if !isMarker(segs[i]) {
				name = segs[i]
				break
			}
		}
	}
	return strings.TrimSuffix(name, ".git")
}

// checkSlug reports whether s may be put into a path as one segment.
func checkSlug(kind, s string) error {
	switch s {
	case "":
		return slugError(kind, s, "is empty")
	case ".", "..":
		return slugError(kind, s, "is a relative path element")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c >= 0x7f || strings.IndexByte(`/\?#%`, c) >= 0 {
			return slugError(kind, s, fmt.Sprintf("contains %q at offset %d", c, i))
		}
	}
	return nil
}
