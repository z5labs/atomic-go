// Package storage is a client for the atomic-storage management API: the
// JSON requests that create, read, update and delete workspaces, projects,
// identities, organizations, teams and the other management resources
// of a hosted Atomic server.
//
// A [Client] sends every request to one base URL, which is an org base, the
// apex or a project URL's origin; which one a request needs is the caller's
// to know (docs/remote/endpoints/storage.md#base-urls). Every request goes
// through one pipeline: the same headers on every method, a JSON body
// marshalled without HTML escaping, the server's minimum-version advice
// checked before the status, and the response's envelope unwrapped
// (docs/remote/endpoints/storage.md#response-envelope). Every failed request comes back as a [*remote.Error]; see
// [Client] for how each failure maps to a [remote.Kind].
//
// [Register] sends the one management request that does not go through a
// Client: it registers a signed identity with a server, sending nothing but
// the JSON body (docs/remote/endpoints/storage.md#execute).
//
// # Paths
//
// A slug, ID or key that goes into a request path is refused, with an error
// wrapping [ErrInvalidArgument] and before anything is sent, when the Rust
// client's URL parse would turn it into another path: a '/' that adds a
// segment, a '?' that ends the path, or a ".." that removes the segment
// before it (docs/remote/transport.md#building-request-urls). Percent
// escapes are sent as they are, as the Rust client sends them, so what a
// server that decodes them before routing sees is the server's to decide.
//
// # Host dependencies
//
// A Client reaches nothing outside its own memory except through the [Doer]
// and the [*slog.Logger] given to [New]. The Doer carries the credentials:
// at the program's edge it is an [*http.Client] whose transport adds a
// bearer token minted for the management audience. The package sets no
// timeout, starts no timer and retries nothing; a request's deadline is its
// [context.Context]'s.
//
// The API is documented in docs/remote/endpoints/storage.md and its
// failures in docs/remote/errors.md#storage-api-failures.
package storage

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/z5labs/atomic-go/remote/internal/wire"
)

const (
	// contentType is the Content-Type the Rust StorageClient sends on every
	// request, including those with no body
	// (docs/remote/endpoints/storage.md#request-headers).
	contentType = "application/json"

	// acceptEncoding is the header reqwest generates for the gzip and
	// deflate features StorageClient's crate enables
	// (docs/remote/transport.md#compression).
	acceptEncoding = "gzip,deflate"

	// MaxResponseBody is the most bytes of a successful response's decoded
	// body a [Client] reads. A larger body fails with a
	// [remote.KindProtocol] rather than being read into memory whole. The
	// Rust client sets no limit.
	MaxResponseBody = 64 << 20
)

// ErrInvalidArgument reports an argument that the package refuses before
// anything is sent: an argument or [Option] [New] refuses, or a path
// segment that would address a different resource than the one named.
var ErrInvalidArgument = errors.New("storage: invalid argument")

// Doer sends an HTTP request and returns its response. An [*http.Client] is
// a Doer.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// config is what the options set.
type config struct {
	userAgent    *string
	optionErrors []error
}

// Option configures a [Client] built by [New].
type Option func(*config)

// WithUserAgent sets the User-Agent header sent on every request. The
// default is "atomic/" followed by the client version given to [New], as
// the Rust StorageClient sends it. Every request carries a User-Agent, so
// [New] refuses an empty ua.
func WithUserAgent(ua string) Option {
	return func(c *config) {
		if ua == "" {
			c.optionErrors = append(c.optionErrors, fmt.Errorf("%w: WithUserAgent(\"\"): the User-Agent must not be empty", ErrInvalidArgument))
			return
		}
		c.userAgent = &ua
	}
}

// Client sends management API requests to one base URL. It is safe for
// concurrent use as long as its [Doer] and logger are.
//
// # Failures
//
// A response whose status is outside 2xx is a [remote.KindHTTP], whatever
// the status, as the Rust client's HttpError is. Its Code and Message are
// the server's: from an envelope whose boolean success sits beside an error
// holding string code and message members, or else from a bare
// {"code": …, "message": …} with both strings. Any other body, plain text
// included, gives the Message "HTTP {status}"
// (docs/remote/errors.md#storage-api-json). Body holds at most
// [remote.MaxErrorBody] bytes of the body, and Code and Message are read
// from those bytes alone, so an error body longer than that gives
// "HTTP {status}" where the Rust client, which reads it whole, finds them. The [remote.Error] classifiers
// and sentinels then work from the status: a 401 matches
// [remote.ErrUnauthorized] and is [remote.Error.IsAuth], a 404 matches
// [remote.ErrNotFound], and a 409 matches [remote.ErrConflict].
//
// A 2xx response to a DELETE succeeds whatever its body. Any other 2xx
// response must be an envelope saying success: true with non-null data;
// otherwise it is a [remote.KindProtocol] wrapping [ErrNoData], a
// [*FailureError] or the JSON error. The Rust client reports these as Other,
// which says nothing a caller can act on.
//
// A request that cannot be sent, or a 2xx body that cannot be read, is a
// [remote.KindConnectionFailed] whose cause is the failure, so a cancelled
// context matches [context.Canceled]. The Rust client reports these as
// Other, which is never retryable (docs/remote/endpoints/storage.md#error-mapping).
type Client struct {
	// base is the base URL with its trailing '/' characters trimmed, and
	// basePath its escaped path, trimmed the same way.
	base     *url.URL
	basePath string

	requester *wire.Requester
}

