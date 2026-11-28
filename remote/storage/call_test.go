package storage

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
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

const (
	testVersion = "0.22.1"
	testBase    = "https://acme.atomic.storage"
)

// seen is what a handler saw, captured for assertions on the test
// goroutine. remotetest.Transport serves on the caller's goroutine, so no
// lock is needed.
type seen struct {
	calls      int
	method     string
	requestURI string
	header     http.Header
	body       []byte
}

// recording wraps h so that every request it serves is recorded into got.
func recording(got *seen, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = seen{
			calls:      got.calls + 1,
			method:     r.Method,
			requestURI: r.RequestURI,
			header:     r.Header.Clone(),
			body:       body,
		}
		h.ServeHTTP(w, r)
	})
}

func newTestClient(t *testing.T, doer Doer, opts ...Option) (*Client, *remotetest.LogRecorder) {
	t.Helper()
	base, err := url.Parse(testBase)
	require.NoError(t, err)
	logs := remotetest.NewLogRecorder()
	c, err := New(base, doer, logs.Logger(), testVersion, opts...)
	require.NoError(t, err)
	return c, logs
}

// muxClient returns a Client whose requests are served in memory by a
// ServeMux holding the single route pattern, answered by h, and the record
// of what the mux saw.
func muxClient(t *testing.T, pattern string, h http.HandlerFunc, opts ...Option) (*Client, *seen, *remotetest.LogRecorder) {
	t.Helper()
	got := &seen{}
	mux := http.NewServeMux()
	mux.Handle(pattern, h)
	c, logs := newTestClient(t, remotetest.Client(recording(got, mux)), opts...)
	return c, got, logs
}

type thing struct {
	Name string `json:"name"`
}

func getThing(ctx context.Context, c *Client) (thing, error) {
	return call[thing](ctx, c, request{op: "get_thing", method: http.MethodGet, path: "/things/x"})
}

func requireRemoteError(t *testing.T, err error) *remote.Error {
	t.Helper()
	var rerr *remote.Error
	require.ErrorAs(t, err, &rerr)
	return rerr
}

func TestCallReturnsData(t *testing.T) {
	c, got, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Data(w, http.StatusOK, map[string]string{"name": "x"}))
	})
	th, err := getThing(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, thing{Name: "x"}, th)
	require.Equal(t, 1, got.calls)
	require.Equal(t, "/things/x", got.requestURI)
}

func TestCallAcceptsAny2xx(t *testing.T) {
	c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
		storagetest.Raw(w, http.StatusAccepted, `{"success":true,"data":{"name":"y"}}`)
	})
	th, err := getThing(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, thing{Name: "y"}, th)
}

func TestCallHeaders(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPost, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			c, got, _ := muxClient(t, "/things/x", func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, storagetest.Data(w, http.StatusOK, map[string]string{}))
			})
			_, err := call[map[string]string](t.Context(), c, request{op: "op", method: method, path: "/things/x"})
			require.NoError(t, err)
			require.Equal(t, method, got.method)
			require.Equal(t, []string{"application/json"}, got.header.Values("Content-Type"))
			require.Equal(t, []string{"atomic/" + testVersion}, got.header.Values("User-Agent"))
			require.Equal(t, []string{"gzip,deflate"}, got.header.Values("Accept-Encoding"))
			require.Empty(t, got.body)
		})
	}
}

func TestCallUserAgentOption(t *testing.T) {
	c, got, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Data(w, http.StatusOK, thing{}))
	}, WithUserAgent("my-tool/1.0"))
	_, err := getThing(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, []string{"my-tool/1.0"}, got.header.Values("User-Agent"))
}

