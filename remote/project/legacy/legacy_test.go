package legacy_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/project/legacy"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/views"
)

const (
	clientVersion = "0.22.1"
	codeURL       = "https://acme.example.com/workspaces/w/projects/p/code"
	codePath      = "/workspaces/w/projects/p/code"
)

// jsonInfo is the generic JSON info blob an older server answers a query
// it does not recognise with (docs/remote/transport.md#feature-detection-by-response-shape).
const jsonInfo = `{"workspace":"w","project":"p","stack":"dev"}`

// errSentinel is a cause a test can match with errors.Is.
var errSentinel = errors.New("sentinel")

// reply is one canned response.
type reply struct {
	status int
	body   string
	header []string // name, value, name, value, ...
}

// request is what the server saw of one request.
type request struct {
	method     string
	requestURI string
	header     http.Header
	body       []byte
}

// server answers each request by its RawQuery and records what it saw.
// remotetest.Transport serves on the caller's goroutine, so no lock is
// needed.
type server struct {
	routes map[string]reply
	seen   []request
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.seen = append(s.seen, request{method: r.Method, requestURI: r.RequestURI, header: r.Header.Clone(), body: body})
	rep, ok := s.routes[r.URL.RawQuery]
	if !ok {
		http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusTeapot)
		return
	}
	for i := 0; i+1 < len(rep.header); i += 2 {
		w.Header().Set(rep.header[i], rep.header[i+1])
	}
	status := rep.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, rep.body)
}

func projectURL(t *testing.T) serverurl.Project {
	t.Helper()
	base, err := url.Parse("https://acme.example.com")
	require.NoError(t, err)
	u, err := serverurl.NewProject(base, "w", "p")
	require.NoError(t, err)
	return u
}

// newClient returns a Client whose requests s answers, in memory.
func newClient(t *testing.T, s *server, opts ...legacy.Option) *legacy.Client {
	t.Helper()
	return newClientWithDoer(t, remotetest.Client(s), opts...)
}

func newClientWithDoer(t *testing.T, doer project.Doer, opts ...legacy.Option) *legacy.Client {
	t.Helper()
	c, err := legacy.New(projectURL(t), doer, remotetest.NewLogRecorder().Logger(), clientVersion, opts...)
	require.NoError(t, err)
	return c
}

// serve returns a Client and the server answering it with routes.
func serve(t *testing.T, routes map[string]reply) (*legacy.Client, *server) {
	t.Helper()
	s := &server{routes: routes}
	return newClient(t, s), s
}

func requireRemoteError(t *testing.T, err error, kind remote.Kind) *remote.Error {
	t.Helper()
	var re *remote.Error
	require.ErrorAs(t, err, &re)
	require.Equal(t, kind, re.Kind, "error: %v", err)
	return re
}

// tagBody is a download_tag body: n as an 8-byte big-endian length, then
// data.
func tagBody(n uint64, data string) string {
	var prefix [8]byte
	binary.BigEndian.PutUint64(prefix[:], n)
	return string(prefix[:]) + data
}

func TestNewNamesEveryMissingArgument(t *testing.T) {
	c, err := legacy.New(serverurl.Project{}, nil, nil, "")
	require.Nil(t, c)
	require.ErrorIs(t, err, legacy.ErrInvalidArgument)

	msg := err.Error()
	require.Contains(t, msg, "the project URL is required")
	require.Contains(t, msg, "the Doer is required")
	require.Contains(t, msg, "the *slog.Logger is required")
	require.Contains(t, msg, "the client version is required")
}

func TestNewNamesOnlyWhatIsMissing(t *testing.T) {
	logger := remotetest.NewLogRecorder().Logger()
	c, err := legacy.New(projectURL(t), nil, logger, clientVersion)
	require.Nil(t, c)
	require.ErrorIs(t, err, legacy.ErrInvalidArgument)
	require.Contains(t, err.Error(), "the Doer is required")
	require.NotContains(t, err.Error(), "project URL")
	require.NotContains(t, err.Error(), "slog")
	require.NotContains(t, err.Error(), "client version")
}

func TestNewRefusesANilOption(t *testing.T) {
	doer := remotetest.Client(http.NotFoundHandler())
	c, err := legacy.New(projectURL(t), doer, remotetest.NewLogRecorder().Logger(), clientVersion, nil)
	require.Nil(t, c)
	require.ErrorIs(t, err, legacy.ErrInvalidArgument)
	require.Contains(t, err.Error(), "nil Option")
}

