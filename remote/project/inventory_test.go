package project_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/views"
)

const refsViewsPath = "/workspaces/w/projects/p/refs/views"

func TestListViewRefsRequest(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))

	infos, err := c.ListViewRefs(context.Background())
	require.NoError(t, err)
	require.Nil(t, infos)

	require.Equal(t, 1, got.calls)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, refsViewsPath, got.requestURI)
	require.Empty(t, got.header.Values("Atomic-Protocol"))
	require.Empty(t, got.header.Values("Content-Type"))
	require.Empty(t, got.body)
	require.Equal(t, "atomic-0.22.1", got.header.Get("User-Agent"))
}

func TestListViewRefsFromABaseURLWithoutCode(t *testing.T) {
	var got seen
	doer := remotetest.Client(recordingHandler(&got, respond(http.StatusOK, nil)))
	u, _, err := serverurl.ParseProject("https://acme.example.com/workspaces/w/projects/p")
	require.NoError(t, err)
	c, err := project.New(u, doer, remotetest.NewLogRecorder().Logger(), clientVersion)
	require.NoError(t, err)

	_, err = c.ListViewRefs(context.Background())
	require.NoError(t, err)
	require.Equal(t, refsViewsPath, got.requestURI)
}

func TestListViewRefsParses(t *testing.T) {
	body := "main\tshared\t-\t3\tSTATEA\tSETA\r\n" +
		"\n" +
		"feat\tDRAFT\tmain\t5\t-\n"
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(body), "Content-Type", "text/plain")))

	infos, err := c.ListViewRefs(context.Background())
	require.NoError(t, err)
	require.Equal(t, []views.Info{
		{Name: "main", Scope: ident.ScopeShared, ChangeCount: 3, State: "STATEA", SetID: "SETA"},
		{Name: "feat", Scope: ident.Scope("DRAFT"), Parent: "main", ChangeCount: 5},
	}, infos)
}

func TestListViewRefsBadRow(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{
			name: "bad count on line 2",
			body: "main\tshared\t-\t3\tSTATEA\n" + "dev\tshared\t-\tnot-a-count\t-\n",
			want: "Invalid view inventory row 2: invalid change count 'not-a-count'",
		},
		{
			name: "unknown scope after a blank line",
			body: "\n" + "dev\tunknown\t-\t0\t-\n",
			want: "Invalid view inventory row 2: unsupported view scope 'unknown'",
		},
		{
			name: "an older server's JSON",
			body: `{"workspace":"w"}`,
			want: "Invalid view inventory row 1: expected 5 or 6 tab-separated fields, got 1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(tc.body))))

			infos, err := c.ListViewRefs(context.Background())
			require.Nil(t, infos)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.Equal(t, "list_view_refs", re.Op)
			require.Equal(t, http.MethodGet, re.Method)
			require.Equal(t, "https://acme.example.com"+refsViewsPath, re.URL)
			require.Equal(t, tc.want, re.Message)
			require.EqualError(t, err, "Protocol error: "+tc.want)

			var ie *views.InventoryError
			require.ErrorAs(t, err, &ie)
		})
	}
}

func TestListViewRefsBadRowLineNumber(t *testing.T) {
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte("main\tshared\t-\t3\tS\n"+"dev\tshared\t-\tx\t-\n"))))
	_, err := c.ListViewRefs(context.Background())
	var ie *views.InventoryError
	require.ErrorAs(t, err, &ie)
	require.Equal(t, 2, ie.Line)
}

func TestListViewRefsTooLarge(t *testing.T) {
	body := "main\tshared\t-\t3\tSTATEA\n"
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(body))), project.WithMaxPackSize(int64(len(body)-1)))

	_, err := c.ListViewRefs(context.Background())
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.Contains(t, re.Message, "larger than")

	c, _ = newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(body))), project.WithMaxPackSize(int64(len(body))))
	infos, err := c.ListViewRefs(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
}

