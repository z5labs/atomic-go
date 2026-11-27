package project_test

import (
	"bytes"
	"context"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/syncproto"
)

func requireRemoteError(t *testing.T, err error, kind remote.Kind) *remote.Error {
	t.Helper()
	var re *remote.Error
	require.ErrorAs(t, err, &re)
	require.Equal(t, kind, re.Kind, "error: %v", err)
	return re
}

func TestSyncPushRequest(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))

	p := samplePack()
	_, err := c.SyncPush(context.Background(), p)
	require.NoError(t, err)

	require.Equal(t, 1, got.calls)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, projectPath, got.requestURI)
	require.Equal(t, "sync/1", got.header.Get("Atomic-Protocol"))
	require.Equal(t, "application/vnd.atomic.sync.v1", got.header.Get("Content-Type"))
	require.Equal(t, "atomic-0.22.1", got.header.Get("User-Agent"))
	require.Equal(t, "zstd, gzip, deflate", got.header.Get("Accept-Encoding"))

	// Buffered, never chunked.
	require.Empty(t, got.transferEncoding)
	require.Equal(t, int64(len(got.body)), got.contentLength)
	require.Equal(t, mustEncodePack(t, p), got.body)

	decoded, err := syncproto.NewCodec().DecodePack(bytes.NewReader(got.body))
	require.NoError(t, err)
	require.Equal(t, p, decoded)
}

func TestSyncPushSummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   project.PushSummary
		debug  bool
	}{
		{name: "201 with both counts", status: http.StatusCreated, body: `{"stored":3,"refs_moved":1}`, want: project.PushSummary{Stored: 3, RefsMoved: 1}},
		{name: "200 with both counts", status: http.StatusOK, body: `{"stored":5,"refs_moved":2}`, want: project.PushSummary{Stored: 5, RefsMoved: 2}},
		{name: "missing fields default to zero", status: http.StatusOK, body: `{"stored":4}`, want: project.PushSummary{Stored: 4}},
		{name: "unknown fields are ignored", status: http.StatusOK, body: `{"stored":1,"refs_moved":1,"extra":true}`, want: project.PushSummary{Stored: 1, RefsMoved: 1}},
		{name: "200 ok is a zero summary", status: http.StatusOK, body: "ok", debug: true},
		{name: "empty body is a zero summary", status: http.StatusOK, body: "", debug: true},
		{name: "negative count is a zero summary", status: http.StatusOK, body: `{"stored":-1}`, debug: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, logs := newClient(t, recordingHandler(new(seen), respond(tc.status, []byte(tc.body), "Content-Type", "application/json")))

			s, err := c.SyncPush(context.Background(), samplePack())
			require.NoError(t, err)
			require.Equal(t, tc.want, s)

			debug := debugRecords(logs)
			if !tc.debug {
				require.Empty(t, debug)
				return
			}
			require.Len(t, debug, 1)
			require.Contains(t, debug[0].Message, "summary")
		})
	}
}

func TestSyncPushSummaryThatFailsMidRead(t *testing.T) {
	body := &trackingBody{r: strings.NewReader(`{"stored":`), err: errSentinel}
	c, logs := newClientWithDoer(t, bodyDoer(http.StatusCreated, body))

	s, err := c.SyncPush(context.Background(), samplePack())
	require.NoError(t, err)
	require.Equal(t, project.PushSummary{}, s)
	require.Len(t, debugRecords(logs), 1)
	require.True(t, body.closed)
}

