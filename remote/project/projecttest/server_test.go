package projecttest_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project/projecttest"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/views"
	"github.com/z5labs/atomic-go/zstdframe"
)

const (
	projectPath = "/workspaces/w/projects/p"
	codeURL     = "https://acme.example.com" + projectPath + "/code"
	identityURL = codeURL + "?project-identity=1"
	viewsURL    = "https://acme.example.com" + projectPath + "/refs/views"
)

// firstDocument and secondDocument are two different identity documents.
const (
	firstDocument  = `{"@type": "atomic:ProjectIdentity", "atomic:projectId": "AAAA"}`
	secondDocument = `{"@type": "atomic:ProjectIdentity", "atomic:projectId": "BBBB"}`
)

// response is what a test reads back from one request.
type response struct {
	status int
	header http.Header
	body   []byte
}

// do sends a request to the Server through remotetest.Client and reads the
// whole response.
func do(t *testing.T, srv *projecttest.Server, method, url string, header http.Header, body []byte) response {
	t.Helper()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, rd)
	require.NoError(t, err)
	for k, vs := range header {
		req.Header[k] = vs
	}
	resp, err := remotetest.Client(srv).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return response{status: resp.StatusCode, header: resp.Header, body: b}
}

func syncHeader() http.Header {
	return http.Header{
		syncproto.ProtocolHeader: {syncproto.ProtocolVersion},
		"Content-Type":           {syncproto.ContentType},
	}
}

// push sends p as a sync push.
func push(t *testing.T, srv *projecttest.Server, p syncproto.Pack) response {
	t.Helper()
	body, err := syncproto.NewCodec().EncodePack(p)
	require.NoError(t, err)
	return do(t, srv, http.MethodPost, codeURL, syncHeader(), body)
}

// pull sends w as a sync pull, requires a 200 and returns the decoded pack
// with the response.
func pull(t *testing.T, srv *projecttest.Server, w syncproto.Wants) (syncproto.Pack, response) {
	t.Helper()
	body, err := syncproto.NewCodec().EncodeWants(w)
	require.NoError(t, err)
	resp := do(t, srv, http.MethodGet, codeURL, syncHeader(), body)
	require.Equal(t, http.StatusOK, resp.status, string(resp.body))
	p, err := syncproto.NewCodec().DecodePack(bytes.NewReader(resp.body))
	require.NoError(t, err)
	return p, resp
}

// snapshot returns a shared view snapshot under parent holding changes, and
// the View object that carries it.
func snapshot(parent string, changes ...ident.Hash) (views.Snapshot, syncproto.ObjectRecord) {
	s := views.Snapshot{
		Scope:      ident.ScopeShared,
		ParentView: parent,
		OwnSetID:   ident.SetIDOf(changes).String(),
	}
	for _, c := range changes {
		s.OwnChanges = append(s.OwnChanges, c.String())
	}
	if len(changes) > 0 {
		s.MerkleState = ident.Sum([]byte("state of " + parent)).String()
	}
	return s, syncproto.ObjectRecord{Family: syncproto.View, Key: s.Key(), Bytes: s.MarshalCanonical()}
}

// change returns a Change object and its hash.
func change(content string) (ident.Hash, syncproto.ObjectRecord) {
	h := ident.Sum([]byte(content))
	return h, syncproto.ObjectRecord{Family: syncproto.Change, Key: h.String(), Bytes: []byte(content)}
}

func ptr(s string) *string { return &s }

// keys returns the keys of objs, in order.
func keys(objs []syncproto.ObjectRecord) []string {
	var ks []string
	for _, o := range objs {
		ks = append(ks, o.Key)
	}
	return ks
}

