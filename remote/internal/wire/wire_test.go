package wire_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/zstdframe"
)

// seen is what a handler saw, captured for assertions on the test goroutine.
type seen struct {
	method           string
	requestURI       string
	contentLength    int64
	transferEncoding []string
	header           http.Header
	body             []byte
}

// recordingHandler records the request it serves into *got and answers
// with respond.
func recordingHandler(got *seen, respond func(http.ResponseWriter)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = seen{
			method:           r.Method,
			requestURI:       r.RequestURI,
			contentLength:    r.ContentLength,
			transferEncoding: r.TransferEncoding,
			header:           r.Header.Clone(),
			body:             body,
		}
		respond(w)
	})
}

func ok(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func newRequester(h http.Handler, logs *remotetest.LogRecorder, p wire.Profile) *wire.Requester {
	return wire.NewRequester(remotetest.Client(h), logs.Logger(), p)
}

func defaultProfile() wire.Profile {
	return wire.Profile{
		UserAgent:     "atomic-0.22.1",
		ClientVersion: "0.22.1",
		Header:        http.Header{"Accept-Encoding": {"zstd, gzip, deflate"}},
		MaxDecoded:    1 << 20,
	}
}

// do sends req and reads the whole body, failing the test on any error.
func do(t *testing.T, rq *wire.Requester, req wire.Request) (*wire.Response, []byte) {
	t.Helper()
	resp, err := rq.Do(context.Background(), req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, b
}

func attrs(rec slog.Record) map[string]string {
	m := map[string]string{}
	rec.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.String()
		return true
	})
	return m
}

func TestNewRequesterRequiresItsDependencies(t *testing.T) {
	logger := remotetest.NewLogRecorder().Logger()
	doer := remotetest.Client(recordingHandler(new(seen), ok))

	require.Panics(t, func() { wire.NewRequester(nil, logger, wire.Profile{}) })
	require.Panics(t, func() { wire.NewRequester(doer, nil, wire.Profile{}) })
	require.Panics(t, func() { wire.NewRequester(doer, logger, wire.Profile{MaxDecoded: -1}) })
}

func TestMinVersionWarning(t *testing.T) {
	for _, tc := range []struct {
		name          string
		clientVersion string
		header        string // "" sends no header
		status        int
		warn          bool
	}{
		{name: "newer server minimum", clientVersion: "0.22.1", header: "9.0.0", status: http.StatusOK, warn: true},
		{name: "checked on an error status", clientVersion: "0.22.1", header: "9.0.0", status: http.StatusInternalServerError, warn: true},
		{name: "older server minimum", clientVersion: "0.22.1", header: "0.1.0", status: http.StatusOK},
		{name: "equal versions", clientVersion: "0.22.1", header: "0.22.1", status: http.StatusOK},
		{name: "garbage header", clientVersion: "0.22.1", header: "garbage", status: http.StatusOK},
		{name: "no header", clientVersion: "0.22.1", status: http.StatusOK},
		{name: "empty client version", clientVersion: "", header: "9.0.0", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := remotetest.NewLogRecorder()
			p := defaultProfile()
			p.ClientVersion = tc.clientVersion
			h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.header != "" {
					// Lower case, to show the name is matched in any case.
					w.Header()["x-atomic-min-version"] = []string{tc.header}
				}
				w.WriteHeader(tc.status)
			})
			rq := newRequester(h, logs, p)

			resp, _ := do(t, rq, wire.Request{Op: "get_state", URL: mustURL(t, "https://user:secret@example.com/code?state=")})
			require.Equal(t, tc.status, resp.StatusCode)
			require.Equal(t, tc.header, resp.MinVersion)

			records := logs.Records()
			if !tc.warn {
				require.Empty(t, records)
				return
			}
			require.Len(t, records, 1)
			require.Equal(t, slog.LevelWarn, records[0].Level)
			require.Equal(t, map[string]string{
				"required_version": tc.header,
				"current_version":  tc.clientVersion,
				"url":              "https://example.com/code?state=",
			}, attrs(records[0]))
		})
	}
}