func TestURL(t *testing.T) {
	c, _ := serve(t, nil)
	require.Equal(t, codeURL, c.URL().String())
}

func TestUserAgent(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []legacy.Option
		want []string // nil means no User-Agent header
	}{
		{name: "default", want: []string{"atomic-0.22.1"}},
		{name: "WithUserAgent", opts: []legacy.Option{legacy.WithUserAgent("custom/1")}, want: []string{"custom/1"}},
		{name: "empty", opts: []legacy.Option{legacy.WithUserAgent("")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &server{routes: map[string]reply{"views": {}}}
			c := newClient(t, s, tc.opts...)
			_, err := c.ListViews(context.Background())
			require.NoError(t, err)
			require.Len(t, s.seen, 1)
			require.Equal(t, tc.want, s.seen[0].header.Values("User-Agent"))
			require.Equal(t, "zstd, gzip, deflate", s.seen[0].header.Get("Accept-Encoding"))
		})
	}
}

func TestStateRequest(t *testing.T) {
	c, s := serve(t, map[string]reply{"view=dev&state=": {body: "42 ABC123 DEF456\n"}})

	st, err := c.State(context.Background(), "dev")
	require.NoError(t, err)
	require.Equal(t, legacy.State{Position: 42, Merkle: "ABC123", TagMerkle: "DEF456"}, st)

	require.Len(t, s.seen, 1)
	got := s.seen[0]
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, codePath+"?view=dev&state=", got.requestURI)
	require.Empty(t, got.body)
	require.Empty(t, got.header.Values("Content-Type"))
	require.Empty(t, got.header.Values("Atomic-Protocol"))
}

func TestStateBodies(t *testing.T) {
	for _, tc := range []struct {
		body string
		want legacy.State
	}{
		{body: "42 ABC123 DEF456", want: legacy.State{Position: 42, Merkle: "ABC123", TagMerkle: "DEF456"}},
		{body: "42 ABC DEF SID32", want: legacy.State{Position: 42, Merkle: "ABC", TagMerkle: "DEF", SetID: "SID32"}},
		{body: "42 A B S X", want: legacy.State{Position: 42, Merkle: "A", TagMerkle: "B", SetID: "S"}},
		{body: "-", want: legacy.State{Empty: true}},
	} {
		t.Run(tc.body, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{"view=dev&state=": {body: tc.body}})
			st, err := c.State(context.Background(), "dev")
			require.NoError(t, err)
			require.Equal(t, tc.want, st)
		})
	}
}

func TestStateBodiesThatDoNotParse(t *testing.T) {
	for _, tc := range []struct {
		body     string
		msg      string
		sentinel error
	}{
		{body: "", msg: "Protocol error: Failed to parse state response: empty state response", sentinel: legacy.ErrEmpty},
		{body: "42 ABC", msg: "Protocol error: Failed to parse state response: invalid state format: '42 ABC'", sentinel: legacy.ErrInvalidFormat},
		{body: "x A B", msg: "Protocol error: Failed to parse state response: invalid position: 'x'", sentinel: legacy.ErrInvalidNumber},
	} {
		t.Run(tc.body, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{"view=dev&state=": {body: tc.body}})
			st, err := c.State(context.Background(), "dev")
			require.Equal(t, legacy.State{}, st)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.EqualError(t, err, tc.msg)
			require.ErrorIs(t, err, tc.sentinel)
			require.Equal(t, "get_state", re.Op)
			require.Equal(t, codeURL+"?view=dev&state=", re.URL)
			require.Equal(t, http.StatusOK, re.StatusCode)
		})
	}
}

func TestChangelist(t *testing.T) {
	body := "0.ABC.DEF\n" +
		"\n" +
		"  1.GHI.JKL.\r\n" +
		"   \n" +
		"2.A.B..\n" +
		"3.A.B.C\n"
	c, s := serve(t, map[string]reply{"view=dev&changelist=5": {body: body}})

	entries, err := c.Changelist(context.Background(), "dev", 5)
	require.NoError(t, err)
	require.Equal(t, []legacy.ChangelistEntry{
		{Sequence: 0, Hash: "ABC", Merkle: "DEF"},
		{Sequence: 1, Hash: "GHI", Merkle: "JKL", Tagged: true},
		{Sequence: 2, Hash: "A", Merkle: "B", Tagged: true},
		{Sequence: 3, Hash: "A", Merkle: "B"},
	}, entries)
	require.Equal(t, codePath+"?view=dev&changelist=5", s.seen[0].requestURI)
}

