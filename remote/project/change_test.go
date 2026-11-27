package project_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/zstdframe"
)

// changeHashText is the golden hash of docs/remote/types.md.
const changeHashText = "GJGC6OK6JT77OB2PDGFYEC2BUISJWQNQVT345SNIZ2CKKXGV652A"

const changeURI = projectPath + "?change=" + changeHashText

func changeHash(t *testing.T) ident.Hash {
	t.Helper()
	h, err := ident.ParseHash(changeHashText)
	require.NoError(t, err)
	return h
}

// download is one of the two change downloads, normalised to return the
// bytes it fetched.
type download struct {
	name string
	op   string
	get  func(context.Context, *project.Client, ident.Hash) ([]byte, error)
}

func downloads() []download {
	return []download{
		{
			name: "DownloadChange",
			op:   "download_change",
			get: func(ctx context.Context, c *project.Client, h ident.Hash) ([]byte, error) {
				return c.DownloadChange(ctx, h)
			},
		},
		{
			name: "DownloadChangeTo",
			op:   "download_change_to_file",
			get: func(ctx context.Context, c *project.Client, h ident.Hash) ([]byte, error) {
				var buf bytes.Buffer
				n, err := c.DownloadChangeTo(ctx, h, &buf)
				if err != nil {
					return nil, err
				}
				if n != int64(buf.Len()) {
					return nil, errors.New("DownloadChangeTo returned a count unlike the bytes it wrote")
				}
				return buf.Bytes(), nil
			},
		},
	}
}

func TestDownloadChangeRequest(t *testing.T) {
	change := []byte("ATOMIC change file bytes, not verified")
	for _, d := range downloads() {
		t.Run(d.name, func(t *testing.T) {
			var got seen
			c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, change, "Content-Type", "application/octet-stream")))

			b, err := d.get(context.Background(), c, changeHash(t))
			require.NoError(t, err)
			require.Equal(t, change, b)

			require.Equal(t, 1, got.calls)
			require.Equal(t, http.MethodGet, got.method)
			require.Equal(t, "/workspaces/w/projects/p/code?change=GJGC6OK6JT77OB2PDGFYEC2BUISJWQNQVT345SNIZ2CKKXGV652A", got.requestURI)
			require.Empty(t, got.header.Values("Atomic-Protocol"))
			require.Empty(t, got.header.Values("Content-Type"))
			require.Empty(t, got.body)
			require.Equal(t, int64(0), got.contentLength)
			require.Equal(t, "atomic-0.22.1", got.header.Get("User-Agent"))
			require.Equal(t, "zstd, gzip, deflate", got.header.Get("Accept-Encoding"))
		})
	}
}

func TestDownloadChangeToReturnsTheCount(t *testing.T) {
	change := bytes.Repeat([]byte("change"), 20000)
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, change)))

	var buf bytes.Buffer
	n, err := c.DownloadChangeTo(context.Background(), changeHash(t), &buf)
	require.NoError(t, err)
	require.Equal(t, int64(len(change)), n)
	require.Equal(t, change, buf.Bytes())
}

func TestDownloadChangeDecodesZstd(t *testing.T) {
	change := bytes.Repeat([]byte("a zstd-encoded change file "), 100)
	enc, err := zstdframe.NewEncoder(3)
	require.NoError(t, err)
	body := enc.Compress(nil, change)
	require.NotEqual(t, change, body)

	for _, d := range downloads() {
		t.Run(d.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, body, "Content-Encoding", "zstd")))

			b, err := d.get(context.Background(), c, changeHash(t))
			require.NoError(t, err)
			require.Equal(t, change, b)
		})
	}
}

