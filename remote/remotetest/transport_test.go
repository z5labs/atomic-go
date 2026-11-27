package remotetest_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/remotetest"
)

// closeRecorder is a request body that records whether it was closed.
type closeRecorder struct {
	r      io.Reader
	closed bool
}

func (c *closeRecorder) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

// opaqueBody is a request body of a type net/http cannot measure, so a
// request carrying it has an unknown length. It records how many goroutines
// were running each time it was read.
type opaqueBody struct {
	r          io.Reader
	closed     bool
	goroutines []int
}

func (o *opaqueBody) Read(p []byte) (int, error) {
	o.goroutines = append(o.goroutines, runtime.NumGoroutine())
	return o.r.Read(p)
}

func (o *opaqueBody) Close() error {
	o.closed = true
	return nil
}

// served is what a handler saw, captured for assertions on the test goroutine.
type served struct {
	called        bool
	method        string
	requestURI    string
	rawQuery      string
	contentLength int64
	header        http.Header
	body          []byte
	ctx           context.Context
}

func recordingHandler(got *served, respond http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.called = true
		got.method = r.Method
		got.requestURI = r.RequestURI
		got.rawQuery = r.URL.RawQuery
		got.contentLength = r.ContentLength
		got.header = r.Header.Clone()
		got.ctx = r.Context()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		got.body = b
		if respond != nil {
			respond(w, r)
		}
	})
}

