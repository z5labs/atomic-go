// Package wire is the request pipeline the project, legacy and storage
// clients share, so that a request from any of them puts on the wire the
// bytes the Rust CLI does.
//
// A [Requester] sends a [Request] through a [Doer] with the client's default
// headers ([Profile]), an exact Content-Length on every method, GET included,
// and the query exactly as a [Query] built it. Before the status is looked
// at, it checks the response's [remote.MinVersionHeader] and logs an upgrade
// warning, and it decodes the response's Content-Encoding. [ErrorBody] and
// [StatusError] turn an unexpected status into a [*remote.Error].
//
// # Deliberate deviations from the Rust client
//
// The Rust client advertises zstd but cannot decode it
// (docs/remote/transport.md#compression); a Requester decodes it.
//
// Text bodies are never transcoded. A body is read as bytes and treated as
// UTF-8 whatever charset its Content-Type names, where reqwest's text()
// decodes with that charset (docs/remote/transport.md#reading-responses).
// Servers send UTF-8, so the two agree on every body a server is expected to
// send, and a body in another charset comes back byte for byte rather than
// reinterpreted.
//
// # Host dependencies
//
// The only host dependencies are the [Doer] and the [*slog.Logger] given to
// [NewRequester]. Deadlines come from the request's [context.Context]; the
// package sets no timeout, starts no timer and retries nothing.
package wire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/z5labs/atomic-go/remote"
)

// Doer sends an HTTP request and returns its response. An [*http.Client]
// is a Doer.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Profile is what a client puts on every request it sends.
type Profile struct {
	// UserAgent is the User-Agent header, such as "atomic-0.22.1". An
	// empty UserAgent sends no User-Agent at all, rather than net/http's
	// default.
	UserAgent string

	// ClientVersion is the running client's version, compared against a
	// response's [remote.MinVersionHeader]. When it is empty no upgrade
	// warning is ever logged.
	ClientVersion string

	// Header holds the default headers sent on every request, such as
	// Accept-Encoding and credentials. A header the [Request] sets itself
	// replaces the default of the same name. A User-Agent here is ignored;
	// UserAgent decides it.
	Header http.Header

	// MaxDecoded is the most bytes a zstd-encoded response body may decode
	// to. Reading past it fails with an error wrapping
	// [zstdframe.ErrLimit].
	MaxDecoded int64
}

// Requester sends requests with one client's [Profile]. It is safe for
// concurrent use as long as its [Doer] and logger are.
type Requester struct {
	doer    Doer
	logger  *slog.Logger
	profile Profile
}

// NewRequester returns a Requester sending through doer and logging to
// logger. Both are required, and MaxDecoded must not be negative; it panics
// otherwise. The profile's Header is copied, so later changes to it do not
// reach the Requester.
func NewRequester(doer Doer, logger *slog.Logger, p Profile) *Requester {
	if doer == nil {
		panic("wire: NewRequester: nil Doer")
	}
	if logger == nil {
		panic("wire: NewRequester: nil *slog.Logger")
	}
	if p.MaxDecoded < 0 {
		panic(fmt.Sprintf("wire: NewRequester: negative MaxDecoded %d", p.MaxDecoded))
	}
	// Canonical keys, so a request header always replaces the default of
	// the same name however either was spelled.
	h := make(http.Header, len(p.Header))
	for name, values := range p.Header {
		k := http.CanonicalHeaderKey(name)
		h[k] = append(h[k], values...)
	}
	p.Header = h
	return &Requester{doer: doer, logger: logger, profile: p}
}

// Request is one request to send.
type Request struct {
	// Op names the client method sending it, such as "get_state". It
	// becomes [remote.Error.Op].
	Op string

	// Method is the HTTP method; empty means GET.
	Method string

	// URL is the request URL. Its RawQuery is sent exactly as it is, so
	// build it with a [Query]. Userinfo in it never reaches a log record
	// or an error.
	URL *url.URL

	// Header holds headers for this request alone. Each one replaces the
	// [Profile] default of the same name.
	Header http.Header

	// Body is the request body, sent with its exact length on any method.
	// A nil or empty Body sends none, and no Content-Length on a method
	// that usually has no body.
	Body []byte

	// Stream is a request body read as it is sent, of exactly StreamLength
	// bytes; the send fails if it holds fewer or more. A StreamLength of 0
	// sends no body and does not read Stream. At most one of Body and
	// Stream may be set. Stream is never closed, and net/http may still be
	// reading it after Do returns, when the server answers before the
	// upload ends: do not reuse or close it until the response body is
	// closed.
	Stream       io.Reader
	StreamLength int64
}

// Response is a response whose Content-Encoding has been decoded.
type Response struct {
	StatusCode int

	// Header is the response's header as it was received, including its
	// Content-Encoding and Content-Length.
	Header http.Header

	// Body is the decoded body. The caller must close it. A Read error,
	// including a body that fails to decode or a zstd body past
	// MaxDecoded, is the caller's to classify; the Rust client reports a
	// success body it cannot read as a [remote.KindConnectionFailed]
	// (docs/remote/errors.md#failures-without-a-usable-response).
	Body io.ReadCloser

	// URL is the request URL without its userinfo.
	URL string

	// MinVersion is the raw value of the response's
	// [remote.MinVersionHeader], or "" when it had none.
	MinVersion string

	// method is the request's method, for [StatusError].
	method string
}

