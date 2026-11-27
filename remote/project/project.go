// Package project is a client for one project's protocol endpoint, the
// /code URL every object and ref of a project moves through.
//
// A [Client] pushes a [syncproto.Pack] with [Client.SyncPush] and pulls one
// with [Client.SyncPull], or streams a pull into an [ObjectSink] with
// [Client.SyncPullTo], so that memory stays bounded by the largest object
// rather than the whole pack. [Client.ListViewRefs] lists the remote's
// views, [Client.FetchProjectIdentity] reads the identity a project has
// bound and [Client.BindProjectIdentity] offers one. [Client.DownloadChange]
// fetches one change file by its hash, and [Client.DownloadChangeTo]
// streams one into an [io.Writer]. Every failed request comes back as a
// [*remote.Error] whose [remote.Kind] is the one the Atomic CLI maps the
// same failure to, except where a method says it deviates.
//
// A server that predates the identity endpoint is not a failure: both
// identity requests report it as unsupported, so a program can run against
// old and new servers alike.
//
// The requests are documented in docs/remote/endpoints/project.md, under
// Sync protocol, Reads, View inventory and Project identity, and their
// failures in docs/remote/errors.md, under Project-protocol failures and
// Decode failures and version mismatch.
//
// # Host dependencies
//
// A Client reaches nothing outside its own memory except through the
// [Doer] and the [*slog.Logger] given to [New]. The Doer is normally an
// [*http.Client] built at the program's edge, possibly with a transport
// that adds credentials. The Rust client's request and connect timeouts
// have no equivalent here: a request's deadline is its [context.Context]'s,
// and the package sets no timeout, starts no timer and retries nothing
// (docs/remote/transport.md#timeouts).
//
// # The pull's GET body
//
// [Client.SyncPull] sends its [syncproto.Wants] as the body of a GET, with
// an exact Content-Length, as the Rust client does. Whether every server
// and proxy accepts a GET with a body is an open question
// (docs/remote/transport.md#open-questions); the client sends exactly what
// Rust sends rather than guess at an alternative.
package project

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/z5labs/atomic-go/remote/internal/wire"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/syncproto"
)

// acceptEncoding is the Accept-Encoding the Rust client sends on every
// request (docs/remote/transport.md#default-headers).
const acceptEncoding = "zstd, gzip, deflate"

// ErrInvalidArgument reports an argument or [Option] that [New] refuses.
// Every error New returns wraps it once for each argument it refused.
var ErrInvalidArgument = errors.New("project: invalid argument")

// Doer sends an HTTP request and returns its response. An [*http.Client]
// is a Doer.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// config is what the options set.
type config struct {
	userAgent    *string
	maxPackSize  int64
	optionErrors []error
}

// Option configures a [Client] built by [New].
type Option func(*config)

// WithUserAgent sets the User-Agent header sent on every request. The
// default is "atomic-" followed by the client version given to [New], as
// the Atomic CLI sends it. An empty ua sends no User-Agent at all, rather
// than net/http's default.
func WithUserAgent(ua string) Option {
	return func(c *config) {
		c.userAgent = &ua
	}
}

// WithMaxPackSize sets the most bytes a pulled pack may inflate to, which
// also caps a response body sent with a zstd Content-Encoding and the
// change file [Client.DownloadChange] reads into memory. A request past
// any of them fails with a [remote.KindProtocol]. The default, and the largest size
// allowed, is [syncproto.MaxDecodedSize]; n must be positive and no larger,
// or [New] refuses it.
func WithMaxPackSize(n int64) Option {
	return func(c *config) {
		if n < 1 || n > syncproto.MaxDecodedSize {
			c.optionErrors = append(c.optionErrors, fmt.Errorf(
				"%w: WithMaxPackSize(%d) is not between 1 and %d", ErrInvalidArgument, n, syncproto.MaxDecodedSize))
			return
		}
		c.maxPackSize = n
	}
}

// Client sends the project protocol's requests to one project URL. It is
// safe for concurrent use as long as its [Doer] and logger are.
type Client struct {
	url           serverurl.Project
	requester     *wire.Requester
	codec         *syncproto.Codec
	logger        *slog.Logger
	clientVersion string
	maxPackSize   int64
}

// New returns a Client for the project at u, sending through doer and
// logging to logger. clientVersion is the running client's version, such as
// "0.22.1": it is compared against a server's minimum version, shown in a
// version-mismatch error and, unless [WithUserAgent] says otherwise, sent
// in the User-Agent as "atomic-0.22.1".
//
// Every argument is required, and nothing falls back to a default: New
// refuses the zero [serverurl.Project], a nil doer, a nil logger and an
// empty clientVersion, and names every one it refused, along with any
// Option it refused, in one error made with [errors.Join]. Each of them
// wraps [ErrInvalidArgument].
func New(u serverurl.Project, doer Doer, logger *slog.Logger, clientVersion string, opts ...Option) (*Client, error) {
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

	cfg := config{maxPackSize: syncproto.MaxDecodedSize}
	for _, opt := range opts {
		if opt == nil {
			errs = append(errs, fmt.Errorf("%w: nil Option", ErrInvalidArgument))
			continue
		}
		opt(&cfg)
	}
	errs = append(errs, cfg.optionErrors...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("project: New: %w", errors.Join(errs...))
	}

	ua := "atomic-" + clientVersion
	if cfg.userAgent != nil {
		ua = *cfg.userAgent
	}
	requester := wire.NewRequester(doer, logger, wire.Profile{
		UserAgent:     ua,
		ClientVersion: clientVersion,
		Header:        http.Header{"Accept-Encoding": {acceptEncoding}},
		MaxDecoded:    cfg.maxPackSize,
	})
	return &Client{
		url:           u,
		requester:     requester,
		codec:         syncproto.NewCodec(syncproto.WithMaxDecodedSize(cfg.maxPackSize)),
		logger:        logger,
		clientVersion: clientVersion,
		maxPackSize:   cfg.maxPackSize,
	}, nil
}

// URL returns the project URL the Client sends to.
func (c *Client) URL() serverurl.Project {
	return c.url
}