func TestTransport(t *testing.T) {
	t.Run("a GET with a body keeps its Content-Length", func(t *testing.T) {
		var got served
		client := remotetest.Client(recordingHandler(&got, nil))

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://atomic.test/w/p/code", strings.NewReader("wants"))
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.True(t, got.called)
		require.Equal(t, http.MethodGet, got.method)
		require.Equal(t, int64(len("wants")), got.contentLength)
		require.Equal(t, []string{"5"}, got.header.Values("Content-Length"))
		require.Empty(t, got.header.Values("Transfer-Encoding"))
		require.Equal(t, []byte("wants"), got.body)
	})

	t.Run("a GET with a body of unknown length is sent chunked without a probe", func(t *testing.T) {
		var got served
		var transferEncoding []string
		client := remotetest.Client(recordingHandler(&got, func(_ http.ResponseWriter, r *http.Request) {
			transferEncoding = r.TransferEncoding
		}))

		body := &opaqueBody{r: strings.NewReader("wants")}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://atomic.test/w/p/code", body)
		require.NoError(t, err)
		require.Zero(t, req.ContentLength)

		before := runtime.NumGoroutine()
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.NotEmpty(t, body.goroutines)
		for _, n := range body.goroutines {
			require.Equal(t, before, n)
		}
		require.True(t, body.closed)
		require.Equal(t, []string{"chunked"}, transferEncoding)
		require.Equal(t, int64(-1), got.contentLength)
		require.Equal(t, []byte("wants"), got.body)
	})

	t.Run("a GET with an empty body of unknown length is sent without one", func(t *testing.T) {
		var got served
		var transferEncoding []string
		client := remotetest.Client(recordingHandler(&got, func(_ http.ResponseWriter, r *http.Request) {
			transferEncoding = r.TransferEncoding
		}))

		body := &opaqueBody{r: strings.NewReader("")}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://atomic.test/", body)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.True(t, body.closed)
		require.Empty(t, transferEncoding)
		require.Zero(t, got.contentLength)
		require.Empty(t, got.body)
	})

	t.Run("a HEAD keeps the handler's Content-Length without a body", func(t *testing.T) {
		client := remotetest.Client(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, "f.txt", time.Time{}, strings.NewReader("hello world"))
		}))

		req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, "http://atomic.test/f.txt", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, int64(len("hello world")), resp.ContentLength)
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Empty(t, b)
	})

	t.Run("the status and repeated headers reach the caller", func(t *testing.T) {
		var got served
		client := remotetest.Client(recordingHandler(&got, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Add("X-Repeated", "one")
			w.Header().Add("X-Repeated", "two")
			w.WriteHeader(http.StatusTeapot)
			_, _ = io.WriteString(w, "short and stout")
		}))

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://atomic.test/", nil)
		require.NoError(t, err)
		req.Header.Add("X-Asked", "a")
		req.Header.Add("X-Asked", "b")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusTeapot, resp.StatusCode)
		require.Equal(t, []string{"one", "two"}, resp.Header.Values("X-Repeated"))
		require.Equal(t, []string{"a", "b"}, got.header.Values("X-Asked"))
		require.Equal(t, int64(len("short and stout")), resp.ContentLength)
		require.False(t, resp.Close)

		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "short and stout", string(b))
	})

	t.Run("the request URI arrives byte for byte", func(t *testing.T) {
		var got served
		client := remotetest.Client(recordingHandler(&got, nil))

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://atomic.test/w/p?view=a%20b&state=", nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, "/w/p?view=a%20b&state=", got.requestURI)
		require.Equal(t, "view=a%20b&state=", got.rawQuery)
	})

	t.Run("Content-Encoding is passed through untouched", func(t *testing.T) {
		encoded := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x01}
		var got served
		client := remotetest.Client(recordingHandler(&got, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "zstd")
			_, _ = w.Write(encoded)
		}))

		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://atomic.test/", bytes.NewReader(encoded))
		require.NoError(t, err)
		req.Header.Set("Content-Encoding", "zstd")
		req.Header.Set("Accept-Encoding", "zstd, gzip, deflate")

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, "zstd", got.header.Get("Content-Encoding"))
		require.Equal(t, "zstd, gzip, deflate", got.header.Get("Accept-Encoding"))
		require.Equal(t, encoded, got.body)

		require.Equal(t, "zstd", resp.Header.Get("Content-Encoding"))
		require.False(t, resp.Uncompressed)
		b, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, encoded, b)
	})

	t.Run("the handler sees the caller's context", func(t *testing.T) {
		type key struct{}
		var got served
		rt := remotetest.Transport(recordingHandler(&got, nil))

		ctx := context.WithValue(t.Context(), key{}, "value")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://atomic.test/", nil)
		require.NoError(t, err)

		resp, err := rt.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.NotNil(t, got.ctx)
		require.Equal(t, "value", got.ctx.Value(key{}))
		require.Same(t, req, resp.Request)
	})

	t.Run("a done context returns its error without calling the handler", func(t *testing.T) {
		var got served
		rt := remotetest.Transport(recordingHandler(&got, nil))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		body := &closeRecorder{r: strings.NewReader("wants")}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://atomic.test/", body)
		require.NoError(t, err)

		resp, err := rt.RoundTrip(req)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, resp)
		require.False(t, got.called)
		require.True(t, body.closed)
	})

	t.Run("a context done while the handler runs returns its error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		rt := remotetest.Transport(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			cancel()
			_, _ = io.WriteString(w, "too late")
		}))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://atomic.test/", nil)
		require.NoError(t, err)

		resp, err := rt.RoundTrip(req)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, resp)
	})

	t.Run("a request that cannot be read back closes its body", func(t *testing.T) {
		var got served
		rt := remotetest.Transport(recordingHandler(&got, nil))

		body := &closeRecorder{r: strings.NewReader("wants")}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://atomic.test/", body)
		require.NoError(t, err)
		// Request.Write writes the method verbatim, and a space in it leaves
		// a request line http.ReadRequest cannot parse.
		req.Method = "BAD METHOD"

		resp, err := rt.RoundTrip(req)
		require.Error(t, err)
		require.Nil(t, resp)
		require.False(t, got.called)
		require.True(t, body.closed)
	})

	t.Run("a request that cannot be written closes its body", func(t *testing.T) {
		var got served
		rt := remotetest.Transport(recordingHandler(&got, nil))

		body := &closeRecorder{r: strings.NewReader("short")}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://atomic.test/", body)
		require.NoError(t, err)
		req.ContentLength = 100

		resp, err := rt.RoundTrip(req)
		require.Error(t, err)
		require.Nil(t, resp)
		require.False(t, got.called)
		require.True(t, body.closed)
	})

	t.Run("a served request closes its body", func(t *testing.T) {
		var got served
		rt := remotetest.Transport(recordingHandler(&got, nil))

		body := &closeRecorder{r: strings.NewReader("wants")}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://atomic.test/", body)
		require.NoError(t, err)

		resp, err := rt.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.True(t, body.closed)
		require.Equal(t, []byte("wants"), got.body)
	})

	t.Run("the caller's request is not mutated", func(t *testing.T) {
		var got served
		rt := remotetest.Transport(recordingHandler(&got, nil))

		body := &closeRecorder{r: strings.NewReader("wants")}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://atomic.test/w/p?view=a%20b&state=", body)
		require.NoError(t, err)
		req.ContentLength = int64(len("wants"))
		req.Header.Set("Atomic-Protocol", "sync/1")
		wantURL := *req.URL
		wantHeader := req.Header.Clone()

		resp, err := rt.RoundTrip(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, wantURL, *req.URL)
		require.Equal(t, wantHeader, req.Header)
		require.Equal(t, int64(len("wants")), req.ContentLength)
		require.Empty(t, req.RequestURI)
		require.Empty(t, req.TransferEncoding)
		require.Same(t, body, req.Body)
		require.Equal(t, []byte("wants"), got.body)
	})
}

func TestClient(t *testing.T) {
	t.Run("sets no Timeout", func(t *testing.T) {
		client := remotetest.Client(http.NotFoundHandler())
		require.Zero(t, client.Timeout)
		require.NotNil(t, client.Transport)
	})
}

func TestRoundTripFunc(t *testing.T) {
	t.Run("calls the function", func(t *testing.T) {
		errFake := errors.New("fake")
		var seen *http.Request
		var rt http.RoundTripper = remotetest.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = r
			return nil, errFake
		})

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://atomic.test/", nil)
		require.NoError(t, err)

		resp, err := rt.RoundTrip(req)
		require.ErrorIs(t, err, errFake)
		require.Nil(t, resp)
		require.Same(t, req, seen)
	})
}