// New returns a Client sending to base through doer and logging to logger.
// clientVersion is the running client's version, such as "0.22.1": it is
// compared against a server's minimum version and, unless [WithUserAgent]
// says otherwise, sent in the User-Agent as "atomic/0.22.1".
//
// base must be an absolute http or https URL with a host and no userinfo,
// query or fragment, and its path must hold no "." or ".." segment. Trailing
// '/' characters are trimmed from it, as the Rust client trims them, so
// "https://example.com/" and "https://example.com" are the same base.
//
// Every argument is required, and nothing falls back to a default: New
// refuses a nil base, a nil doer, a nil logger and an empty clientVersion,
// and names every one it refused, along with any problem with base and any
// Option it refused, in one error made with [errors.Join]. Each of them
// wraps [ErrInvalidArgument].
func New(base *url.URL, doer Doer, logger *slog.Logger, clientVersion string, opts ...Option) (*Client, error) {
	var errs []error
	missing := func(what string) {
		errs = append(errs, fmt.Errorf("%w: %s is required", ErrInvalidArgument, what))
	}
	var trimmed *url.URL
	if base == nil {
		missing("the base URL")
	} else {
		var baseErrs []error
		trimmed, baseErrs = checkBase("the base URL", base)
		errs = append(errs, baseErrs...)
	}
	if doer == nil {
		missing("the Doer")
	}
	if logger == nil {
		missing("the *slog.Logger")
	}
	if clientVersion == "" {
		missing("the client version")
	}

	var cfg config
	for _, opt := range opts {
		if opt == nil {
			errs = append(errs, fmt.Errorf("%w: nil Option", ErrInvalidArgument))
			continue
		}
		opt(&cfg)
	}
	errs = append(errs, cfg.optionErrors...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("storage: New: %w", errors.Join(errs...))
	}

	ua := "atomic/" + clientVersion
	if cfg.userAgent != nil {
		ua = *cfg.userAgent
	}
	requester := wire.NewRequester(doer, logger, wire.Profile{
		UserAgent:     ua,
		ClientVersion: clientVersion,
		Header: http.Header{
			"Content-Type":    {contentType},
			"Accept-Encoding": {acceptEncoding},
		},
		MaxDecoded: MaxResponseBody,
	})
	return &Client{
		base:      trimmed,
		basePath:  trimmed.EscapedPath(),
		requester: requester,
	}, nil
}

// checkBase returns a copy of base with its trailing '/' characters
// trimmed, and every reason it cannot be a base URL. name is how each
// reason refers to base, such as "the base URL".
func checkBase(name string, base *url.URL) (*url.URL, []error) {
	var errs []error
	refuse := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: %s %s", ErrInvalidArgument, name, fmt.Sprintf(format, args...)))
	}
	switch strings.ToLower(base.Scheme) {
	case "http", "https":
	case "":
		refuse("is not absolute")
	default:
		refuse("has scheme %q, not http or https", base.Scheme)
	}
	if base.Opaque != "" {
		refuse("is opaque")
	}
	if base.Host == "" {
		refuse("has no host")
	}
	if base.User != nil {
		refuse("has userinfo")
	}
	if base.RawQuery != "" || base.ForceQuery {
		refuse("has a query")
	}
	if base.Fragment != "" || base.RawFragment != "" {
		refuse("has a fragment")
	}

	// The escaped path is what is sent, so it is the one trimmed, and the
	// unescaped Path is derived from it.
	u := *base
	u.Scheme = strings.ToLower(u.Scheme)
	escaped := strings.TrimRight(base.EscapedPath(), "/")
	if escaped != "" && !strings.HasPrefix(escaped, "/") {
		// A URL built by hand may have a rootless path, which net/http
		// would send as a malformed request target.
		escaped = "/" + escaped
	}
	p, err := url.PathUnescape(escaped)
	if err != nil {
		refuse("has a path that does not unescape: %v", err)
	}
	u.Path = p
	u.RawPath = escaped
	if err := checkDotSegments(escaped); err != nil {
		refuse("path %q: %v", escaped, err)
	}
	return &u, errs
}

// BaseURL returns a copy of the base URL the Client sends to, with its
// trailing '/' characters trimmed.
func (c *Client) BaseURL() *url.URL {
	u := *c.base
	return &u
}