func TestChangelistEmpty(t *testing.T) {
	c, _ := serve(t, map[string]reply{"view=dev&changelist=0": {body: "\n \n"}})
	entries, err := c.Changelist(context.Background(), "dev", 0)
	require.NoError(t, err)
	require.Nil(t, entries)
}

func TestChangelistBadLineFailsTheResponse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		line     string
		msg      string
		sentinel error
	}{
		{name: "InvalidFormat", line: "0.ABC.", msg: "Protocol error: Failed to parse changelist entry: invalid changelist format: '0.ABC.'", sentinel: legacy.ErrInvalidFormat},
		{name: "InvalidSequence", line: "seq.ABC.DEF", msg: "Protocol error: Failed to parse changelist entry: invalid sequence number: 'seq'", sentinel: legacy.ErrInvalidNumber},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "0.ABC.DEF\n" + tc.line + "\n2.MNO.PQR\n"
			c, _ := serve(t, map[string]reply{"view=dev&changelist=0": {body: body}})

			entries, err := c.Changelist(context.Background(), "dev", 0)
			require.Nil(t, entries)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.EqualError(t, err, tc.msg)
			require.ErrorIs(t, err, tc.sentinel)
			require.Equal(t, "get_changelist", re.Op)
		})
	}
}

func TestID(t *testing.T) {
	c, s := serve(t, map[string]reply{"view=dev&id": {body: "  0f6c1d2e-uuid \r\n"}})
	id, err := c.ID(context.Background(), "dev")
	require.NoError(t, err)
	require.Equal(t, "0f6c1d2e-uuid", id)
	require.Equal(t, codePath+"?view=dev&id", s.seen[0].requestURI)
}

func TestDownloadTag(t *testing.T) {
	c, s := serve(t, map[string]reply{"tag=STATE": {body: tagBody(5, "hello") + "trailing bytes"}})
	data, err := c.DownloadTag(context.Background(), "STATE")
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data)
	require.Equal(t, codePath+"?tag=STATE", s.seen[0].requestURI)
}

func TestDownloadTagEmpty(t *testing.T) {
	c, _ := serve(t, map[string]reply{"tag=STATE": {body: tagBody(0, "")}})
	data, err := c.DownloadTag(context.Background(), "STATE")
	require.NoError(t, err)
	require.Empty(t, data)
}

func TestDownloadTagBadBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		msg  string
	}{
		{name: "empty", body: "", msg: "Protocol error: Tag response too short (missing length prefix)"},
		{name: "short", body: "\x00\x00\x00\x00\x00\x00\x05", msg: "Protocol error: Tag response too short (missing length prefix)"},
		{name: "truncated", body: tagBody(5, "hel"), msg: "Protocol error: Tag response truncated: expected 5 bytes, got 3"},
		{name: "a length past any body", body: tagBody(^uint64(0), "x"), msg: "Protocol error: Tag response truncated: expected 18446744073709551615 bytes, got 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{"tag=STATE": {body: tc.body}})
			data, err := c.DownloadTag(context.Background(), "STATE")
			require.Nil(t, data)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.EqualError(t, err, tc.msg)
			require.Equal(t, "download_tag", re.Op)
		})
	}
}

func TestListTags(t *testing.T) {
	body := "HASH1 v1.0\n" +
		"\n" +
		"  HASH2 release candidate two  \r\n" +
		"NOSPACE\n" +
		"HASH3  padded\n"
	c, s := serve(t, map[string]reply{"tags=dev": {body: body}})

	refs, err := c.ListTags(context.Background(), "dev")
	require.NoError(t, err)
	require.Equal(t, []legacy.TagRef{
		{Hash: "HASH1", Name: "v1.0"},
		{Hash: "HASH2", Name: "release candidate two"},
		{Hash: "HASH3", Name: " padded"},
	}, refs)
	require.Equal(t, codePath+"?tags=dev", s.seen[0].requestURI)
}

func TestListTagsNotFoundIsEmpty(t *testing.T) {
	c, _ := serve(t, map[string]reply{"tags=dev": {status: http.StatusNotFound, body: "no such view"}})
	refs, err := c.ListTags(context.Background(), "dev")
	require.NoError(t, err)
	require.Nil(t, refs)
}