func TestMinVersionHeaderInAnyCase(t *testing.T) {
	// A Doer other than net/http's may hand back keys that are not
	// canonical.
	logs := remotetest.NewLogRecorder()
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-ATOMIC-MIN-VERSION": {" 9.0.0 "}},
			Body:       http.NoBody,
		}, nil
	})}
	rq := wire.NewRequester(doer, logs.Logger(), defaultProfile())

	resp, _ := do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})
	require.Equal(t, " 9.0.0 ", resp.MinVersion, "MinVersion keeps the raw value")
	require.Len(t, logs.Records(), 1)
}

func TestGetWithBodyHasExactContentLength(t *testing.T) {
	var got seen
	rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), defaultProfile())

	do(t, rq, wire.Request{Method: http.MethodGet, URL: mustURL(t, "https://example.com/code"), Body: []byte("abc")})

	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, int64(3), got.contentLength)
	require.Empty(t, got.transferEncoding)
	require.Equal(t, []byte("abc"), got.body)
}

func TestStreamHasExactContentLength(t *testing.T) {
	var got seen
	rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), defaultProfile())

	do(t, rq, wire.Request{
		Method:       http.MethodPost,
		URL:          mustURL(t, "https://example.com/code"),
		Stream:       strings.NewReader("hello"),
		StreamLength: 5,
	})

	require.Equal(t, int64(5), got.contentLength)
	require.Empty(t, got.transferEncoding)
	require.Equal(t, []byte("hello"), got.body)
}

func TestGetWithoutBodySendsNoContentLength(t *testing.T) {
	var got seen
	rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), defaultProfile())

	do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})

	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, int64(0), got.contentLength)
	require.Empty(t, got.header.Values("Content-Length"))
	require.Empty(t, got.transferEncoding)
}

func TestMalformedRequest(t *testing.T) {
	rq := newRequester(recordingHandler(new(seen), ok), remotetest.NewLogRecorder(), defaultProfile())
	u := mustURL(t, "https://example.com/code")

	for name, req := range map[string]wire.Request{
		"no URL":                {Op: "x"},
		"both Body and Stream":  {URL: u, Body: []byte("a"), Stream: strings.NewReader("a"), StreamLength: 1},
		"negative StreamLength": {URL: u, Stream: strings.NewReader("a"), StreamLength: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := rq.Do(context.Background(), req)
			var rerr *remote.Error
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, remote.KindOther, rerr.Kind)
		})
	}
}

func TestHeaders(t *testing.T) {
	t.Run("profile headers and User-Agent", func(t *testing.T) {
		var got seen
		rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), defaultProfile())

		do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})

		require.Equal(t, []string{"atomic-0.22.1"}, got.header.Values("User-Agent"))
		require.Equal(t, []string{"zstd, gzip, deflate"}, got.header.Values("Accept-Encoding"))
	})

	t.Run("an empty UserAgent sends none", func(t *testing.T) {
		var got seen
		p := defaultProfile()
		p.UserAgent = ""
		rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), p)

		do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})

		_, present := got.header["User-Agent"]
		require.False(t, present, "got User-Agent %q", got.header.Get("User-Agent"))
	})

	t.Run("a request header replaces the default", func(t *testing.T) {
		var got seen
		p := defaultProfile()
		p.Header.Set("Authorization", "Bearer default")
		rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), p)

		do(t, rq, wire.Request{
			URL:    mustURL(t, "https://example.com/code"),
			Header: http.Header{"authorization": {"Bearer mine"}, "Atomic-Protocol": {"1"}},
		})

		require.Equal(t, []string{"Bearer mine"}, got.header.Values("Authorization"))
		require.Equal(t, []string{"1"}, got.header.Values("Atomic-Protocol"))
	})

	t.Run("the profile is copied", func(t *testing.T) {
		var got seen
		p := defaultProfile()
		rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), p)
		p.Header.Set("Accept-Encoding", "changed")

		do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})

		require.Equal(t, []string{"zstd, gzip, deflate"}, got.header.Values("Accept-Encoding"))
	})
}

func TestRawQueryIsSentAsBuilt(t *testing.T) {
	var got seen
	rq := newRequester(recordingHandler(&got, ok), remotetest.NewLogRecorder(), defaultProfile())

	var q wire.Query
	require.NoError(t, q.Add("view", "%41+= é"))
	u := mustURL(t, "https://example.com/w/p/code")
	u.RawQuery = q.String()

	do(t, rq, wire.Request{URL: u})

	require.Equal(t, "/w/p/code?view=%41+=%20%C3%A9", got.requestURI)
}

