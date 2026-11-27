// Package serverurl turns an Atomic server, an org, a workspace and a
// project into the URLs the remote clients send requests to: the org host,
// the management apex, the project's /code endpoint, its sibling resources,
// its origin and the apex its tokens are minted for.
//
// Every other package builds those URLs through this one, so none of them
// carries userinfo, a query or fragment that a later request would corrupt,
// or a slug that changes the path. A [Server] is the configured server; a
// [Project] is one project's protocol endpoint, built with [NewProject] or
// read from a stored remote with [ParseProject].
//
// Nothing here resolves a name or touches the network.
//
// The rules are documented in docs/remote/transport.md#server-urls and
// docs/remote/endpoints/project.md#how-a-request-is-addressed.
package serverurl

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	// ErrScheme reports a URL whose scheme is not http or https.
	ErrScheme = errors.New("scheme is not http or https")

	// ErrQueryOrFragment reports a URL holding a query or a fragment,
	// which a request URL built from it by appending would corrupt
	// (docs/remote/transport.md#building-request-urls).
	ErrQueryOrFragment = errors.New("URL has a query or fragment")

	// ErrUserinfo reports a URL holding userinfo where none is allowed.
	ErrUserinfo = errors.New("URL has userinfo")

	// ErrHost reports a URL whose host is missing, is not ASCII, or cannot
	// carry an org label.
	ErrHost = errors.New("invalid host")

	// ErrSlug reports an org, workspace or project slug that would change
	// the URL it is put into.
	ErrSlug = errors.New("invalid slug")
)

// parse parses raw and checks it for the rules every URL in this package
// shares, except userinfo, which is the caller's to judge. The result is
// normalized as [check] describes.
func parse(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		// A *url.Error quotes its input, which may hold a password or a
		// token in the query, so only its cause is kept.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("serverurl: invalid URL: %w", err)
	}
	// url.Parse does not record a bare trailing '#', so it is looked for in
	// raw. A '#' can appear in a parsed URL only as the fragment's
	// delimiter.
	if strings.Contains(raw, "#") {
		return nil, urlError(ErrQueryOrFragment, u)
	}
	if err := check(u); err != nil {
		return nil, err
	}
	return u, nil
}

// check reports whether u, which this package owns, follows the shared
// rules, and normalizes it in place: the scheme and host are lowercased,
// except for an IPv6 zone, and a port that is empty or the scheme's default
// is dropped.
func check(u *url.URL) error {
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return urlError(ErrScheme, u)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return urlError(ErrQueryOrFragment, u)
	}
	if u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return urlError(ErrHost, u)
	}
	for i := 0; i < len(u.Host); i++ {
		if u.Host[i] >= 0x80 {
			return urlError(ErrHost, u)
		}
	}
	u.Host = lowerHost(u.Host)
	switch p := u.Port(); {
	case p == "":
		u.Host = strings.TrimSuffix(u.Host, ":")
	case (u.Scheme == "http" && p == "80") || (u.Scheme == "https" && p == "443"):
		u.Host = strings.TrimSuffix(u.Host, ":"+p)
	}
	return nil
}

// lowerHost lowercases host, a URL's host[:port], except for the zone of an
// IPv6 address, which names an interface and may be case-sensitive.
func lowerHost(host string) string {
	if !strings.HasPrefix(host, "[") {
		return strings.ToLower(host)
	}
	zone := strings.IndexByte(host, '%')
	end := strings.IndexByte(host, ']')
	if zone < 0 || end < zone {
		return strings.ToLower(host)
	}
	return strings.ToLower(host[:zone]) + host[zone:end] + strings.ToLower(host[end:])
}

// clone returns a copy of u that shares nothing mutable with it.
func clone(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	c := *u
	if u.User != nil {
		ui := *u.User
		c.User = &ui
	}
	return &c
}

// setEscapedPath sets u's path from its escaped form esc, which must be a
// valid escaping.
func setEscapedPath(u *url.URL, esc string) {
	p, err := url.PathUnescape(esc)
	if err != nil {
		// Every caller builds esc from an EscapedPath and escaped
		// segments, so this cannot happen.
		panic("serverurl: invalid escaped path " + esc)
	}
	u.Path = p
	u.RawPath = esc
}

// escapeSegments escapes each '/'-separated segment of p, leaving the
// separators as they are.
func escapeSegments(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// urlError reports u as refused with sentinel. The URL is quoted without its
// password, query or fragment, any of which may hold a secret.
func urlError(sentinel error, u *url.URL) error {
	shown := clone(u)
	shown.RawQuery, shown.ForceQuery = "", false
	shown.Fragment, shown.RawFragment = "", ""
	return fmt.Errorf("serverurl: %w: %q", sentinel, shown.Redacted())
}

func slugError(kind, slug, reason string) error {
	return fmt.Errorf("serverurl: %w: %s %q %s", ErrSlug, kind, slug, reason)
}