func TestServer_Push(t *testing.T) {
	t.Run("a first push of dev stores the pack and creates the ref", func(t *testing.T) {
		srv := projecttest.NewServer()
		h, ch := change("a")
		_, view := snapshot("", h)

		resp := push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{ch, view},
			Refs:    []syncproto.RefRecord{{Name: "dev", NewTarget: view.Key}},
		})

		require.Equal(t, http.StatusOK, resp.status, string(resp.body))
		require.Equal(t, "application/json", resp.header.Get("Content-Type"))
		require.JSONEq(t, `{"stored":2,"refs_moved":1}`, string(resp.body))
		target, ok := srv.Ref("dev")
		require.True(t, ok)
		require.Equal(t, view.Key, target)

		got, _ := pull(t, srv, syncproto.All(nil))
		require.Equal(t, []syncproto.ObjectRecord{ch, view}, got.Objects)
	})

	t.Run("a stale ExpectOld is refused with 409 and stores nothing", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, first := snapshot("")
		srv.PutObject(first)
		srv.SetRef("dev", first.Key)
		h, ch := change("b")
		_, next := snapshot("", h)

		resp := push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{ch, next},
			Refs: []syncproto.RefRecord{
				{Name: "fresh", NewTarget: next.Key},
				{Name: "dev", ExpectOld: ptr("stale"), NewTarget: next.Key},
			},
		})

		require.Equal(t, http.StatusConflict, resp.status)
		require.True(t, strings.HasPrefix(string(resp.body), "ref CAS refused"), string(resp.body))
		target, ok := srv.Ref("dev")
		require.True(t, ok)
		require.Equal(t, first.Key, target)
		_, ok = srv.Ref("fresh")
		require.False(t, ok, "a ref checked before the refused one must not move")
		got, _ := pull(t, srv, syncproto.All(nil))
		require.Equal(t, []string{first.Key}, keys(got.Objects))
	})

	t.Run("a nil ExpectOld is refused when the ref exists", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, first := snapshot("")
		srv.PutObject(first)
		srv.SetRef("dev", first.Key)

		resp := push(t, srv, syncproto.Pack{Refs: []syncproto.RefRecord{{Name: "dev", NewTarget: first.Key}}})

		require.Equal(t, http.StatusConflict, resp.status)
		require.Contains(t, string(resp.body), "ref CAS refused")
	})

	t.Run("a matching ExpectOld moves the ref to a stored target", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, first := snapshot("")
		h, _ := change("c")
		_, next := snapshot("", h)
		srv.PutObject(first)
		srv.PutObject(next)
		srv.SetRef("dev", first.Key)

		resp := push(t, srv, syncproto.Pack{Refs: []syncproto.RefRecord{
			{Name: "dev", ExpectOld: ptr(first.Key), NewTarget: next.Key},
		}})

		require.Equal(t, http.StatusOK, resp.status, string(resp.body))
		require.JSONEq(t, `{"stored":0,"refs_moved":1}`, string(resp.body))
		target, _ := srv.Ref("dev")
		require.Equal(t, next.Key, target)
	})

	t.Run("an absent target is refused with 400 and stores nothing", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, ch := change("d")

		resp := push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{ch},
			Refs:    []syncproto.RefRecord{{Name: "dev", NewTarget: "missing"}},
		})

		require.Equal(t, http.StatusBadRequest, resp.status)
		require.Contains(t, string(resp.body), "missing")
		_, ok := srv.Ref("dev")
		require.False(t, ok)
		got, _ := pull(t, srv, syncproto.All(nil))
		require.Empty(t, got.Objects)
	})

	t.Run("a target that is not a View object is absent", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, ch := change("e")

		resp := push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{ch},
			Refs:    []syncproto.RefRecord{{Name: "dev", NewTarget: ch.Key}},
		})

		require.Equal(t, http.StatusBadRequest, resp.status)
	})

	t.Run("a second move of one ref is checked against the first", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, a := snapshot("")
		h, _ := change("f")
		_, b := snapshot("", h)

		resp := push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{a, b},
			Refs: []syncproto.RefRecord{
				{Name: "dev", NewTarget: a.Key},
				{Name: "dev", ExpectOld: ptr(a.Key), NewTarget: b.Key},
			},
		})

		require.Equal(t, http.StatusOK, resp.status, string(resp.body))
		require.JSONEq(t, `{"stored":2,"refs_moved":2}`, string(resp.body), "every ref record counts, as in the test peer")
		target, _ := srv.Ref("dev")
		require.Equal(t, b.Key, target)
	})

	t.Run("a second creation of one ref is refused and stores nothing", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, a := snapshot("")
		h, _ := change("g")
		_, b := snapshot("", h)

		resp := push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{a, b},
			Refs: []syncproto.RefRecord{
				{Name: "dev", NewTarget: a.Key},
				{Name: "dev", NewTarget: b.Key},
			},
		})

		require.Equal(t, http.StatusConflict, resp.status)
		require.Contains(t, string(resp.body), "ref CAS refused")
		_, ok := srv.Ref("dev")
		require.False(t, ok)
		got, _ := pull(t, srv, syncproto.All(nil))
		require.Empty(t, got.Objects)
	})

	t.Run("an object of an unknown family is refused with 400", func(t *testing.T) {
		srv := projecttest.NewServer()
		// Encoding refuses an unknown family, so splice one into the
		// postcard: an object's family is its first byte.
		_, ch := change("h")
		raw, err := syncproto.Pack{Objects: []syncproto.ObjectRecord{ch}}.AppendPostcard(nil)
		require.NoError(t, err)
		require.Equal(t, byte(1), raw[0], "one object")
		require.Equal(t, byte(syncproto.Change), raw[1])
		raw[1] = 99
		body := zstdframe.Stored{}.Compress(nil, raw)

		resp := do(t, srv, http.MethodPost, codeURL, syncHeader(), body)

		require.Equal(t, http.StatusBadRequest, resp.status)
		require.Contains(t, string(resp.body), "unknown family")
		got, _ := pull(t, srv, syncproto.All(nil))
		require.Empty(t, got.Objects)
	})

	t.Run("concurrent pushes each move the ref once", func(t *testing.T) {
		srv := projecttest.NewServer()
		const n = 8
		statuses := make(chan int, n)
		for i := range n {
			_, view := snapshot(fmt.Sprintf("p%d", i))
			body, err := syncproto.NewCodec().EncodePack(syncproto.Pack{
				Objects: []syncproto.ObjectRecord{view},
				Refs:    []syncproto.RefRecord{{Name: "dev", NewTarget: view.Key}},
			})
			require.NoError(t, err)
			go func() {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, codeURL, bytes.NewReader(body))
				if err != nil {
					statuses <- -1
					return
				}
				req.Header = syncHeader()
				resp, err := remotetest.Client(srv).Do(req)
				if err != nil {
					statuses <- -1
					return
				}
				resp.Body.Close()
				statuses <- resp.StatusCode
			}()
		}
		var ok, conflict int
		for range n {
			switch <-statuses {
			case http.StatusOK:
				ok++
			case http.StatusConflict:
				conflict++
			}
		}

		require.Equal(t, 1, ok, "only the first creation of dev wins")
		require.Equal(t, n-1, conflict)
	})

	t.Run("a body that is not a pack is refused with 400", func(t *testing.T) {
		srv := projecttest.NewServer()

		resp := do(t, srv, http.MethodPost, codeURL, syncHeader(), []byte("not zstd"))

		require.Equal(t, http.StatusBadRequest, resp.status)
	})
}