func TestDownloadChangeNotFound(t *testing.T) {
	for _, d := range downloads() {
		t.Run(d.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusNotFound, []byte("nope"), remote.MinVersionHeader, "0.1.0")))

			b, err := d.get(context.Background(), c, changeHash(t))
			require.Nil(t, b)
			re := requireRemoteError(t, err, remote.KindChangeNotFound)
			require.ErrorIs(t, err, remote.ErrNotFound)
			require.Equal(t, changeHashText, re.Subject)
			require.Equal(t, d.op, re.Op)
			require.Equal(t, http.MethodGet, re.Method)
			require.Equal(t, "https://acme.example.com"+changeURI, re.URL)
			require.Equal(t, http.StatusNotFound, re.StatusCode)
			require.Equal(t, "0.1.0", re.ServerMinVersion)
			require.EqualError(t, err, "Change not found: "+changeHashText)
		})
	}
}

func TestDownloadChangeStatuses(t *testing.T) {
	for _, d := range downloads() {
		for _, tc := range []struct {
			status   int
			kind     remote.Kind
			sentinel error
			auth     bool
		}{
			{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrUnauthorized, auth: true},
			{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrForbidden, auth: true},
			{status: http.StatusInternalServerError, kind: remote.KindHTTP},
			{status: http.StatusConflict, kind: remote.KindHTTP, sentinel: remote.ErrConflict},
			{status: http.StatusCreated, kind: remote.KindHTTP},
			{status: http.StatusNoContent, kind: remote.KindHTTP},
		} {
			t.Run(d.name+"/"+http.StatusText(tc.status), func(t *testing.T) {
				c, _ := newClient(t, recordingHandler(new(seen), respond(tc.status, []byte("nope"), remote.MinVersionHeader, "0.1.0")))

				b, err := d.get(context.Background(), c, changeHash(t))
				require.Nil(t, b)
				re := requireRemoteError(t, err, tc.kind)
				require.Equal(t, d.op, re.Op)
				require.Equal(t, http.MethodGet, re.Method)
				require.Equal(t, "https://acme.example.com"+changeURI, re.URL)
				require.Equal(t, tc.status, re.StatusCode)
				require.Equal(t, "0.1.0", re.ServerMinVersion)
				require.Equal(t, tc.auth, re.IsAuth())
				require.False(t, re.IsNotFound())
				if tc.status != http.StatusNoContent {
					require.Equal(t, "nope", re.Body)
				}
				if tc.sentinel != nil {
					require.ErrorIs(t, err, tc.sentinel)
				}
			})
		}
	}
}

// failingWriter accepts limit bytes and then fails.
type failingWriter struct {
	buf   bytes.Buffer
	limit int
	err   error
}

func (w *failingWriter) Write(p []byte) (int, error) {
	room := w.limit - w.buf.Len()
	if len(p) <= room {
		return w.buf.Write(p)
	}
	n, _ := w.buf.Write(p[:room])
	return n, w.err
}

func TestDownloadChangeToWriterFailure(t *testing.T) {
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte("ATOMIC change"))))
	w := &failingWriter{limit: 3, err: errSentinel}

	n, err := c.DownloadChangeTo(context.Background(), changeHash(t), w)
	re := requireRemoteError(t, err, remote.KindOther)
	require.ErrorIs(t, err, errSentinel)
	require.Equal(t, int64(3), n)
	require.Equal(t, "ATO", w.buf.String())
	require.Equal(t, "download_change_to_file", re.Op)
	require.Equal(t, "https://acme.example.com"+changeURI, re.URL)
	require.Equal(t, "failed to write change file: sentinel", re.Message)
	require.EqualError(t, err, "failed to write change file: sentinel")
	require.False(t, re.Retryable())
}

// shortWriter claims to write fewer bytes than it was given, with no error.
type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) / 2, nil
}

func TestDownloadChangeToShortWrite(t *testing.T) {
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte("ATOMIC"))))

	n, err := c.DownloadChangeTo(context.Background(), changeHash(t), shortWriter{})
	requireRemoteError(t, err, remote.KindOther)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Equal(t, int64(3), n)
}

func TestDownloadChangeToNilWriterSendsNothing(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))

	n, err := c.DownloadChangeTo(context.Background(), changeHash(t), nil)
	requireRemoteError(t, err, remote.KindOther)
	require.Zero(t, n)
	require.Zero(t, got.calls)
}