func TestCallMarshalsBodyWithoutHTMLEscaping(t *testing.T) {
	c, got, _ := muxClient(t, "POST /things", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Data(w, http.StatusCreated, thing{Name: "made"}))
	})
	th, err := call[thing](t.Context(), c, request{
		op: "create_thing", method: http.MethodPost, path: "/things",
		body: thing{Name: "<a> & 'b'"},
	})
	require.NoError(t, err)
	require.Equal(t, thing{Name: "made"}, th)
	require.Equal(t, `{"name":"<a> & 'b'"}`, string(got.body))
	require.Equal(t, []string{"application/json"}, got.header.Values("Content-Type"))
}

func TestCallBodyThatDoesNotMarshal(t *testing.T) {
	c, got, _ := muxClient(t, "/", func(w http.ResponseWriter, r *http.Request) {})
	_, err := call[thing](t.Context(), c, request{op: "op", method: http.MethodPost, path: "/things", body: make(chan int)})
	rerr := requireRemoteError(t, err)
	require.Equal(t, remote.KindOther, rerr.Kind)
	require.Zero(t, got.calls)
}

func TestCallSendsQueryAsBuilt(t *testing.T) {
	c, got, _ := muxClient(t, "GET /identities/resolve", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Data(w, http.StatusOK, thing{}))
	})
	_, err := call[thing](t.Context(), c, request{
		op: "resolve_identity_by_email", method: http.MethodGet,
		path: "/identities/resolve", query: "email=" + component("alice@example.com"),
	})
	require.NoError(t, err)
	require.Equal(t, "/identities/resolve?email=alice%40example.com", got.requestURI)
}

func TestCallStatusErrors(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantCode    string
		wantMessage string
	}{
		{
			name:        "401 envelope",
			status:      http.StatusUnauthorized,
			body:        `{"success":false,"error":{"code":"UNAUTHORIZED","message":"bad token"}}`,
			wantCode:    "UNAUTHORIZED",
			wantMessage: "bad token",
		},
		{
			name:        "404 envelope",
			status:      http.StatusNotFound,
			body:        `{"success":false,"error":{"code":"NOT_FOUND","message":"workspace not found"}}`,
			wantCode:    "NOT_FOUND",
			wantMessage: "workspace not found",
		},
		{
			name:        "502 text",
			status:      http.StatusBadGateway,
			body:        "Bad Gateway",
			wantMessage: "HTTP 502",
		},
		{
			name:        "bare code and message",
			status:      http.StatusBadRequest,
			body:        `{"code":"VALIDATION","message":"name is required"}`,
			wantCode:    "VALIDATION",
			wantMessage: "name is required",
		},
		{
			name:        "envelope error without success",
			status:      http.StatusBadRequest,
			body:        `{"error":{"code":"X","message":"m"}}`,
			wantMessage: "HTTP 400",
		},
		{
			name:        "envelope error without message",
			status:      http.StatusBadRequest,
			body:        `{"success":false,"error":{"code":"X"}}`,
			wantMessage: "HTTP 400",
		},
		{
			name:        "envelope without error",
			status:      http.StatusInternalServerError,
			body:        `{"success":false}`,
			wantMessage: "HTTP 500",
		},
		{
			name:        "bare code that is not a string",
			status:      http.StatusBadRequest,
			body:        `{"code":1,"message":"m"}`,
			wantMessage: "HTTP 400",
		},
		{
			name:        "keys in another case",
			status:      http.StatusBadRequest,
			body:        `{"Code":"X","Message":"m"}`,
			wantMessage: "HTTP 400",
		},
		{
			name:        "409 envelope",
			status:      http.StatusConflict,
			body:        `{"success":false,"error":{"code":"CONFLICT","message":"exists"}}`,
			wantCode:    "CONFLICT",
			wantMessage: "exists",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
				storagetest.Raw(w, tt.status, tt.body)
			})
			_, err := getThing(t.Context(), c)
			rerr := requireRemoteError(t, err)
			require.Equal(t, remote.KindHTTP, rerr.Kind)
			require.Equal(t, tt.status, rerr.StatusCode)
			require.Equal(t, tt.wantCode, rerr.Code)
			require.Equal(t, tt.wantMessage, rerr.Message)
			require.Equal(t, tt.body, rerr.Body)
			require.Equal(t, "get_thing", rerr.Op)
			require.Equal(t, http.MethodGet, rerr.Method)
			require.Equal(t, testBase+"/things/x", rerr.URL)
			require.Equal(t, "HTTP error "+strconv.Itoa(tt.status)+": "+tt.wantMessage, rerr.Error())
		})
	}
}