// chain stores dev, a root view, and feature, a view whose parent is dev,
// with one change in each, and returns their objects.
func chain(srv *projecttest.Server) (devChange, dev, featureChange, feature syncproto.ObjectRecord) {
	h1, devChange := change("dev change")
	_, dev = snapshot("", h1)
	h2, featureChange := change("feature change")
	_, feature = snapshot("dev", h1, h2)
	for _, o := range []syncproto.ObjectRecord{devChange, dev, featureChange, feature} {
		srv.PutObject(o)
	}
	srv.SetRef("dev", dev.Key)
	srv.SetRef("feature", feature.Key)
	return devChange, dev, featureChange, feature
}

func TestServer_Pull(t *testing.T) {
	t.Run("Advertise of feature includes its parent dev", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, dev, _, feature := chain(srv)

		got, resp := pull(t, srv, syncproto.Advertise("feature"))

		require.Equal(t, syncproto.ContentType, resp.header.Get("Content-Type"))
		require.Equal(t, []syncproto.RefRecord{
			{Name: "feature", NewTarget: feature.Key},
			{Name: "dev", NewTarget: dev.Key},
		}, got.Refs)
		require.Equal(t, []syncproto.ObjectRecord{dev, feature}, got.Objects)
	})

	t.Run("empty Refs means every view, by name", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, dev, _, feature := chain(srv)
		_, other := snapshot("")
		srv.PutObject(other)
		srv.SetRef("alpha", other.Key)

		got, _ := pull(t, srv, syncproto.Advertise())

		require.Equal(t, []syncproto.RefRecord{
			{Name: "alpha", NewTarget: other.Key},
			{Name: "dev", NewTarget: dev.Key},
			{Name: "feature", NewTarget: feature.Key},
		}, got.Refs)
	})

	t.Run("haves are dropped", func(t *testing.T) {
		srv := projecttest.NewServer()
		devChange, dev, featureChange, feature := chain(srv)

		got, _ := pull(t, srv, syncproto.Wants{Refs: []string{"feature"}, Haves: []string{devChange.Key, dev.Key}})

		require.Equal(t, []syncproto.ObjectRecord{featureChange, feature}, got.Objects)
		require.Len(t, got.Refs, 2)
	})

	t.Run("RefsOnly keeps View objects only", func(t *testing.T) {
		srv := projecttest.NewServer()
		devChange, dev, featureChange, feature := chain(srv)

		full, _ := pull(t, srv, syncproto.All(nil))
		refsOnly, _ := pull(t, srv, syncproto.Wants{RefsOnly: true})

		require.Equal(t, []syncproto.ObjectRecord{devChange, dev, featureChange, feature}, full.Objects)
		require.Equal(t, []syncproto.ObjectRecord{dev, feature}, refsOnly.Objects)
		require.Equal(t, full.Refs, refsOnly.Refs)
	})

	t.Run("a requested view with no ref is left out", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, dev, _, _ := chain(srv)

		got, _ := pull(t, srv, syncproto.Advertise("nowhere", "dev"))

		require.Equal(t, []syncproto.RefRecord{{Name: "dev", NewTarget: dev.Key}}, got.Refs)
	})

	t.Run("a pushed ref is answered without ExpectOld", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, view := snapshot("")
		resp := push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{view},
			Refs:    []syncproto.RefRecord{{Name: "dev", NewTarget: view.Key}},
		})
		require.Equal(t, http.StatusOK, resp.status)
		_, next := snapshot("dev")
		resp = push(t, srv, syncproto.Pack{
			Objects: []syncproto.ObjectRecord{next},
			Refs:    []syncproto.RefRecord{{Name: "dev", ExpectOld: ptr(view.Key), NewTarget: next.Key}},
		})
		require.Equal(t, http.StatusOK, resp.status)

		got, _ := pull(t, srv, syncproto.Advertise("dev"))

		require.Equal(t, []syncproto.RefRecord{{Name: "dev", NewTarget: next.Key}}, got.Refs)
	})

	t.Run("a stored snapshot that does not parse is a 500", func(t *testing.T) {
		srv := projecttest.NewServer()
		srv.PutObject(syncproto.ObjectRecord{Family: syncproto.View, Key: "broken", Bytes: []byte("{")})
		srv.SetRef("dev", "broken")
		body, err := syncproto.NewCodec().EncodeWants(syncproto.Advertise("dev"))
		require.NoError(t, err)

		resp := do(t, srv, http.MethodGet, codeURL, syncHeader(), body)

		require.Equal(t, http.StatusInternalServerError, resp.status)
	})

	t.Run("the chain ends at a snapshot that is not stored", func(t *testing.T) {
		srv := projecttest.NewServer()
		srv.SetRef("dev", "absent")

		got, _ := pull(t, srv, syncproto.Advertise("dev"))

		require.Equal(t, []syncproto.RefRecord{{Name: "dev", NewTarget: "absent"}}, got.Refs)
	})

	t.Run("a body that is not wants is refused with 400", func(t *testing.T) {
		srv := projecttest.NewServer()

		resp := do(t, srv, http.MethodGet, codeURL, syncHeader(), []byte("not zstd"))

		require.Equal(t, http.StatusBadRequest, resp.status)
	})
}

