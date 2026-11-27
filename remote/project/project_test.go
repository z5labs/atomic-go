package project_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/syncproto"
)

const (
	clientVersion = "0.22.1"
	projectPath   = "/workspaces/w/projects/p/code"
)

// seen is what a handler saw, captured for assertions on the test goroutine.
// remotetest.Transport serves on the caller's goroutine, so no lock is
// needed.
type seen struct {
	calls            int
	method           string
	requestURI       string
	contentLength    int64
	transferEncoding []string
	header           http.Header
	body             []byte
}

// recordingHandler records each request it serves into got and answers it
// with respond.
func recordingHandler(got *seen, respond func(http.ResponseWriter)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = seen{
			calls:            got.calls + 1,
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

// respond returns a responder writing status, the headers in kv (name,
// value, name, value, ...) and body.
func respond(status int, body []byte, kv ...string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		for i := 0; i+1 < len(kv); i += 2 {
			w.Header().Set(kv[i], kv[i+1])
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

func projectURL(t *testing.T) serverurl.Project {
	t.Helper()
	base, err := url.Parse("https://acme.example.com")
	require.NoError(t, err)
	u, err := serverurl.NewProject(base, "w", "p")
	require.NoError(t, err)
	return u
}

// newClient returns a Client serving every request with h, in memory, and
// the recorder its logger writes to.
func newClient(t *testing.T, h http.Handler, opts ...project.Option) (*project.Client, *remotetest.LogRecorder) {
	t.Helper()
	return newClientWithDoer(t, remotetest.Client(h), opts...)
}

func newClientWithDoer(t *testing.T, doer project.Doer, opts ...project.Option) (*project.Client, *remotetest.LogRecorder) {
	t.Helper()
	logs := remotetest.NewLogRecorder()
	c, err := project.New(projectURL(t), doer, logs.Logger(), clientVersion, opts...)
	require.NoError(t, err)
	return c, logs
}

// trackingBody is a response body that records whether it was closed and
// can fail after its data.
type trackingBody struct {
	r      io.Reader
	err    error // returned once r is exhausted; nil means io.EOF
	closed bool
}

func (b *trackingBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF && b.err != nil {
		return n, b.err
	}
	return n, err
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

// bodyDoer answers every request with status and body.
func bodyDoer(status int, body io.ReadCloser) project.Doer {
	return &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{},
			Body:       body,
			Request:    req,
		}, nil
	})}
}

func debugRecords(logs *remotetest.LogRecorder) []slog.Record {
	var out []slog.Record
	for _, rec := range logs.Records() {
		if rec.Level == slog.LevelDebug {
			out = append(out, rec)
		}
	}
	return out
}

func TestNewNamesEveryMissingArgument(t *testing.T) {
	c, err := project.New(serverurl.Project{}, nil, nil, "")
	require.Nil(t, c)
	require.ErrorIs(t, err, project.ErrInvalidArgument)

	msg := err.Error()
	require.Contains(t, msg, "the project URL is required")
	require.Contains(t, msg, "the Doer is required")
	require.Contains(t, msg, "the *slog.Logger is required")
	require.Contains(t, msg, "the client version is required")
}

func TestNewNamesOnlyWhatIsMissing(t *testing.T) {
	logger := remotetest.NewLogRecorder().Logger()
	c, err := project.New(projectURL(t), nil, logger, clientVersion)
	require.Nil(t, c)
	require.ErrorIs(t, err, project.ErrInvalidArgument)
	require.Contains(t, err.Error(), "the Doer is required")
	require.NotContains(t, err.Error(), "project URL")
	require.NotContains(t, err.Error(), "slog")
	require.NotContains(t, err.Error(), "client version")
}

func TestNewRefusesAMaxPackSizeOutOfRange(t *testing.T) {
	doer := remotetest.Client(http.NotFoundHandler())
	logger := remotetest.NewLogRecorder().Logger()
	for _, n := range []int64{0, -1, syncproto.MaxDecodedSize + 1} {
		c, err := project.New(projectURL(t), doer, logger, clientVersion, project.WithMaxPackSize(n))
		require.Nil(t, c, "size %d", n)
		require.ErrorIs(t, err, project.ErrInvalidArgument, "size %d", n)
		require.Contains(t, err.Error(), "WithMaxPackSize", "size %d", n)
	}

	for _, n := range []int64{1, syncproto.MaxDecodedSize} {
		c, err := project.New(projectURL(t), doer, logger, clientVersion, project.WithMaxPackSize(n))
		require.NoError(t, err, "size %d", n)
		require.NotNil(t, c)
	}
}

func TestNewRefusesANilOption(t *testing.T) {
	doer := remotetest.Client(http.NotFoundHandler())
	logger := remotetest.NewLogRecorder().Logger()
	_, err := project.New(projectURL(t), doer, logger, clientVersion, nil)
	require.ErrorIs(t, err, project.ErrInvalidArgument)
}

func TestURL(t *testing.T) {
	c, _ := newClient(t, http.NotFoundHandler())
	require.Equal(t, projectURL(t), c.URL())
	require.Equal(t, "https://acme.example.com"+projectPath, c.URL().String())
}

func TestUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []project.Option
		want []string // nil means no User-Agent header
	}{
		{name: "default", want: []string{"atomic-0.22.1"}},
		{name: "WithUserAgent", opts: []project.Option{project.WithUserAgent("custom/1")}, want: []string{"custom/1"}},
		{name: "empty", opts: []project.Option{project.WithUserAgent("")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got seen
			c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)), tc.opts...)
			_, err := c.SyncPush(context.Background(), syncproto.Pack{})
			require.NoError(t, err)
			require.Equal(t, tc.want, got.header.Values("User-Agent"))
		})
	}
}

func TestRequestSendsNoCredentialsOfItsOwn(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))
	_, err := c.SyncPush(context.Background(), syncproto.Pack{})
	require.NoError(t, err)
	require.Empty(t, got.header.Values("Authorization"))
}

// errSentinel is a cause a test can match with errors.Is.
var errSentinel = errors.New("sentinel")

// mustEncodePack encodes p, failing the test on error.
func mustEncodePack(t *testing.T, p syncproto.Pack) []byte {
	t.Helper()
	b, err := syncproto.NewCodec().EncodePack(p)
	require.NoError(t, err)
	return b
}

// samplePack is a pack with three objects and two refs.
func samplePack() syncproto.Pack {
	old := "view-old"
	return syncproto.Pack{
		Objects: []syncproto.ObjectRecord{
			{Family: syncproto.Change, Key: "CHANGEA", Bytes: []byte("change a")},
			{Family: syncproto.View, Key: "viewkey", Bytes: []byte(`{"name":"main"}`)},
			{Family: syncproto.Tag, Key: "TAGB", Bytes: bytes.Repeat([]byte{7}, 64)},
		},
		Refs: []syncproto.RefRecord{
			{Name: "main", ExpectOld: &old, NewTarget: "viewkey"},
			{Name: "dev", NewTarget: "viewkey"},
		},
	}
}