func TestDownloadAttestationAndProvenance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		query    string
		op       string
		download func(*legacy.Client, string) ([]byte, error)
		notFound string
	}{
		{
			name:  "attestation",
			query: "attest=HASH",
			op:    "download_attestation",
			download: func(c *legacy.Client, h string) ([]byte, error) {
				return c.DownloadAttestation(context.Background(), h)
			},
			notFound: "Protocol error: Attestation not found: HASH",
		},
		{
			name:     "provenance",
			query:    "provenance=HASH",
			op:       "download_provenance",
			download: func(c *legacy.Client, h string) ([]byte, error) { return c.DownloadProvenance(context.Background(), h) },
			notFound: "Protocol error: Provenance graph not found: HASH",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := serve(t, map[string]reply{tc.query: {body: "ATTS\x00\x01raw"}})
			data, err := tc.download(c, "HASH")
			require.NoError(t, err)
			require.Equal(t, []byte("ATTS\x00\x01raw"), data)
			require.Equal(t, codePath+"?"+tc.query, s.seen[0].requestURI)

			c, _ = serve(t, map[string]reply{tc.query: {status: http.StatusNotFound, body: "gone"}})
			data, err = tc.download(c, "HASH")
			require.Nil(t, data)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.EqualError(t, err, tc.notFound)
			require.Equal(t, tc.op, re.Op)
			require.Equal(t, http.StatusNotFound, re.StatusCode)
			require.ErrorIs(t, err, remote.ErrNotFound)
		})
	}
}

func TestProvenanceList(t *testing.T) {
	c, s := serve(t, map[string]reply{"provenance-list": {body: "H1\n\n  H2 \r\nH3"}})
	hashes, err := c.ProvenanceList(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"H1", "H2", "H3"}, hashes)
	require.Equal(t, codePath+"?provenance-list", s.seen[0].requestURI)

	c, _ = serve(t, map[string]reply{"provenance-list": {status: http.StatusNotFound}})
	hashes, err = c.ProvenanceList(context.Background())
	require.NoError(t, err)
	require.Nil(t, hashes)
}

func TestListViews(t *testing.T) {
	body := "dev\tshared\t-\t3\t2AAAAAAAAAAA\tSET\n" +
		"\n" +
		"feature\tdraft\tdev\t2\tXYZ\r\n" +
		"bare\tshared\n" +
		"no-scope\n"
	c, s := serve(t, map[string]reply{"views": {body: body}})

	infos, err := c.ListViews(context.Background())
	require.NoError(t, err)
	require.Equal(t, []views.Info{
		{Name: "dev", Scope: "shared", ChangeCount: 3, State: "2AAAAAAAAAAA", SetID: "SET"},
		{Name: "feature", Scope: "draft", Parent: "dev", ChangeCount: 2, State: "XYZ"},
		{Name: "bare", Scope: "shared"},
	}, infos)
	require.Equal(t, codePath+"?views", s.seen[0].requestURI)
}

func TestListViewsJSONInfoBlobIsEmpty(t *testing.T) {
	c, _ := serve(t, map[string]reply{"views": {body: jsonInfo, header: []string{"Content-Type", "application/json"}}})
	infos, err := c.ListViews(context.Background())
	require.NoError(t, err)
	require.Nil(t, infos)
}

func TestListViewsNotFoundIsEmpty(t *testing.T) {
	c, _ := serve(t, map[string]reply{"views": {status: http.StatusNotFound}})
	infos, err := c.ListViews(context.Background())
	require.NoError(t, err)
	require.Nil(t, infos)
}

func TestViewManifest(t *testing.T) {
	text := "\n  \ndev\tshared\t-\tSTATE\nCHANGE1\n"
	c, s := serve(t, map[string]reply{"view-manifest=dev": {body: text}})

	got, ok, err := c.ViewManifest(context.Background(), "dev")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, text, got)
	require.Equal(t, codePath+"?view-manifest=dev", s.seen[0].requestURI)
}

func TestViewManifestNone(t *testing.T) {
	for _, tc := range []struct {
		name string
		rep  reply
	}{
		{name: "404", rep: reply{status: http.StatusNotFound, body: "no such view"}},
		{name: "empty", rep: reply{body: ""}},
		{name: "blank", rep: reply{body: " \n\t\r\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{"view-manifest=dev": tc.rep})
			got, ok, err := c.ViewManifest(context.Background(), "dev")
			require.NoError(t, err)
			require.False(t, ok)
			require.Empty(t, got)
		})
	}
}