func TestCallStatusErrorClassifiers(t *testing.T) {
	answer := func(t *testing.T, status int, body string) error {
		t.Helper()
		c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
			storagetest.Raw(w, status, body)
		})
		_, err := getThing(t.Context(), c)
		return err
	}

	t.Run("401", func(t *testing.T) {
		c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, storagetest.Fail(w, http.StatusUnauthorized, "UNAUTHORIZED", "bad token"))
		})
		_, err := getThing(t.Context(), c)
		rerr := requireRemoteError(t, err)
		require.Equal(t, "UNAUTHORIZED", rerr.Code)
		require.Equal(t, "bad token", rerr.Message)
		require.ErrorIs(t, err, remote.ErrUnauthorized)
		require.True(t, rerr.IsAuth())
		require.False(t, rerr.IsNotFound())
		require.False(t, rerr.Retryable())
		require.Equal(t, "HTTP error 401: bad token", rerr.Error())
	})
	t.Run("404", func(t *testing.T) {
		err := answer(t, http.StatusNotFound, `{"success":false,"error":{"code":"NOT_FOUND","message":"workspace not found"}}`)
		require.ErrorIs(t, err, remote.ErrNotFound)
		require.False(t, requireRemoteError(t, err).IsAuth())
	})
	t.Run("403", func(t *testing.T) {
		err := answer(t, http.StatusForbidden, `{"code":"FORBIDDEN","message":"no"}`)
		require.ErrorIs(t, err, remote.ErrForbidden)
		require.True(t, requireRemoteError(t, err).IsAuth())
	})
	t.Run("409", func(t *testing.T) {
		require.ErrorIs(t, answer(t, http.StatusConflict, "conflict"), remote.ErrConflict)
	})
	t.Run("502", func(t *testing.T) {
		err := answer(t, http.StatusBadGateway, "Bad Gateway")
		require.Equal(t, "HTTP error 502: HTTP 502", err.Error())
		require.Contains(t, err.Error(), "502")
	})
}