func TestSyncPushStatuses(t *testing.T) {
	for _, tc := range []struct {
		status   int
		kind     remote.Kind
		sentinel error
	}{
		{status: http.StatusConflict, kind: remote.KindProtocol, sentinel: remote.ErrConflict},
		{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrUnauthorized},
		{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrForbidden},
		{status: http.StatusNotFound, kind: remote.KindHTTP, sentinel: remote.ErrNotFound},
		{status: http.StatusInternalServerError, kind: remote.KindHTTP},
		{status: http.StatusBadRequest, kind: remote.KindHTTP},
		{status: http.StatusAccepted, kind: remote.KindHTTP},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(tc.status, []byte("ref CAS refused"), remote.MinVersionHeader, "0.1.0")))

			s, err := c.SyncPush(context.Background(), samplePack())
			require.Equal(t, project.PushSummary{}, s)
			re := requireRemoteError(t, err, tc.kind)
			require.Equal(t, "sync_push", re.Op)
			require.Equal(t, http.MethodPost, re.Method)
			require.Equal(t, "https://acme.example.com"+projectPath, re.URL)
			require.Equal(t, tc.status, re.StatusCode)
			require.Equal(t, "ref CAS refused", re.Message)
			require.Equal(t, "ref CAS refused", re.Body)
			require.Equal(t, "0.1.0", re.ServerMinVersion)
			if tc.sentinel != nil {
				require.ErrorIs(t, err, tc.sentinel)
			}
		})
	}
}

func TestSyncPushConflictText(t *testing.T) {
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusConflict, []byte("ref CAS refused"))))
	_, err := c.SyncPush(context.Background(), samplePack())
	require.EqualError(t, err, "Protocol error: ref CAS refused")
}

func TestSyncPushEncodeFailureSendsNothing(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))

	bad := syncproto.Pack{Objects: []syncproto.ObjectRecord{{Family: syncproto.Family(99), Key: "k"}}}
	_, err := c.SyncPush(context.Background(), bad)
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.ErrorIs(t, err, syncproto.ErrUnknownFamily)
	require.True(t, strings.HasPrefix(re.Message, "failed to encode sync pack: "), re.Message)
	require.Zero(t, got.calls)
}

func TestSyncPushConnectionFailure(t *testing.T) {
	doer := &http.Client{Transport: roundTripError(errSentinel)}
	c, _ := newClientWithDoer(t, doer)
	_, err := c.SyncPush(context.Background(), samplePack())
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.True(t, re.Retryable())
}

func TestSyncPullRequest(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, mustEncodePack(t, syncproto.Pack{}))))

	w := syncproto.Wants{Refs: []string{"main"}, Haves: []string{"CHANGEA", "CHANGEB"}}
	_, err := c.SyncPull(context.Background(), w)
	require.NoError(t, err)

	require.Equal(t, 1, got.calls)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, projectPath, got.requestURI)
	require.Equal(t, "sync/1", got.header.Get("Atomic-Protocol"))
	require.Equal(t, "application/vnd.atomic.sync.v1", got.header.Get("Content-Type"))
	require.Equal(t, "atomic-0.22.1", got.header.Get("User-Agent"))
	require.Equal(t, "zstd, gzip, deflate", got.header.Get("Accept-Encoding"))

	// A GET with a body of fixed length.
	require.Empty(t, got.transferEncoding)
	require.Greater(t, got.contentLength, int64(0))
	require.Equal(t, int64(len(got.body)), got.contentLength)

	decoded, err := syncproto.NewCodec().DecodeWants(bytes.NewReader(got.body))
	require.NoError(t, err)
	require.Equal(t, w, decoded)
}

func TestSyncPullDecodes(t *testing.T) {
	for _, contentType := range []string{"application/vnd.atomic.sync.v1", "application/octet-stream", ""} {
		t.Run(contentType, func(t *testing.T) {
			p := samplePack()
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, mustEncodePack(t, p), "Content-Type", contentType)))

			got, err := c.SyncPull(context.Background(), syncproto.All(nil))
			require.NoError(t, err)
			require.Equal(t, p, got)
		})
	}
}

func TestSyncPullStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   remote.Kind
	}{
		{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed},
		{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed},
		{status: http.StatusNotFound, kind: remote.KindHTTP},
		{status: http.StatusConflict, kind: remote.KindHTTP},
		{status: http.StatusInternalServerError, kind: remote.KindHTTP},
		{status: http.StatusCreated, kind: remote.KindHTTP},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(tc.status, []byte("nope"))))

			p, err := c.SyncPull(context.Background(), syncproto.All(nil))
			require.Equal(t, syncproto.Pack{}, p)
			re := requireRemoteError(t, err, tc.kind)
			require.Equal(t, "sync_pull", re.Op)
			require.Equal(t, http.MethodGet, re.Method)
			require.Equal(t, tc.status, re.StatusCode)
			require.Equal(t, "nope", re.Message)
		})
	}
}

