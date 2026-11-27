// Package remotetest provides in-memory fakes for testing HTTP clients of an
// Atomic remote without a network.
//
// [Transport] and [Client] serve an [http.Handler] in memory, through a real
// HTTP/1.1 encoding and decoding of the request and the response, so a client
// under test sees the same bytes it would see from a server. [RoundTripFunc]
// adapts a function into an [http.RoundTripper] for one-off fakes, and
// [LogRecorder] is a [slog.Handler] that keeps every record it is given.
//
// Nothing in this package opens a socket, starts a goroutine or reads the
// clock.
package remotetest

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
)

// Transport returns an [http.RoundTripper] that serves every request with h,
// in memory.
//
// Each round trip encodes the request with [http.Request.Write], decodes it
// with [http.ReadRequest] and serves it through an [httptest.ResponseRecorder]
// with the caller's context. The response is then encoded with
// [http.Response.Write] and decoded with [http.ReadResponse], with
// [http.Response.Request] set to the caller's request. The handler therefore
// sees the request as a server would, with its RequestURI byte for byte, and
// the caller sees the response as a client would. Content-Encoding is passed
// through untouched in both directions; nothing is compressed or decompressed.
//
// A request whose context is already done returns the context's error
// without calling h. The caller's request is never mutated, and its body is
// closed on every path, as the [http.RoundTripper] contract requires.
func Transport(h http.Handler) http.RoundTripper {
	return handlerTransport{handler: h}
}

// Client returns an [http.Client] whose Transport is [Transport](h). It sets
// no Timeout: deadlines come from the request's context.
func Client(h http.Handler) *http.Client {
	return &http.Client{Transport: Transport(h)}
}

// RoundTripFunc adapts a function into an [http.RoundTripper].
type RoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls f(req).
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type handlerTransport struct {
	handler http.Handler
}

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := newOnceCloser(req.Body)

	ctx := req.Context()
	if err := ctx.Err(); err != nil {
		body.close()
		return nil, err
	}

	// Write a shallow copy whose body closes at most once, so the caller's
	// request is left as it was and the body can be closed on every path
	// below without closing it twice.
	out := req.Clone(ctx)
	if body != nil {
		out.Body = body
	}

	var wire bytes.Buffer
	if err := out.Write(&wire); err != nil {
		body.close()
		return nil, err
	}
	// Request.Write closes the body once it has been sent; this makes that
	// a guarantee rather than an implementation detail.
	body.close()

	served, err := http.ReadRequest(bufio.NewReader(&wire))
	if err != nil {
		return nil, err
	}
	served = served.WithContext(ctx)

	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, served)

	result := rec.Result()
	if result.ContentLength < 0 {
		// A handler that sets no Content-Length and writes a short body
		// gets one from a real server, which buffers the body; without one
		// Response.Write would fall back to closing the connection to mark
		// the end of the body.
		result.ContentLength = int64(rec.Body.Len())
	}

	wire.Reset()
	if err := result.Write(&wire); err != nil {
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(&wire), req)
}

// onceCloser passes reads through to its body and closes it at most once.
type onceCloser struct {
	body   io.ReadCloser
	closed bool
}

// newOnceCloser returns nil for a request with no body, which keeps
// Request.Write's handling of a nil body and of [http.NoBody] unchanged.
func newOnceCloser(body io.ReadCloser) *onceCloser {
	if body == nil || body == http.NoBody {
		return nil
	}
	return &onceCloser{body: body}
}

func (c *onceCloser) Read(p []byte) (int, error) {
	return c.body.Read(p)
}

func (c *onceCloser) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	return c.body.Close()
}

// close closes c, if there is a body to close. A Close error is the body's
// to report; the round trip has already failed or finished with it.
func (c *onceCloser) close() {
	if c == nil {
		return
	}
	_ = c.Close()
}