func TestCallProtocolErrors(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantMessage string
		check       func(t *testing.T, err error)
	}{
		{
			name:        "success without data",
			body:        `{"success":true}`,
			wantMessage: "response had success=true but no data",
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrNoData)
			},
		},
		{
			name:        "success with null data",
			body:        `{"success":true,"data":null}`,
			wantMessage: "response had success=true but no data",
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, ErrNoData)
			},
		},
		{
			name:        "success false with an error",
			body:        `{"success":false,"error":{"code":"NOT_FOUND","message":"workspace not found"}}`,
			wantMessage: "NOT_FOUND: workspace not found",
			check: func(t *testing.T, err error) {
				var fe *FailureError
				require.ErrorAs(t, err, &fe)
				require.Equal(t, http.StatusOK, fe.Status)
				require.Equal(t, &APIError{Code: "NOT_FOUND", Message: "workspace not found"}, fe.Err)
				require.Equal(t, "NOT_FOUND", requireRemoteError(t, err).Code)
				// The status is 200, so nothing says not found.
				require.NotErrorIs(t, err, remote.ErrNotFound)
			},
		},
		{
			name:        "success false without an error",
			body:        `{"success":false}`,
			wantMessage: "request failed with no error details",
			check: func(t *testing.T, err error) {
				var fe *FailureError
				require.ErrorAs(t, err, &fe)
				require.Nil(t, fe.Err)
				require.Equal(t, "request failed with no error details", fe.Error())
			},
		},
		{
			name: "not JSON",
			body: "<html>hello</html>",
			check: func(t *testing.T, err error) {
				rerr := requireRemoteError(t, err)
				require.True(t, strings.HasPrefix(rerr.Message, "invalid response JSON: "), rerr.Message)
				require.True(t, strings.HasSuffix(rerr.Message, "(body: <html>hello</html>)"), rerr.Message)
				require.Error(t, rerr.Err)
			},
		},
		{
			name: "missing success",
			body: `{"data":{"name":"x"}}`,
		},
		{
			name: "success is not a bool",
			body: `{"success":"true","data":{"name":"x"}}`,
		},
		{
			name: "data of the wrong type",
			body: `{"success":true,"data":"x"}`,
		},
		{
			name: "success written twice",
			body: `{"success":false,"success":true,"data":{"name":"x"}}`,
		},
		{
			name: "success in another case",
			body: `{"Success":true,"data":{"name":"x"}}`,
		},
		{
			name: "data after the envelope",
			body: `{"success":true,"data":{"name":"x"}} {}`,
		},
		{
			name: "malformed metadata",
			body: `{"success":true,"data":{"name":"x"},"metadata":{"page":1}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
				storagetest.Raw(w, http.StatusOK, tt.body)
			})
			_, err := getThing(t.Context(), c)
			rerr := requireRemoteError(t, err)
			require.Equal(t, remote.KindProtocol, rerr.Kind)
			require.Equal(t, http.StatusOK, rerr.StatusCode)
			if tt.wantMessage != "" {
				require.Equal(t, tt.wantMessage, rerr.Message)
			} else {
				require.True(t, strings.HasPrefix(rerr.Message, "invalid response JSON: "), rerr.Message)
			}
			if tt.check != nil {
				tt.check(t, err)
			}
		})
	}
}

func TestCallAcceptsMetadataAliases(t *testing.T) {
	for _, body := range []string{
		`{"success":true,"data":{"name":"x"},"metadata":{"page":1,"perPage":20,"total":41,"totalPages":3}}`,
		`{"success":true,"data":{"name":"x"},"metadata":{"page":1,"per_page":20,"total":41,"total_pages":3}}`,
	} {
		c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
			storagetest.Raw(w, http.StatusOK, body)
		})
		th, err := getThing(t.Context(), c)
		require.NoError(t, err, body)
		require.Equal(t, thing{Name: "x"}, th)
	}
}

func TestQuoteCutsLongBodies(t *testing.T) {
	require.Equal(t, "short", quote([]byte("short")))
	long := strings.Repeat("a", maxQuoted-1) + "é" + "tail"
	got := quote([]byte(long))
	require.Equal(t, strings.Repeat("a", maxQuoted-1)+"… ("+"2005"+" bytes total)", got)
}

func TestCallDeleteIgnoresBody(t *testing.T) {
	c, got, _ := muxClient(t, "DELETE /things/x", func(w http.ResponseWriter, r *http.Request) {
		storagetest.Raw(w, http.StatusNoContent, "")
	})
	_, err := call[struct{}](t.Context(), c, request{op: "delete_thing", method: http.MethodDelete, path: "/things/x"})
	require.NoError(t, err)
	require.Equal(t, 1, got.calls)

	c, _, _ = muxClient(t, "DELETE /things/x", func(w http.ResponseWriter, r *http.Request) {
		storagetest.Raw(w, http.StatusOK, "garbage, not an envelope")
	})
	_, err = call[struct{}](t.Context(), c, request{op: "delete_thing", method: http.MethodDelete, path: "/things/x"})
	require.NoError(t, err)
}

func TestCallDelete204WithGarbageBody(t *testing.T) {
	// A 204 cannot carry a body through a real HTTP encoding, so this
	// answers with one directly.
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("{{{ garbage")),
			Request:    req,
		}, nil
	})}
	c, _ := newTestClient(t, doer)
	_, err := call[struct{}](t.Context(), c, request{op: "delete_thing", method: http.MethodDelete, path: "/things/x"})
	require.NoError(t, err)
}

func TestCallDeleteStatusError(t *testing.T) {
	c, _, _ := muxClient(t, "DELETE /things/x", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "gone"))
	})
	_, err := call[struct{}](t.Context(), c, request{op: "delete_thing", method: http.MethodDelete, path: "/things/x"})
	rerr := requireRemoteError(t, err)
	require.Equal(t, remote.KindHTTP, rerr.Kind)
	require.Equal(t, "gone", rerr.Message)
	require.ErrorIs(t, err, remote.ErrNotFound)
}

// failingBody yields its data and then fails.
type failingBody struct {
	r   io.Reader
	err error
}

func (b *failingBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, b.err
	}
	return n, err
}

func (b *failingBody) Close() error { return nil }

func TestCallBodyFailingMidReadIsConnectionFailed(t *testing.T) {
	cause := errors.New("connection reset")
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       &failingBody{r: strings.NewReader(`{"success":true,"da`), err: cause},
			Request:    req,
		}, nil
	})}
	c, _ := newTestClient(t, doer)
	_, err := getThing(t.Context(), c)
	rerr := requireRemoteError(t, err)
	require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
	require.Equal(t, http.StatusOK, rerr.StatusCode)
	require.ErrorIs(t, err, cause)
	require.True(t, rerr.Retryable())
}