func TestSyncPullVersionMismatch(t *testing.T) {
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte("garbage"), remote.MinVersionHeader, "0.16.2")))

	_, err := c.SyncPull(context.Background(), syncproto.All(nil))
	re := requireRemoteError(t, err, remote.KindVersionMismatch)
	require.Equal(t, "0.16.2", re.ServerMinVersion)
	require.Equal(t, clientVersion, re.ClientVersion)
	require.Equal(t, http.StatusOK, re.StatusCode)

	var de *syncproto.DecodeError
	require.ErrorAs(t, err, &de)
	require.Contains(t, err.Error(), "Client version: 0.22.1")
	require.Contains(t, err.Error(), "Server min-version: 0.16.2")
}

func TestSyncPullPostcardFailureIsAVersionMismatch(t *testing.T) {
	// Valid zstd, but not a postcard pack: a sequence length with no
	// objects after it.
	body, err := syncproto.NewCodec().EncodeWants(syncproto.Wants{Refs: []string{"a", "b", "c"}})
	require.NoError(t, err)
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, body)))

	_, err = c.SyncPull(context.Background(), syncproto.All(nil))
	re := requireRemoteError(t, err, remote.KindVersionMismatch)
	require.Empty(t, re.ServerMinVersion)
	var de *syncproto.DecodeError
	require.ErrorAs(t, err, &de)
}

// bigPack returns a pack whose one object is n incompressible bytes.
func bigPack(n int) syncproto.Pack {
	r := rand.New(rand.NewPCG(1, 2))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return syncproto.Pack{Objects: []syncproto.ObjectRecord{{Family: syncproto.Change, Key: "BIG", Bytes: b}}}
}

func TestSyncPullPastTheMaxPackSize(t *testing.T) {
	body := mustEncodePack(t, bigPack(4096))
	for _, tc := range []struct {
		name string
		pull func(*project.Client) error
	}{
		{name: "SyncPull", pull: func(c *project.Client) error {
			_, err := c.SyncPull(context.Background(), syncproto.All(nil))
			return err
		}},
		{name: "SyncPullTo", pull: func(c *project.Client) error {
			_, err := c.SyncPullTo(context.Background(), syncproto.All(nil), new(recordingSink))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, body)), project.WithMaxPackSize(1024))

			err := tc.pull(c)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.ErrorIs(t, err, syncproto.ErrTooLarge)
			require.True(t, strings.HasPrefix(re.Message, "failed to decode sync pack: "), re.Message)
			require.EqualError(t, err, "Protocol error: failed to decode sync pack: "+syncproto.ErrTooLarge.Error()+": more than 1024 bytes")
		})
	}

	t.Run("within the limit", func(t *testing.T) {
		c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, body)), project.WithMaxPackSize(1<<20))
		_, err := c.SyncPull(context.Background(), syncproto.All(nil))
		require.NoError(t, err)
	})
}

func TestSyncPullReadFailure(t *testing.T) {
	enc := mustEncodePack(t, samplePack())
	body := &trackingBody{r: bytes.NewReader(enc[:len(enc)/2]), err: errSentinel}
	c, _ := newClientWithDoer(t, bodyDoer(http.StatusOK, body))

	_, err := c.SyncPull(context.Background(), syncproto.All(nil))
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.True(t, re.Retryable())
	require.True(t, body.closed)
}

func TestSyncPullReadFailureAfterTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	enc := mustEncodePack(t, samplePack())
	body := &trackingBody{r: bytes.NewReader(enc[:len(enc)/2]), err: context.Canceled}
	doer := bodyDoer(http.StatusOK, body)
	c, _ := newClientWithDoer(t, doerFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := doer.Do(req)
		cancel()
		return resp, err
	}))

	_, err := c.SyncPull(ctx, syncproto.All(nil))
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.True(t, re.ContextDone)
	require.False(t, re.Retryable())
	require.ErrorIs(t, err, context.Canceled)
}