func TestViewManifestUnsupported(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "JSON info blob", body: jsonInfo},
		// Only the first non-blank line is looked at.
		{name: "tab after the first line", body: "\nheader\ndev\tshared\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{"view-manifest=dev": {body: tc.body}})
			got, ok, err := c.ViewManifest(context.Background(), "dev")
			require.False(t, ok)
			require.Empty(t, got)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.EqualError(t, err, "Protocol error: server does not support view manifests (?view-manifest)")
			require.Equal(t, "get_view_manifest", re.Op)
		})
	}
}

// call is one read, made against c, reduced to its error.
type call struct {
	name  string
	op    string
	query string
	do    func(c *legacy.Client) error
}

// calls is every read, each asking for view "dev", state "STATE" or hash
// "HASH".
var calls = []call{
	{name: "State", op: "get_state", query: "view=dev&state=", do: func(c *legacy.Client) error {
		_, err := c.State(context.Background(), "dev")
		return err
	}},
	{name: "Changelist", op: "get_changelist", query: "view=dev&changelist=0", do: func(c *legacy.Client) error {
		_, err := c.Changelist(context.Background(), "dev", 0)
		return err
	}},
	{name: "ID", op: "get_id", query: "view=dev&id", do: func(c *legacy.Client) error {
		_, err := c.ID(context.Background(), "dev")
		return err
	}},
	{name: "DownloadTag", op: "download_tag", query: "tag=STATE", do: func(c *legacy.Client) error {
		_, err := c.DownloadTag(context.Background(), "STATE")
		return err
	}},
	{name: "ListTags", op: "list_remote_tags", query: "tags=dev", do: func(c *legacy.Client) error {
		_, err := c.ListTags(context.Background(), "dev")
		return err
	}},
	{name: "DownloadAttestation", op: "download_attestation", query: "attest=HASH", do: func(c *legacy.Client) error {
		_, err := c.DownloadAttestation(context.Background(), "HASH")
		return err
	}},
	{name: "DownloadProvenance", op: "download_provenance", query: "provenance=HASH", do: func(c *legacy.Client) error {
		_, err := c.DownloadProvenance(context.Background(), "HASH")
		return err
	}},
	{name: "ProvenanceList", op: "get_provenance_list", query: "provenance-list", do: func(c *legacy.Client) error {
		_, err := c.ProvenanceList(context.Background())
		return err
	}},
	{name: "ListViews", op: "list_views", query: "views", do: func(c *legacy.Client) error {
		_, err := c.ListViews(context.Background())
		return err
	}},
	{name: "ViewManifest", op: "get_view_manifest", query: "view-manifest=dev", do: func(c *legacy.Client) error {
		_, _, err := c.ViewManifest(context.Background(), "dev")
		return err
	}},
}

func TestNotFoundKinds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    remote.Kind
		subject string
		msg     string
	}{
		{name: "State", kind: remote.KindViewNotFound, subject: "dev", msg: "View not found: dev"},
		{name: "Changelist", kind: remote.KindViewNotFound, subject: "dev", msg: "View not found: dev"},
		{name: "ID", kind: remote.KindViewNotFound, subject: "dev", msg: "View not found: dev"},
		{name: "DownloadTag", kind: remote.KindTagNotFound, subject: "STATE", msg: "Tag not found for state: STATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl := callNamed(t, tc.name)
			c, _ := serve(t, map[string]reply{cl.query: {status: http.StatusNotFound, body: "nope", header: []string{remote.MinVersionHeader, "0.1.0"}}})

			err := cl.do(c)
			re := requireRemoteError(t, err, tc.kind)
			require.EqualError(t, err, tc.msg)
			require.Equal(t, tc.subject, re.Subject)
			require.Equal(t, cl.op, re.Op)
			require.Equal(t, http.MethodGet, re.Method)
			require.Equal(t, codeURL+"?"+cl.query, re.URL)
			require.Equal(t, http.StatusNotFound, re.StatusCode)
			require.Equal(t, "0.1.0", re.ServerMinVersion)
			require.ErrorIs(t, err, remote.ErrNotFound)
		})
	}
}

func callNamed(t *testing.T, name string) call {
	t.Helper()
	for _, c := range calls {
		if c.name == name {
			return c
		}
	}
	require.FailNow(t, "no call named "+name)
	return call{}
}