// Do sends r and returns its response, whatever its status.
//
// Before the status is looked at, it logs one Warn record when the
// response's [remote.MinVersionHeader] names a version newer than the
// profile's ClientVersion, by [remote.NeedsUpgrade]. Nothing is logged when
// ClientVersion is empty or the header is absent or does not parse.
//
// The body's Content-Encoding is decoded: zstd, capped at the profile's
// MaxDecoded, gzip and x-gzip, deflate (read as zlib, as HTTP defines it) and
// identity. Any other coding fails with a [remote.KindProtocol].
//
// A request that cannot be sent, including one whose ctx is already done,
// fails with a [remote.KindConnectionFailed] wrapping the cause, whose
// ContextDone is set exactly when ctx has ended. A malformed Request fails
// with a [remote.KindOther].
func (q *Requester) Do(ctx context.Context, r Request) (*Response, error) {
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	if r.URL == nil {
		return nil, &remote.Error{Kind: remote.KindOther, Op: r.Op, Method: method, Message: "wire: request has no URL"}
	}
	shown := withoutUserinfo(r.URL)

	req, err := q.newRequest(ctx, method, r)
	if err != nil {
		return nil, &remote.Error{Kind: remote.KindOther, Op: r.Op, Method: method, URL: shown, Message: err.Error(), Err: err}
	}

	if err := ctx.Err(); err != nil {
		return nil, connectionFailed(ctx, r.Op, method, shown, err)
	}
	resp, err := q.doer.Do(req)
	if err != nil {
		return nil, connectionFailed(ctx, r.Op, method, shown, err)
	}

	minVersion := headerValue(resp.Header, remote.MinVersionHeader)
	if q.profile.ClientVersion != "" && remote.NeedsUpgrade(q.profile.ClientVersion, minVersion) {
		q.logger.WarnContext(ctx, "this server requires a newer Atomic CLI; run 'atomic update' to upgrade",
			slog.String("required_version", minVersion),
			slog.String("current_version", q.profile.ClientVersion),
			slog.String("url", shown),
		)
	}

	body, err := decodeBody(resp, q.profile.MaxDecoded)
	if err != nil {
		_ = resp.Body.Close()
		return nil, &remote.Error{
			Kind:             remote.KindProtocol,
			Op:               r.Op,
			Method:           method,
			URL:              shown,
			StatusCode:       resp.StatusCode,
			Message:          err.Error(),
			ServerMinVersion: minVersion,
			Err:              err,
		}
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       body,
		URL:        shown,
		MinVersion: minVersion,
		method:     method,
	}, nil
}

// newRequest builds the *http.Request for r, with its headers and a body
// of known length.
func (q *Requester) newRequest(ctx context.Context, method string, r Request) (*http.Request, error) {
	if r.Body != nil && r.Stream != nil {
		return nil, errors.New("wire: request has both a Body and a Stream")
	}
	if r.Stream != nil && r.StreamLength < 0 {
		return nil, fmt.Errorf("wire: negative StreamLength %d", r.StreamLength)
	}

	// The request is built with an empty URL and given a copy of r.URL,
	// rather than r.URL's text, so RawQuery is sent as built and no parse
	// error can carry the URL's userinfo into an error.
	req, err := http.NewRequestWithContext(ctx, method, "", nil)
	if err != nil {
		return nil, fmt.Errorf("wire: new request: %w", err)
	}
	u := *r.URL
	req.URL = &u
	req.Host = u.Host

	switch {
	case len(r.Body) > 0:
		b := r.Body
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(b)), nil
		}
		req.ContentLength = int64(len(b))
	case r.Stream != nil && r.StreamLength > 0:
		// Hiding any Close keeps the caller's reader open; the
		// transport closes only this wrapper.
		req.Body = io.NopCloser(r.Stream)
		req.ContentLength = r.StreamLength
	default:
		// No body at all, rather than an empty one: net/http reads a
		// non-nil body with a ContentLength of 0 as one of unknown length,
		// which it would chunk.
		req.Body = nil
		req.ContentLength = 0
	}

	h := q.profile.Header.Clone()
	if h == nil {
		h = make(http.Header)
	}
	// Set directly, so an empty value is kept: net/http then sends no
	// User-Agent rather than its own.
	h["User-Agent"] = []string{q.profile.UserAgent}
	for name, values := range r.Header {
		h[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	req.Header = h
	return req, nil
}

// connectionFailed is the error for a request that could not be sent.
func connectionFailed(ctx context.Context, op, method, url string, err error) *remote.Error {
	return &remote.Error{
		Kind:        remote.KindConnectionFailed,
		Op:          op,
		Method:      method,
		URL:         url,
		ContextDone: ctx.Err() != nil,
		Err:         err,
	}
}

// withoutUserinfo returns u as text with any userinfo removed.
func withoutUserinfo(u *url.URL) string {
	c := *u
	c.User = nil
	return c.String()
}

// headerValue returns the first value of the header name, matching names
// case-insensitively even in a header whose keys are not canonical.
func headerValue(h http.Header, name string) string {
	if v, ok := h[http.CanonicalHeaderKey(name)]; ok && len(v) > 0 {
		return v[0]
	}
	for k, v := range h {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// ErrorBody reads an error response's body as text: at most
// [remote.MaxErrorBody] bytes of it, not transcoded. It returns "" when the
// body cannot be read, as the Rust client does. The caller still closes
// resp.Body.
func ErrorBody(resp *Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, remote.MaxErrorBody))
	if err != nil {
		return ""
	}
	return string(b)
}

// StatusError returns the error for a response whose status op does not
// handle otherwise: a [remote.KindAuthenticationFailed] for 401 and 403 and
// a [remote.KindHTTP] for anything else, carrying body, from [ErrorBody], as
// both its Message and its Body.
func StatusError(op string, resp *Response, body string) *remote.Error {
	kind := remote.KindHTTP
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		kind = remote.KindAuthenticationFailed
	}
	return &remote.Error{
		Kind:             kind,
		Op:               op,
		Method:           resp.method,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		Message:          body,
		Body:             body,
		ServerMinVersion: resp.MinVersion,
	}
}