func TestDownloadChangeReadFailure(t *testing.T) {
	for _, d := range downloads() {
		t.Run(d.name, func(t *testing.T) {
			body := &trackingBody{r: strings.NewReader("ATOMIC"), err: errSentinel}
			c, _ := newClientWithDoer(t, bodyDoer(http.StatusOK, body))

			b, err := d.get(context.Background(), c, changeHash(t))
			require.Nil(t, b)
			re := requireRemoteError(t, err, remote.KindConnectionFailed)
			require.ErrorIs(t, err, errSentinel)
			require.Equal(t, d.op, re.Op)
			require.False(t, re.ContextDone)
			require.True(t, re.Retryable())
			require.True(t, body.closed)
		})
	}
}

func TestDownloadChangeToReadFailureKeepsTheCount(t *testing.T) {
	body := &trackingBody{r: strings.NewReader("ATOMIC"), err: errSentinel}
	c, _ := newClientWithDoer(t, bodyDoer(http.StatusOK, body))

	var buf bytes.Buffer
	n, err := c.DownloadChangeTo(context.Background(), changeHash(t), &buf)
	requireRemoteError(t, err, remote.KindConnectionFailed)
	require.Equal(t, int64(6), n)
	require.Equal(t, "ATOMIC", buf.String())
}

func TestDownloadChangeReadFailureAfterTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &trackingBody{r: strings.NewReader("ATOMIC"), err: context.Canceled}
	doer := bodyDoer(http.StatusOK, body)
	c, _ := newClientWithDoer(t, doerFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := doer.Do(req)
		cancel()
		return resp, err
	}))

	_, err := c.DownloadChange(ctx, changeHash(t))
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.True(t, re.ContextDone)
	require.False(t, re.Retryable())
	require.ErrorIs(t, err, context.Canceled)
}

func TestDownloadChangeConnectionFailure(t *testing.T) {
	for _, d := range downloads() {
		t.Run(d.name, func(t *testing.T) {
			c, _ := newClientWithDoer(t, &http.Client{Transport: roundTripError(errSentinel)})

			_, err := d.get(context.Background(), c, changeHash(t))
			re := requireRemoteError(t, err, remote.KindConnectionFailed)
			require.ErrorIs(t, err, errSentinel)
			require.Equal(t, d.op, re.Op)
		})
	}
}

func TestDownloadChangeTooLarge(t *testing.T) {
	change := []byte("ATOMIC change file")

	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, change)), project.WithMaxPackSize(int64(len(change)-1)))
	b, err := c.DownloadChange(context.Background(), changeHash(t))
	require.Nil(t, b)
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.Equal(t, "change file is larger than 17 bytes", re.Message)
	require.Equal(t, "download_change", re.Op)

	t.Run("at the limit", func(t *testing.T) {
		c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, change)), project.WithMaxPackSize(int64(len(change))))
		b, err := c.DownloadChange(context.Background(), changeHash(t))
		require.NoError(t, err)
		require.Equal(t, change, b)
	})

	t.Run("DownloadChangeTo has no limit", func(t *testing.T) {
		c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, change)), project.WithMaxPackSize(1))
		var buf bytes.Buffer
		n, err := c.DownloadChangeTo(context.Background(), changeHash(t), &buf)
		require.NoError(t, err)
		require.Equal(t, int64(len(change)), n)
		require.Equal(t, change, buf.Bytes())
	})
}

func TestDownloadChangePastTheLimitInAContentEncoding(t *testing.T) {
	enc, err := zstdframe.NewEncoder(3)
	require.NoError(t, err)
	body := enc.Compress(nil, bytes.Repeat([]byte("x"), 4096))

	for _, d := range downloads() {
		t.Run(d.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, body, "Content-Encoding", "zstd")), project.WithMaxPackSize(1024))

			_, err := d.get(context.Background(), c, changeHash(t))
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.ErrorIs(t, err, zstdframe.ErrLimit)
			require.False(t, re.Retryable())
			require.True(t, strings.HasPrefix(re.Message, "failed to decode change file: "), re.Message)
		})
	}
}