func TestContentEncoding(t *testing.T) {
	const text = "the quick brown fox jumps over the lazy dog"

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, err := gw.Write([]byte(text))
	require.NoError(t, err)
	require.NoError(t, gw.Close())

	var zl bytes.Buffer
	zw := zlib.NewWriter(&zl)
	_, err = zw.Write([]byte(text))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	enc, err := zstdframe.NewEncoder(3)
	require.NoError(t, err)
	defer enc.Close()
	zs := enc.Compress(nil, []byte(text))

	for _, tc := range []struct {
		coding string
		body   []byte
	}{
		{"", []byte(text)},
		{"identity", []byte(text)},
		{"gzip", gz.Bytes()},
		{"x-gzip", gz.Bytes()},
		{"GZIP", gz.Bytes()},
		{"deflate", zl.Bytes()},
		{"zstd", zs},
	} {
		t.Run("decodes "+tc.coding, func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.coding != "" {
					w.Header().Set("Content-Encoding", tc.coding)
				}
				_, _ = w.Write(tc.body)
			})
			rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

			_, b := do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})
			require.Equal(t, text, string(b))
		})
	}

	t.Run("decodes stacked codings, last applied first", func(t *testing.T) {
		var gzzs bytes.Buffer
		w := gzip.NewWriter(&gzzs)
		_, err := w.Write(zs)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "zstd, gzip")
			_, _ = w.Write(gzzs.Bytes())
		})
		rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

		_, b := do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})
		require.Equal(t, text, string(b))
	})

	t.Run("br is a protocol error", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "br")
			_, _ = w.Write([]byte("whatever"))
		})
		rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

		_, err := rq.Do(context.Background(), wire.Request{Op: "get_state", URL: mustURL(t, "https://example.com/code")})
		var rerr *remote.Error
		require.ErrorAs(t, err, &rerr)
		require.Equal(t, remote.KindProtocol, rerr.Kind)
		require.Equal(t, "get_state", rerr.Op)
		require.Equal(t, http.StatusOK, rerr.StatusCode)
	})

	t.Run("zstd past MaxDecoded fails", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "zstd")
			_, _ = w.Write(zs)
		})
		p := defaultProfile()
		p.MaxDecoded = int64(len(text) - 1)
		rq := newRequester(h, remotetest.NewLogRecorder(), p)

		resp, err := rq.Do(context.Background(), wire.Request{URL: mustURL(t, "https://example.com/code")})
		require.NoError(t, err)
		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		require.ErrorIs(t, err, zstdframe.ErrLimit)
	})

	t.Run("a malformed gzip body fails when read", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("not gzip"))
		})
		rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

		resp, err := rq.Do(context.Background(), wire.Request{URL: mustURL(t, "https://example.com/code")})
		require.NoError(t, err)
		defer resp.Body.Close()
		_, err = io.ReadAll(resp.Body)
		require.Error(t, err)
	})

	t.Run("an empty encoded body reads as empty", func(t *testing.T) {
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			w.WriteHeader(http.StatusOK)
		})
		rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

		_, b := do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})
		require.Empty(t, b)
	})
}

func TestTextIsNotTranscoded(t *testing.T) {
	latin1 := []byte{'c', 'a', 'f', 0xE9} // "café" in ISO-8859-1
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=iso-8859-1")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(latin1)
	})
	rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

	t.Run("success body", func(t *testing.T) {
		_, b := do(t, rq, wire.Request{URL: mustURL(t, "https://example.com/code")})
		require.Equal(t, latin1, b)
	})

	t.Run("error body", func(t *testing.T) {
		resp, err := rq.Do(context.Background(), wire.Request{URL: mustURL(t, "https://example.com/code")})
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, string(latin1), wire.ErrorBody(resp))
	})
}

// failingBody fails every Read.
type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }
func (failingBody) Close() error             { return nil }

