package serverurl

import (
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// Server is a configured Atomic server: the URL of its profile and whether
// it serves a single org at its bare host.
//
// A multi-tenant server addresses each org on its own host, {org}.{host};
// a single-tenant server, one whose registration answered with mode
// "single", addresses its one org at the configured URL
// (docs/remote/transport.md#org-host).
type Server struct {
	// URL is the server profile's URL. Build it with [ParseServer], which
	// checks and normalizes it.
	URL *url.URL

	// SingleTenant reports that the server serves one org at URL.
	SingleTenant bool
}

// ParseServer parses raw as a server profile's URL.
//
// The scheme must be http or https ([ErrScheme]), and the URL may hold no
// query or fragment ([ErrQueryOrFragment]) and no userinfo ([ErrUserinfo]).
// The host must be present and ASCII ([ErrHost]). The scheme and host are
// lowercased and a default port is dropped; the path is kept as given.
func ParseServer(raw string, singleTenant bool) (Server, error) {
	u, err := parse(raw)
	if err != nil {
		return Server{}, err
	}
	if u.User != nil {
		return Server{}, urlError(ErrUserinfo, u.Redacted())
	}
	return Server{URL: u, SingleTenant: singleTenant}, nil
}

// OrgBase returns the base URL of org's management API and of the project
// URLs [NewProject] builds for it (docs/remote/transport.md#org-host).
//
// For a multi-tenant server it is {scheme}://{org}.{host}[:port], with the
// configured path dropped. org must be a non-empty run of ASCII letters,
// digits, '-' and '_', and is lowercased; anything else is refused with
// [ErrSlug], because it would change the host rather than name a label of
// it. A server whose host is an IP address cannot carry an org label and is
// refused with [ErrHost].
//
// For a single-tenant server it is the configured URL less one trailing
// '/', whatever org is.
//
// A Server whose URL is nil, or does not follow [ParseServer]'s rules, is
// refused with the error ParseServer would give.
func (s Server) OrgBase(org string) (*url.URL, error) {
	u, err := s.checked()
	if err != nil {
		return nil, err
	}
	if s.SingleTenant {
		if strings.HasSuffix(u.Path, "/") {
			setEscapedPath(u, strings.TrimSuffix(u.EscapedPath(), "/"))
		}
		return u, nil
	}
	if org == "" {
		return nil, slugError("org", org, "is empty")
	}
	for i := 0; i < len(org); i++ {
		if c := org[i]; !isLabelByte(c) {
			return nil, slugError("org", org, fmt.Sprintf("contains %q at offset %d", c, i))
		}
	}
	if _, err := netip.ParseAddr(u.Hostname()); err == nil {
		return nil, urlError(ErrHost, u.Redacted())
	}
	return &url.URL{Scheme: u.Scheme, Host: strings.ToLower(org) + "." + u.Host}, nil
}

// Apex returns the server's management apex, the URL its cross-org
// management calls go to and both management clients mint their tokens for:
// the configured URL less every trailing '/'
// (docs/remote/transport.md#apex-host, #management-api-base). It returns nil
// for a Server whose URL is nil.
func (s Server) Apex() *url.URL {
	u := clone(s.URL)
	if u == nil {
		return nil
	}
	if strings.HasSuffix(u.Path, "/") {
		setEscapedPath(u, strings.TrimRight(u.EscapedPath(), "/"))
	}
	return u
}

// checked returns a checked and normalized copy of s.URL.
func (s Server) checked() (*url.URL, error) {
	if s.URL == nil {
		return nil, fmt.Errorf("serverurl: %w: server has no URL", ErrHost)
	}
	u := clone(s.URL)
	if err := check(u); err != nil {
		return nil, err
	}
	if u.User != nil {
		return nil, urlError(ErrUserinfo, u.Redacted())
	}
	return u, nil
}

func isLabelByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '-' || c == '_'
}