func TestCallSendFailureIsConnectionFailed(t *testing.T) {
	cause := errors.New("connection refused")
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, cause
	})}
	c, _ := newTestClient(t, doer)
	_, err := getThing(t.Context(), c)
	rerr := requireRemoteError(t, err)
	require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "Failed to connect to remote: "+testBase+"/things/x", rerr.Error())
}

func TestCallContextCanceledSurvives(t *testing.T) {
	c, got, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Data(w, http.StatusOK, thing{}))
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := getThing(ctx, c)
	rerr := requireRemoteError(t, err)
	require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, rerr.ContextDone)
	require.False(t, rerr.Retryable())
	require.Zero(t, got.calls)
}

func TestCallWarnsOnNewerMinVersionBeforeStatus(t *testing.T) {
	c, _, logs := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(remote.MinVersionHeader, "0.30.0")
		require.NoError(t, storagetest.Fail(w, http.StatusNotFound, "NOT_FOUND", "no such thing"))
	})
	_, err := getThing(t.Context(), c)
	rerr := requireRemoteError(t, err)
	require.Equal(t, http.StatusNotFound, rerr.StatusCode)
	require.Equal(t, "0.30.0", rerr.ServerMinVersion)

	var warns []slog.Record
	for _, rec := range logs.Records() {
		if rec.Level == slog.LevelWarn {
			warns = append(warns, rec)
		}
	}
	require.Len(t, warns, 1)
	attrs := map[string]string{}
	warns[0].Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	require.Equal(t, "0.30.0", attrs["required_version"])
	require.Equal(t, testVersion, attrs["current_version"])
}

func TestCallDecodesGzip(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte(`{"success":true,"data":{"name":"zipped"}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	})
	th, err := getThing(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, thing{Name: "zipped"}, th)
}

func TestCallDecodesDeflate(t *testing.T) {
	// HTTP's deflate is the zlib format, RFC 1950, as reqwest reads it.
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	_, err := zw.Write([]byte(`{"success":true,"data":{"name":"deflated"}}`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	c, _, _ := muxClient(t, "GET /things/x", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	})
	th, err := getThing(t.Context(), c)
	require.NoError(t, err)
	require.Equal(t, thing{Name: "deflated"}, th)
}

func TestCallRefusesBodyLargerThanMax(t *testing.T) {
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(io.LimitReader(zeros{}, MaxResponseBody+1)),
			Request:    req,
		}, nil
	})}
	c, _ := newTestClient(t, doer)
	_, err := getThing(t.Context(), c)
	rerr := requireRemoteError(t, err)
	require.Equal(t, remote.KindProtocol, rerr.Kind)
	require.Contains(t, rerr.Message, "larger than")
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