func TestServer_Identity(t *testing.T) {
	t.Run("an unbound project answers bound false", func(t *testing.T) {
		srv := projecttest.NewServer()

		resp := do(t, srv, http.MethodGet, identityURL, nil, nil)

		require.Equal(t, http.StatusOK, resp.status)
		require.JSONEq(t, `{"bound":false}`, string(resp.body))
	})

	t.Run("the first bind wins and a second gets 409", func(t *testing.T) {
		srv := projecttest.NewServer()
		jsonHeader := http.Header{"Content-Type": {"application/json"}}

		first := do(t, srv, http.MethodPost, identityURL, jsonHeader, []byte(firstDocument))
		again := do(t, srv, http.MethodPost, identityURL, jsonHeader, []byte(` `+firstDocument+"\n"))
		second := do(t, srv, http.MethodPost, identityURL, jsonHeader, []byte(secondDocument))
		read := do(t, srv, http.MethodGet, identityURL, nil, nil)

		require.Equal(t, http.StatusOK, first.status)
		require.Equal(t, http.StatusOK, again.status, "the same document again is not a conflict")
		require.Equal(t, http.StatusConflict, second.status)
		var conflict struct{ Code string }
		require.NoError(t, json.Unmarshal(second.body, &conflict))
		require.Equal(t, "IDENTITY_CONFLICT", conflict.Code)
		require.Equal(t, http.StatusOK, read.status)
		require.JSONEq(t, `{"bound":true,"identity":`+firstDocument+`}`, string(read.body))
	})

	t.Run("Bind seeds the identity served verbatim", func(t *testing.T) {
		srv := projecttest.NewServer()
		srv.Bind(json.RawMessage(firstDocument))

		read := do(t, srv, http.MethodGet, identityURL, nil, nil)
		second := do(t, srv, http.MethodPost, identityURL, nil, []byte(secondDocument))

		require.Contains(t, string(read.body), `"identity":`+firstDocument)
		require.Equal(t, http.StatusConflict, second.status)
	})

	t.Run("a body that is not a JSON object is refused with 400", func(t *testing.T) {
		srv := projecttest.NewServer()

		notJSON := do(t, srv, http.MethodPost, identityURL, nil, []byte("{"))
		notObject := do(t, srv, http.MethodPost, identityURL, nil, []byte("[]"))
		read := do(t, srv, http.MethodGet, identityURL, nil, nil)

		require.Equal(t, http.StatusBadRequest, notJSON.status)
		require.Equal(t, http.StatusBadRequest, notObject.status)
		require.JSONEq(t, `{"bound":false}`, string(read.body))
	})

	t.Run("Bind panics on a document that is not a JSON object", func(t *testing.T) {
		srv := projecttest.NewServer()

		require.Panics(t, func() { srv.Bind(json.RawMessage(`"x"`)) })
	})
}