func TestListViewRefsStatuses(t *testing.T) {
	for _, tc := range []struct {
		status   int
		kind     remote.Kind
		sentinel error
		auth     bool
	}{
		{status: http.StatusNotFound, kind: remote.KindRepositoryNotFound, sentinel: remote.ErrNotFound},
		{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrUnauthorized, auth: true},
		{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrForbidden, auth: true},
		{status: http.StatusInternalServerError, kind: remote.KindHTTP},
		{status: http.StatusConflict, kind: remote.KindHTTP, sentinel: remote.ErrConflict},
		{status: http.StatusCreated, kind: remote.KindHTTP},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(tc.status, []byte("nope"), remote.MinVersionHeader, "0.1.0")))

			infos, err := c.ListViewRefs(context.Background())
			require.Nil(t, infos)
			re := requireRemoteError(t, err, tc.kind)
			require.Equal(t, "list_view_refs", re.Op)
			require.Equal(t, http.MethodGet, re.Method)
			require.Equal(t, "https://acme.example.com"+refsViewsPath, re.URL)
			require.Equal(t, tc.status, re.StatusCode)
			require.Equal(t, "0.1.0", re.ServerMinVersion)
			require.Equal(t, tc.auth, re.IsAuth())
			if tc.sentinel != nil {
				require.ErrorIs(t, err, tc.sentinel)
			}
		})
	}
}

func TestListViewRefsNotFoundText(t *testing.T) {
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusNotFound, []byte("nope"))))
	_, err := c.ListViewRefs(context.Background())
	require.EqualError(t, err, "Repository not found: https://acme.example.com"+refsViewsPath)
	require.True(t, errors.Is(err, remote.ErrNotFound))
}

func TestListViewRefsReadFailure(t *testing.T) {
	body := &trackingBody{r: strings.NewReader("main\tshared"), err: errSentinel}
	c, _ := newClientWithDoer(t, bodyDoer(http.StatusOK, body))

	_, err := c.ListViewRefs(context.Background())
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.False(t, re.ContextDone)
	require.True(t, body.closed)
}

func TestListViewRefsConnectionFailure(t *testing.T) {
	c, _ := newClientWithDoer(t, &http.Client{Transport: roundTripError(errSentinel)})
	_, err := c.ListViewRefs(context.Background())
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.Equal(t, "list_view_refs", re.Op)
}

// minVersionWarnings returns the min-version warnings logs holds.
func minVersionWarnings(logs *remotetest.LogRecorder) []slog.Record {
	var out []slog.Record
	for _, rec := range logs.Records() {
		if rec.Level == slog.LevelWarn {
			out = append(out, rec)
		}
	}
	return out
}

func TestNewerMinVersionWarns(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*project.Client) error
		body string
		path string
	}{
		{
			name: "ListViewRefs",
			call: func(c *project.Client) error {
				_, err := c.ListViewRefs(context.Background())
				return err
			},
			path: refsViewsPath,
		},
		{
			name: "FetchProjectIdentity",
			call: func(c *project.Client) error {
				_, err := c.FetchProjectIdentity(context.Background())
				return err
			},
			body: `{"bound":false}`,
			path: projectPath + "?project-identity=1",
		},
		{
			name: "BindProjectIdentity",
			call: func(c *project.Client) error {
				_, err := c.BindProjectIdentity(context.Background(), []byte(`{}`))
				return err
			},
			path: projectPath + "?project-identity=1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, logs := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(tc.body), remote.MinVersionHeader, "9.0.0")))
			require.NoError(t, tc.call(c))

			warnings := minVersionWarnings(logs)
			require.Len(t, warnings, 1)
			var shown string
			warnings[0].Attrs(func(a slog.Attr) bool {
				if a.Key == "url" {
					shown = a.Value.String()
				}
				return true
			})
			require.Equal(t, "https://acme.example.com"+tc.path, shown)
		})
	}
}
