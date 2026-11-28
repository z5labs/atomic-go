// Package legacy is a client for the query-string reads of a project's
// protocol endpoint: the requests older servers and tools answer at the
// /code URL with a query such as ?view=dev&state=.
//
// Nothing calls these. The Atomic CLI moved every push, pull and clone to
// the sync protocol and the refs/views inventory, which package project
// covers, and current servers may not serve these requests at all
// (docs/remote/endpoints/project.md#which-methods-have-callers). The
// package exists for older servers and tools, and is kept apart from
// package project so that it can be dropped without touching the client
// people use.
//
// A [Client] reads a view's [State] with [Client.State], its log with
// [Client.Changelist] and its id with [Client.ID]; a view's tags with
// [Client.ListTags] and one tag with [Client.DownloadTag]; an attestation
// with [Client.DownloadAttestation]; provenance graphs with
// [Client.ProvenanceList] and [Client.DownloadProvenance]; and the
// remote's views with [Client.ListViews] and [Client.ViewManifest]. Every
// failed request comes back as a [*remote.Error] whose [remote.Kind] is
// the one the Atomic CLI maps the same failure to.
//
// The requests are documented in docs/remote/endpoints/project.md, under
// Reads, their bodies in docs/remote/types.md, under StateResponse and
// ChangelistEntry, and their failures in docs/remote/errors.md, under
// Project-protocol failures.
//
// # Host dependencies
//
// A Client reaches nothing outside its own memory except through the
// [project.Doer] and the [*slog.Logger] given to [New]. A request's
// deadline is its [context.Context]'s; the package sets no timeout, starts
// no timer and retries nothing.
package legacy

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/z5labs/atomic-go/remote/internal/wire"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/syncproto"
)

// acceptEncoding is the Accept-Encoding the Rust client sends on every
// request (docs/remote/transport.md#default-headers).
const acceptEncoding = "zstd, gzip, deflate"

// maxBodySize is the most bytes a response body is read into memory, or
// inflates to from a zstd Content-Encoding. It matches package project's
// largest pack size.
const maxBodySize = syncproto.MaxDecodedSize

// ErrInvalidArgument reports an argument or [Option] that [New] refuses.
// Every error New returns wraps it once for each argument it refused.
var ErrInvalidArgument = errors.New("legacy: invalid argument")

// options is what the [Option]s set.
type options struct {
	userAgent *string
}

// Option configures a [Client] built by [New].
type Option func(*options)

// WithUserAgent sets the User-Agent header sent on every request. The
// default is "atomic-" followed by the client version given to [New], as
// the Atomic CLI sends it. An empty ua sends no User-Agent at all, rather
// than net/http's default.
func WithUserAgent(ua string) Option {
	return func(o *options) {
		o.userAgent = &ua
	}
}

// Client sends the query-string reads to one project URL. It is safe for
// concurrent use as long as its [project.Doer] and logger are.
type Client struct {
	url       serverurl.Project
	requester *wire.Requester
	maxBody   int64
}

// New returns a Client for the project at u, sending through doer and
// logging to logger. clientVersion is the running client's version, such as
// "0.22.1": it is compared against a server's minimum version and, unless
// [WithUserAgent] says otherwise, sent in the User-Agent as
// "atomic-0.22.1".
//
// Nothing calls these requests any more, and a server may not serve them;
// see the package documentation.
//
// Every argument is required, and nothing falls back to a default: New
// refuses the zero [serverurl.Project], a nil doer, a nil logger and an
// empty clientVersion, and names every one it refused, along with any nil
// Option, in one error made with [errors.Join]. Each of them wraps
// [ErrInvalidArgument].
func New(u serverurl.Project, doer project.Doer, logger *slog.Logger, clientVersion string, opts ...Option) (*Client, error) {
	var errs []error
	missing := func(what string) {
		errs = append(errs, fmt.Errorf("%w: %s is required", ErrInvalidArgument, what))
	}
	if u.IsZero() {
		missing("the project URL")
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

	var o options
	for _, opt := range opts {
		if opt == nil {
			errs = append(errs, fmt.Errorf("%w: nil Option", ErrInvalidArgument))
			continue
		}
		opt(&o)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("legacy: New: %w", errors.Join(errs...))
	}

	ua := "atomic-" + clientVersion
	if o.userAgent != nil {
		ua = *o.userAgent
	}
	return newClient(u, doer, logger, clientVersion, ua, maxBodySize), nil
}

// newClient returns a Client whose arguments New has already checked,
// reading at most maxBody bytes of a body, whether sent as is or inflated
// from a zstd Content-Encoding.
func newClient(u serverurl.Project, doer project.Doer, logger *slog.Logger, clientVersion, ua string, maxBody int64) *Client {
	return &Client{
		url: u,
		requester: wire.NewRequester(doer, logger, wire.Profile{
			UserAgent:     ua,
			ClientVersion: clientVersion,
			Header:        http.Header{"Accept-Encoding": {acceptEncoding}},
			MaxDecoded:    maxBody,
		}),
		maxBody: maxBody,
	}
}

// URL returns the project URL the Client sends to.
func (c *Client) URL() serverurl.Project {
	return c.url
}
