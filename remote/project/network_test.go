package project_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"github.com/z5labs/bedrock/host/hosttest"

	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/syncproto"
)

// TestSyncOverARealHTTPServer sends a push and a pull through net/http's
// own client and server, over hosttest's in-memory network, so the GET
// body and the buffered POST are seen as a real server sees them.
func TestSyncOverARealHTTPServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		h := hosttest.New(t)
		ln, err := h.ListenTCP(ctx, "tcp", ":8080")
		require.NoError(t, err)

		type request struct {
			method        string
			requestURI    string
			contentLength int64
			protocol      string
			body          []byte
		}
		requests := make(chan request, 2)
		p := samplePack()
		packBody := mustEncodePack(t, p)
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			requests <- request{
				method:        r.Method,
				requestURI:    r.RequestURI,
				contentLength: r.ContentLength,
				protocol:      r.Header.Get("Atomic-Protocol"),
				body:          body,
			}
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"stored":3,"refs_moved":2}`)
				return
			}
			w.Header().Set("Content-Type", syncproto.ContentType)
			_, _ = w.Write(packBody)
		})}
		served := make(chan error, 1)
		go func() { served <- srv.Serve(ln) }()
		// If a require below fails, these still stop the bubble's
		// goroutines, so synctest reports the failure rather than a
		// deadlock. Closing twice is harmless.
		defer srv.Close()
		<-h.Listening(":8080")

		tr := &http.Transport{DialContext: h.DialTCP}
		defer tr.CloseIdleConnections()
		base, err := url.Parse("http://acme.internal:8080")
		require.NoError(t, err)
		u, err := serverurl.NewProject(base, "w", "p")
		require.NoError(t, err)
		c, err := project.New(u, &http.Client{Transport: tr}, remotetest.NewLogRecorder().Logger(), clientVersion)
		require.NoError(t, err)

		summary, err := c.SyncPush(ctx, p)
		require.NoError(t, err)
		require.Equal(t, project.PushSummary{Stored: 3, RefsMoved: 2}, summary)
		push := <-requests
		require.Equal(t, http.MethodPost, push.method)
		require.Equal(t, projectPath, push.requestURI)
		require.Equal(t, "sync/1", push.protocol)
		require.Equal(t, int64(len(push.body)), push.contentLength)
		require.Equal(t, packBody, push.body)

		w := syncproto.Wants{Refs: []string{"main"}, Haves: []string{"CHANGEA"}}
		got, err := c.SyncPull(ctx, w)
		require.NoError(t, err)
		require.Equal(t, p, got)
		pull := <-requests
		require.Equal(t, http.MethodGet, pull.method)
		require.Equal(t, projectPath, pull.requestURI)
		require.Equal(t, "sync/1", pull.protocol)
		require.Greater(t, pull.contentLength, int64(0))
		require.Equal(t, int64(len(pull.body)), pull.contentLength)
		wants, err := syncproto.NewCodec().DecodeWants(bytes.NewReader(pull.body))
		require.NoError(t, err)
		require.Equal(t, w, wants)

		require.NoError(t, srv.Close())
		tr.CloseIdleConnections()
		require.ErrorIs(t, <-served, http.ErrServerClosed)
	})
}
