package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Transport is an [http.RoundTripper] that attaches [Credentials] to the
// requests bound for one origin and passes every other request through
// unchanged. Build one with [NewTransport]; the zero Transport is not
// usable.
//
// A Transport holds no mutable state, so one may be shared by any number of
// goroutines, as long as its wrapped transport and its Credentials may be.
type Transport struct {
	base   http.RoundTripper
	creds  Credentials
	origin originKey
}

// originKey is a URL's scheme, lower-case host and explicit port, which is
// what two URLs must share for a request to one to be a request to the
// other.
type originKey struct {
	scheme string
	host   string
	port   string
}

// NewTransport returns a [Transport] that sends every request through base
// and attaches creds to those bound for origin.
//
// All three are required. Pass [NoCredentials] for an anonymous caller; a
// nil base is never replaced with [http.DefaultTransport]. origin must be an
// absolute http or https URL with a host; only its scheme, host and port are
// kept, the host is compared without regard to case, and a URL that leaves
// out the port matches one that spells out the scheme's default, so
// https://example.com and https://EXAMPLE.com:443/any/path name the same
// origin. NewTransport names every argument it refused in one error made
// with [errors.Join], each wrapping [ErrInvalidArgument].
//
// origin is read, never kept, so the caller may change it afterwards.
func NewTransport(base http.RoundTripper, creds Credentials, origin *url.URL) (*Transport, error) {
	var errs []error
	missing := func(what string) {
		errs = append(errs, fmt.Errorf("%w: %s is required", ErrInvalidArgument, what))
	}
	if base == nil {
		missing("the base RoundTripper")
	}
	if creds == nil {
		missing("the Credentials")
	}
	var key originKey
	if origin == nil {
		missing("the origin")
	} else {
		var ok bool
		key, ok = originOf(origin)
		if !ok {
			errs = append(errs, fmt.Errorf("%w: the origin must be an absolute http or https URL with a host", ErrInvalidArgument))
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("auth: NewTransport: %w", errors.Join(errs...))
	}
	return &Transport{base: base, creds: creds, origin: key}, nil
}

// RoundTrip sends req through the wrapped transport.
//
// A request bound for the Transport's origin is cloned, with req's context,
// the clone is passed to [Credentials.Attach], and the clone is sent. A
// request bound for any other origin, including one whose URL has no host,
// is passed to the wrapped transport as it is. Either way req itself is
// never modified.
//
// If Attach fails, RoundTrip returns its error without calling the wrapped
// transport, and closes req.Body when there is one, as the
// [http.RoundTripper] contract requires of a round trip that fails.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return t.base.RoundTrip(req)
	}
	key, ok := originOf(req.URL)
	if !ok || key != t.origin {
		return t.base.RoundTrip(req)
	}

	out := req.Clone(req.Context())
	if err := t.creds.Attach(out); err != nil {
		if req.Body != nil {
			// The round trip has already failed; a Close error is the
			// body's to report.
			_ = req.Body.Close()
		}
		return nil, fmt.Errorf("auth: attaching credentials: %w", err)
	}
	return t.base.RoundTrip(out)
}

// originOf returns u's scheme, lower-case host and port, with the scheme's
// default port filled in when u leaves it out. It reports false for a URL
// that is not http or https or has no host.
func originOf(u *url.URL) (originKey, bool) {
	scheme := strings.ToLower(u.Scheme)
	var defaultPort string
	switch scheme {
	case "http":
		defaultPort = "80"
	case "https":
		defaultPort = "443"
	default:
		return originKey{}, false
	}
	host := u.Hostname()
	if host == "" {
		return originKey{}, false
	}
	port := u.Port()
	if port == "" {
		port = defaultPort
	}
	return originKey{scheme: scheme, host: lowerHost(host), port: port}, true
}

// lowerHost lowercases a hostname, except for the zone of an IPv6 address,
// which names an interface and may be case-sensitive.
func lowerHost(host string) string {
	if i := strings.IndexByte(host, '%'); i >= 0 {
		return strings.ToLower(host[:i]) + host[i:]
	}
	return strings.ToLower(host)
}