func TestServer_ListViewRefs(t *testing.T) {
	t.Run("every view is a row written from its snapshot", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, dev, _, feature := chain(srv)
		devSnap, err := views.ParseSnapshot(dev.Bytes)
		require.NoError(t, err)
		featureSnap, err := views.ParseSnapshot(feature.Bytes)
		require.NoError(t, err)

		resp := do(t, srv, http.MethodGet, viewsURL, nil, nil)

		require.Equal(t, http.StatusOK, resp.status, string(resp.body))
		infos, err := views.ParseInventory(string(resp.body))
		require.NoError(t, err)
		require.Equal(t, []views.Info{
			{Name: "dev", Scope: ident.ScopeShared, ChangeCount: 1, State: devSnap.MerkleState, SetID: devSnap.OwnSetID},
			{Name: "feature", Scope: ident.ScopeShared, Parent: "dev", ChangeCount: 2, State: featureSnap.MerkleState, SetID: featureSnap.OwnSetID},
		}, infos)
	})

	t.Run("no views is an empty body", func(t *testing.T) {
		srv := projecttest.NewServer()

		resp := do(t, srv, http.MethodGet, viewsURL, nil, nil)

		require.Equal(t, http.StatusOK, resp.status)
		require.Empty(t, resp.body)
	})

	t.Run("a view whose snapshot is not stored is a 500", func(t *testing.T) {
		srv := projecttest.NewServer()
		srv.SetRef("dev", "missing")

		resp := do(t, srv, http.MethodGet, viewsURL, nil, nil)

		require.Equal(t, http.StatusInternalServerError, resp.status)
	})
}