func TestErrorBody(t *testing.T) {
	t.Run("capped at MaxErrorBody", func(t *testing.T) {
		big := bytes.Repeat([]byte("x"), remote.MaxErrorBody+10)
		h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(big)
		})
		rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

		resp, err := rq.Do(context.Background(), wire.Request{URL: mustURL(t, "https://example.com/code")})
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Len(t, wire.ErrorBody(resp), remote.MaxErrorBody)
	})

	t.Run("unreadable is empty", func(t *testing.T) {
		require.Equal(t, "", wire.ErrorBody(&wire.Response{Body: failingBody{}}))
		require.Equal(t, "", wire.ErrorBody(&wire.Response{}))
	})
}

func TestStatusError(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   remote.Kind
	}{
		{http.StatusUnauthorized, remote.KindAuthenticationFailed},
		{http.StatusForbidden, remote.KindAuthenticationFailed},
		{http.StatusInternalServerError, remote.KindHTTP},
		{http.StatusConflict, remote.KindHTTP},
		{http.StatusTeapot, remote.KindHTTP},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set(remote.MinVersionHeader, "0.1.0")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("nope"))
			})
			rq := newRequester(h, remotetest.NewLogRecorder(), defaultProfile())

			resp, err := rq.Do(context.Background(), wire.Request{
				Op:     "get_state",
				Method: http.MethodPost,
				URL:    mustURL(t, "https://u:p@example.com/code"),
			})
			require.NoError(t, err)
			defer resp.Body.Close()

			rerr := wire.StatusError("get_state", resp, wire.ErrorBody(resp))
			require.Equal(t, &remote.Error{
				Kind:             tc.kind,
				Op:               "get_state",
				Method:           http.MethodPost,
				URL:              "https://example.com/code",
				StatusCode:       tc.status,
				Message:          "nope",
				Body:             "nope",
				ServerMinVersion: "0.1.0",
			}, rerr)
		})
	}
}

func TestSendFailures(t *testing.T) {
	t.Run("a client timeout with a live ctx is retryable", func(t *testing.T) {
		// An http.Client Timeout surfaces as an error matching
		// context.DeadlineExceeded while the request's own ctx is live.
		timeout := errors.Join(errors.New("Client.Timeout exceeded"), context.DeadlineExceeded)
		doer := &http.Client{Transport: remotetest.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, timeout
		})}
		rq := wire.NewRequester(doer, remotetest.NewLogRecorder().Logger(), defaultProfile())

		_, err := rq.Do(context.Background(), wire.Request{Op: "sync_pull", URL: mustURL(t, "https://u:p@example.com/code")})
		var rerr *remote.Error
		require.ErrorAs(t, err, &rerr)
		require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
		require.Equal(t, "sync_pull", rerr.Op)
		require.Equal(t, "https://example.com/code", rerr.URL)
		require.False(t, rerr.ContextDone)
		require.True(t, rerr.Retryable())
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("a cancelled ctx is not retryable", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		called := false
		doer := &http.Client{Transport: remotetest.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unreachable")
		})}
		rq := wire.NewRequester(doer, remotetest.NewLogRecorder().Logger(), defaultProfile())

		_, err := rq.Do(ctx, wire.Request{URL: mustURL(t, "https://example.com/code")})
		var rerr *remote.Error
		require.ErrorAs(t, err, &rerr)
		require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
		require.True(t, rerr.ContextDone)
		require.False(t, rerr.Retryable())
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, called)
	})

	t.Run("a ctx cancelled while sending is not retryable", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		doer := &http.Client{Transport: remotetest.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			cancel()
			return nil, context.Canceled
		})}
		rq := wire.NewRequester(doer, remotetest.NewLogRecorder().Logger(), defaultProfile())

		_, err := rq.Do(ctx, wire.Request{URL: mustURL(t, "https://example.com/code")})
		var rerr *remote.Error
		require.ErrorAs(t, err, &rerr)
		require.True(t, rerr.ContextDone)
		require.False(t, rerr.Retryable())
	})

	t.Run("a send failure with a live ctx is retryable", func(t *testing.T) {
		doer := &http.Client{Transport: remotetest.RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		})}
		rq := wire.NewRequester(doer, remotetest.NewLogRecorder().Logger(), defaultProfile())

		_, err := rq.Do(context.Background(), wire.Request{URL: mustURL(t, "https://example.com/code")})
		var rerr *remote.Error
		require.ErrorAs(t, err, &rerr)
		require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
		require.False(t, rerr.ContextDone)
		require.True(t, rerr.Retryable())
	})
}