func TestSyncPullEncodeFailureSendsNothing(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))

	_, err := c.SyncPull(context.Background(), syncproto.Wants{Refs: []string{"\xff"}})
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.ErrorIs(t, err, syncproto.ErrInvalidUTF8)
	require.True(t, strings.HasPrefix(re.Message, "failed to encode sync wants: "), re.Message)
	require.Zero(t, got.calls)
}

// recordingSink keeps every object it is given, and fails with failWith
// on object number failAt, counting from 1, when failAt is set.
type recordingSink struct {
	objects  []syncproto.ObjectRecord
	failAt   int
	failWith error
}

func (s *recordingSink) PutObject(_ context.Context, obj syncproto.ObjectRecord) error {
	if s.failAt > 0 && len(s.objects)+1 == s.failAt {
		return s.failWith
	}
	s.objects = append(s.objects, obj)
	return nil
}

func TestSyncPullToStreamsInOrder(t *testing.T) {
	var got seen
	p := samplePack()
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, mustEncodePack(t, p))))

	w := syncproto.Advertise("main")
	sink := new(recordingSink)
	refs, err := c.SyncPullTo(context.Background(), w, sink)
	require.NoError(t, err)
	require.Equal(t, p.Objects, sink.objects)
	require.Equal(t, p.Refs, refs)

	// The same request as SyncPull.
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, projectPath, got.requestURI)
	require.Equal(t, "sync/1", got.header.Get("Atomic-Protocol"))
	require.Equal(t, "application/vnd.atomic.sync.v1", got.header.Get("Content-Type"))
	decoded, err := syncproto.NewCodec().DecodeWants(bytes.NewReader(got.body))
	require.NoError(t, err)
	require.Equal(t, w, decoded)
}

func TestSyncPullToSinkErrorAbortsThePull(t *testing.T) {
	body := &trackingBody{r: bytes.NewReader(mustEncodePack(t, samplePack()))}
	c, _ := newClientWithDoer(t, bodyDoer(http.StatusOK, body))

	sink := &recordingSink{failAt: 2, failWith: errSentinel}
	refs, err := c.SyncPullTo(context.Background(), syncproto.All(nil), sink)
	require.Nil(t, refs)
	require.ErrorIs(t, err, errSentinel)
	re := requireRemoteError(t, err, remote.KindOther)
	require.Equal(t, "sync_pull", re.Op)
	require.False(t, re.Retryable())
	require.Equal(t, samplePack().Objects[:1], sink.objects)
	require.True(t, body.closed)
}

func TestSyncPullToFailsAsSyncPullDoes(t *testing.T) {
	t.Run("status", func(t *testing.T) {
		c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusForbidden, []byte("no"))))
		_, err := c.SyncPullTo(context.Background(), syncproto.All(nil), new(recordingSink))
		requireRemoteError(t, err, remote.KindAuthenticationFailed)
	})
	t.Run("version mismatch", func(t *testing.T) {
		c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte("garbage"), remote.MinVersionHeader, "0.16.2")))
		_, err := c.SyncPullTo(context.Background(), syncproto.All(nil), new(recordingSink))
		re := requireRemoteError(t, err, remote.KindVersionMismatch)
		require.Equal(t, "0.16.2", re.ServerMinVersion)
		require.Equal(t, clientVersion, re.ClientVersion)
	})
	t.Run("read failure", func(t *testing.T) {
		enc := mustEncodePack(t, samplePack())
		body := &trackingBody{r: bytes.NewReader(enc[:len(enc)/2]), err: errSentinel}
		c, _ := newClientWithDoer(t, bodyDoer(http.StatusOK, body))
		_, err := c.SyncPullTo(context.Background(), syncproto.All(nil), new(recordingSink))
		requireRemoteError(t, err, remote.KindConnectionFailed)
		require.ErrorIs(t, err, errSentinel)
		require.True(t, body.closed)
	})
}

func TestSyncPullToRefusesANilSink(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))
	_, err := c.SyncPullTo(context.Background(), syncproto.All(nil), nil)
	requireRemoteError(t, err, remote.KindOther)
	require.Zero(t, got.calls)
}

// doerFunc adapts a function into a project.Doer.
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// roundTripError is a transport whose every round trip fails with err.
func roundTripError(err error) http.RoundTripper {
	return remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	})
}