func TestServer_NotFound(t *testing.T) {
	cases := []struct {
		name, method, url string
		header            http.Header
	}{
		{"a sync request without the protocol header", http.MethodGet, codeURL, nil},
		{"another method on /code", http.MethodPut, codeURL, syncHeader()},
		{"another query on /code", http.MethodGet, codeURL + "?state", syncHeader()},
		{"another identity method", http.MethodDelete, identityURL, nil},
		{"another resource", http.MethodGet, "https://acme.example.com" + projectPath + "/refs/tags", nil},
		{"a POST of refs/views", http.MethodPost, viewsURL, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := projecttest.NewServer()

			resp := do(t, srv, tc.method, tc.url, tc.header, nil)

			require.Equal(t, http.StatusNotFound, resp.status)
			require.Equal(t, "text/plain; charset=utf-8", resp.header.Get("Content-Type"))
			require.NotEmpty(t, resp.body)
		})
	}
}

func TestWithoutIdentityEndpoint(t *testing.T) {
	srv := projecttest.NewServer(projecttest.WithoutIdentityEndpoint())
	srv.Bind(json.RawMessage(firstDocument))

	read := do(t, srv, http.MethodGet, identityURL, nil, nil)
	bind := do(t, srv, http.MethodPost, identityURL, nil, []byte(secondDocument))

	require.Equal(t, http.StatusNotFound, read.status)
	require.Equal(t, http.StatusNotFound, bind.status)
	_, resp := pull(t, srv, syncproto.Advertise())
	require.Equal(t, http.StatusOK, resp.status, "sync is still served")
}

func TestWithMinVersion(t *testing.T) {
	t.Run("every response carries the header", func(t *testing.T) {
		srv := projecttest.NewServer(projecttest.WithMinVersion("0.23.0"))

		_, pulled := pull(t, srv, syncproto.Advertise())
		identity := do(t, srv, http.MethodGet, identityURL, nil, nil)
		missing := do(t, srv, http.MethodGet, codeURL+"?nothing", nil, nil)

		for _, resp := range []response{pulled, identity, missing} {
			require.Equal(t, "0.23.0", resp.header.Get(remote.MinVersionHeader))
		}
	})

	t.Run("by default no response carries it", func(t *testing.T) {
		srv := projecttest.NewServer()

		resp := do(t, srv, http.MethodGet, identityURL, nil, nil)

		require.Empty(t, resp.header.Values(remote.MinVersionHeader))
	})
}

func TestNewServer(t *testing.T) {
	require.Panics(t, func() { projecttest.NewServer(nil) })
}

func TestServer_PutObject(t *testing.T) {
	t.Run("the object is copied", func(t *testing.T) {
		srv := projecttest.NewServer()
		_, ch := change("copied")
		ch.Bytes = []byte("copied")
		srv.PutObject(ch)
		ch.Bytes[0] = 'X'

		got, _ := pull(t, srv, syncproto.All(nil))

		require.Equal(t, []byte("copied"), got.Objects[0].Bytes)
	})

	t.Run("an unknown family panics", func(t *testing.T) {
		srv := projecttest.NewServer()

		require.Panics(t, func() { srv.PutObject(syncproto.ObjectRecord{Family: syncproto.Family(99), Key: "k"}) })
	})
}

func TestServer_Requests(t *testing.T) {
	srv := projecttest.NewServer()
	wants, err := syncproto.NewCodec().EncodeWants(syncproto.Advertise("dev"))
	require.NoError(t, err)

	resp := do(t, srv, http.MethodGet, codeURL, syncHeader(), wants)
	do(t, srv, http.MethodGet, identityURL, nil, nil)

	require.Equal(t, http.StatusOK, resp.status)
	got := srv.Requests()
	require.Len(t, got, 2)
	require.Equal(t, http.MethodGet, got[0].Method)
	require.Equal(t, projectPath+"/code", got[0].RequestURI)
	require.Equal(t, syncproto.ProtocolVersion, got[0].Header.Get(syncproto.ProtocolHeader))
	require.Equal(t, wants, got[0].Body, "a GET body is recorded exactly")
	require.Equal(t, projectPath+"/code?project-identity=1", got[1].RequestURI)
	require.Nil(t, got[1].Body)

	got[0].Body[0] ^= 0xFF
	got[0].Header.Set(syncproto.ProtocolHeader, "changed")
	again := srv.Requests()
	require.Equal(t, wants, again[0].Body, "Requests returns a copy")
	require.Equal(t, syncproto.ProtocolVersion, again[0].Header.Get(syncproto.ProtocolHeader))
}