func TestStatuses(t *testing.T) {
	for _, cl := range calls {
		for _, tc := range []struct {
			status   int
			kind     remote.Kind
			sentinel error
		}{
			{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrUnauthorized},
			{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrForbidden},
			{status: http.StatusInternalServerError, kind: remote.KindHTTP},
			{status: http.StatusCreated, kind: remote.KindHTTP},
		} {
			t.Run(cl.name+"/"+http.StatusText(tc.status), func(t *testing.T) {
				c, _ := serve(t, map[string]reply{cl.query: {status: tc.status, body: "nope"}})

				err := cl.do(c)
				re := requireRemoteError(t, err, tc.kind)
				require.Equal(t, cl.op, re.Op)
				require.Equal(t, http.MethodGet, re.Method)
				require.Equal(t, codeURL+"?"+cl.query, re.URL)
				require.Equal(t, tc.status, re.StatusCode)
				require.Equal(t, "nope", re.Message)
				require.Equal(t, "nope", re.Body)
				if tc.sentinel != nil {
					require.ErrorIs(t, err, tc.sentinel)
				}
			})
		}
	}
}

func TestUnsafeValuesAreRefusedBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(c *legacy.Client) error
	}{
		{name: "State", do: func(c *legacy.Client) error { _, err := c.State(context.Background(), "feat#1"); return err }},
		{name: "Changelist", do: func(c *legacy.Client) error {
			_, err := c.Changelist(context.Background(), "feat#1", 0)
			return err
		}},
		{name: "ID", do: func(c *legacy.Client) error { _, err := c.ID(context.Background(), "feat#1"); return err }},
		{name: "DownloadTag", do: func(c *legacy.Client) error { _, err := c.DownloadTag(context.Background(), "a&b"); return err }},
		{name: "ListTags", do: func(c *legacy.Client) error { _, err := c.ListTags(context.Background(), "feat#1"); return err }},
		{name: "DownloadAttestation", do: func(c *legacy.Client) error {
			_, err := c.DownloadAttestation(context.Background(), " HASH")
			return err
		}},
		{name: "DownloadProvenance", do: func(c *legacy.Client) error {
			_, err := c.DownloadProvenance(context.Background(), "H\nASH")
			return err
		}},
		{name: "ViewManifest", do: func(c *legacy.Client) error {
			_, _, err := c.ViewManifest(context.Background(), "feat#1")
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := serve(t, nil)
			err := tc.do(c)
			re := requireRemoteError(t, err, remote.KindOther)
			require.Equal(t, codeURL, re.URL)
			require.Empty(t, s.seen)
		})
	}
}

func TestConnectionFailure(t *testing.T) {
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errSentinel
	})}
	c := newClientWithDoer(t, doer)
	for _, cl := range calls {
		t.Run(cl.name, func(t *testing.T) {
			err := cl.do(c)
			re := requireRemoteError(t, err, remote.KindConnectionFailed)
			require.ErrorIs(t, err, errSentinel)
			require.Equal(t, cl.op, re.Op)
			require.True(t, re.Retryable())
		})
	}
}

// failingBody yields its data and then fails with errSentinel.
type failingBody struct {
	r      io.Reader
	closed bool
}

func (b *failingBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, errSentinel
	}
	return n, err
}

func (b *failingBody) Close() error {
	b.closed = true
	return nil
}

func TestReadFailure(t *testing.T) {
	for _, cl := range calls {
		t.Run(cl.name, func(t *testing.T) {
			body := &failingBody{r: strings.NewReader("partial")}
			doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body:       body,
					Request:    req,
				}, nil
			})}
			c := newClientWithDoer(t, doer)

			err := cl.do(c)
			re := requireRemoteError(t, err, remote.KindConnectionFailed)
			require.ErrorIs(t, err, errSentinel)
			require.Equal(t, cl.op, re.Op)
			require.False(t, re.ContextDone)
			require.True(t, body.closed)
		})
	}
}

func TestBodyTooLarge(t *testing.T) {
	c, _ := serve(t, map[string]reply{"view=dev&id": {body: "0123456789"}})
	legacy.SetMaxBody(c, 9)
	_, err := c.ID(context.Background(), "dev")
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.EqualError(t, err, "Protocol error: response body is larger than 9 bytes")
	require.Equal(t, "get_id", re.Op)

	legacy.SetMaxBody(c, 10)
	id, err := c.ID(context.Background(), "dev")
	require.NoError(t, err)
	require.Equal(t, "0123456789", id)
}
