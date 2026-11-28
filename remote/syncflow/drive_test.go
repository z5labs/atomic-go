package syncflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/project/projecttest"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/syncflow"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/tags"
)

var _ syncflow.Remote = (*project.Client)(nil)

const (
	codePath     = "/workspaces/w/projects/p/code"
	identityPath = codePath + "?project-identity=1"

	firstIdentity  = `{"@type": "atomic:ProjectIdentity", "atomic:projectId": "AAAA"}`
	secondIdentity = `{"@type": "atomic:ProjectIdentity", "atomic:projectId": "BBBB"}`
)

// request is a request as the tests compare it: its method and target.
type request struct {
	Method, URI string
}

var (
	getCode      = request{"GET", codePath}
	postCode     = request{"POST", codePath}
	getIdentity  = request{"GET", identityPath}
	postIdentity = request{"POST", identityPath}
)

func requests(srv *projecttest.Server) []request {
	var out []request
	for _, r := range srv.Requests() {
		out = append(out, request{r.Method, r.RequestURI})
	}
	return out
}

func newClient(t *testing.T, srv *projecttest.Server) *project.Client {
	t.Helper()
	u, _, err := serverurl.ParseProject("https://acme.example.com" + codePath)
	require.NoError(t, err)
	c, err := project.New(u, remotetest.Client(srv), remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)
	return c
}

// sourceOf returns a PackSource holding a change file for each of names.
func sourceOf(names ...string) *mapSource {
	src := &mapSource{changes: make(map[ident.Hash][]byte)}
	for _, n := range names {
		src.changes[hash(n)] = []byte("file " + n)
	}
	return src
}

// devChain is a one-view chain, dev, logging c1 and c2.
func devChain() []syncflow.LocalView {
	return []syncflow.LocalView{{Manifest: manifest("dev", ident.ScopeShared, "", "c1", "c2")}}
}

func TestPushFirstWithIdentity(t *testing.T) {
	srv := projecttest.NewServer()
	res, err := syncflow.Push(t.Context(), newClient(t, srv), devChain(), sourceOf("c1", "c2"),
		syncflow.WithIdentity(json.RawMessage(firstIdentity)))
	require.NoError(t, err)

	require.Equal(t, []request{getCode, postIdentity, postCode, getCode}, requests(srv))
	require.Nil(t, res.Conflict)
	require.Len(t, res.Plan.Views, 1)
	require.True(t, res.Plan.Views[0].Declare)
	require.Equal(t, 2, res.PushedChanges)
	require.Equal(t, project.PushSummary{Stored: 3, RefsMoved: 1}, res.Summary)
	_, ok := srv.Ref("dev")
	require.True(t, ok)

	// The advertisement names the chain, refs only.
	codec := syncproto.NewCodec()
	w, err := codec.DecodeWants(bytes.NewReader(srv.Requests()[0].Body))
	require.NoError(t, err)
	require.Equal(t, syncproto.Advertise("dev"), w)
}

func TestPushFirstWithoutIdentity(t *testing.T) {
	srv := projecttest.NewServer()
	res, err := syncflow.Push(t.Context(), newClient(t, srv), devChain(), sourceOf("c1", "c2"))
	require.NoError(t, err)
	require.Equal(t, []request{getCode, postCode, getCode}, requests(srv))
	require.Equal(t, 2, res.PushedChanges)
}

func TestPushRepeatWithNothingNew(t *testing.T) {
	srv := projecttest.NewServer()
	c := newClient(t, srv)
	_, err := syncflow.Push(t.Context(), c, devChain(), sourceOf("c1", "c2"))
	require.NoError(t, err)
	first := len(srv.Requests())

	res, err := syncflow.Push(t.Context(), c, devChain(), sourceOf("c1", "c2"),
		syncflow.WithIdentity(json.RawMessage(firstIdentity)))
	require.NoError(t, err)
	require.Equal(t, []request{getCode}, requests(srv)[first:])
	require.Len(t, res.Plan.Views, 1)
	require.False(t, res.Plan.Views[0].Declare)
	require.Zero(t, res.PushedChanges)
	require.Zero(t, res.Summary)
}

func TestPushRepeatWithLeafTagSendsAgain(t *testing.T) {
	srv := projecttest.NewServer()
	c := newClient(t, srv)
	src := sourceOf("c1", "c2")
	src.tags = map[string][]tags.Record{"dev": {{
		Name:       "v1",
		View:       "dev",
		Sequence:   2,
		ChangeHash: hash("c2"),
		Timestamp:  time.Unix(1700000000, 0).UTC(),
		Kind:       tags.Release,
	}}}
	_, err := syncflow.Push(t.Context(), c, devChain(), src)
	require.NoError(t, err)
	first := len(srv.Requests())

	res, err := syncflow.Push(t.Context(), c, devChain(), src)
	require.NoError(t, err)
	require.Equal(t, []request{getCode, postCode, getCode}, requests(srv)[first:])
	require.False(t, res.Plan.Views[0].Declare)
	require.Zero(t, res.PushedChanges)
	require.Equal(t, project.PushSummary{Stored: 1}, res.Summary)
}

func TestPushDryRun(t *testing.T) {
	srv := projecttest.NewServer()
	res, err := syncflow.Push(t.Context(), newClient(t, srv), devChain(), sourceOf("c1", "c2"),
		syncflow.DryRun(), syncflow.WithIdentity(json.RawMessage(firstIdentity)))
	require.NoError(t, err)
	require.Equal(t, []request{getCode}, requests(srv))
	require.Len(t, res.Plan.Views, 1)
	require.Equal(t, changes("c1", "c2"), res.Plan.Views[0].Store)
	require.Zero(t, res.PushedChanges)
	_, ok := srv.Ref("dev")
	require.False(t, ok)
}

func TestPushWithPlanOptions(t *testing.T) {
	srv := projecttest.NewServer()
	c := newClient(t, srv)
	_, err := syncflow.Push(t.Context(), c, devChain(), sourceOf("c1", "c2"))
	require.NoError(t, err)

	chain := []syncflow.LocalView{{Manifest: manifest("dev", ident.ScopeShared, "", "c1", "c2", "c3")}}
	res, err := syncflow.Push(t.Context(), c, chain, sourceOf("c1", "c2", "c3"),
		syncflow.WithPlanOptions(syncflow.StoreAll()), syncflow.DryRun())
	require.NoError(t, err)
	require.Equal(t, changes("c1", "c2", "c3"), res.Plan.Views[0].Store)
}

func TestPushConflictStopsAfterAdvertise(t *testing.T) {
	srv := projecttest.NewServer()
	c := newClient(t, srv)
	_, err := syncflow.Push(t.Context(), c, devChain(), sourceOf("c1", "c2"))
	require.NoError(t, err)
	first := len(srv.Requests())

	draft := []syncflow.LocalView{{Manifest: manifest("dev", ident.ScopeDraft, "", "c1", "c2", "c3")}}
	res, err := syncflow.Push(t.Context(), c, draft, sourceOf("c1", "c2", "c3"),
		syncflow.WithIdentity(json.RawMessage(firstIdentity)))
	require.NoError(t, err)
	require.Equal(t, []request{getCode}, requests(srv)[first:])
	require.Equal(t, &syncflow.Conflict{View: "dev", Field: syncflow.ConflictScope, Local: "draft", Remote: "shared"}, res.Conflict)
	require.Empty(t, res.Plan.Views)
}

func TestPushIdentityConflict(t *testing.T) {
	srv := projecttest.NewServer()
	srv.Bind(json.RawMessage(secondIdentity))
	_, err := syncflow.Push(t.Context(), newClient(t, srv), devChain(), sourceOf("c1", "c2"),
		syncflow.WithIdentity(json.RawMessage(firstIdentity)))

	var se *syncflow.StageError
	require.ErrorAs(t, err, &se)
	require.Equal(t, syncflow.StageBind, se.Stage)
	require.False(t, se.Landed())
	var ic *syncflow.IdentityConflict
	require.ErrorAs(t, err, &ic)
	require.Equal(t, "IDENTITY_CONFLICT", ic.Code)
	require.NotEmpty(t, ic.Message)
	require.JSONEq(t, secondIdentity, string(ic.Document))

	require.Equal(t, []request{getCode, postIdentity, getIdentity}, requests(srv))
	require.NotContains(t, requests(srv), postCode)
}

func TestPushWithoutIdentityEndpoint(t *testing.T) {
	srv := projecttest.NewServer(projecttest.WithoutIdentityEndpoint())
	res, err := syncflow.Push(t.Context(), newClient(t, srv), devChain(), sourceOf("c1", "c2"),
		syncflow.WithIdentity(json.RawMessage(firstIdentity)))
	require.NoError(t, err)
	require.Equal(t, []request{getCode, postIdentity, postCode, getCode}, requests(srv))
	require.Equal(t, 2, res.PushedChanges)
}

// movingRemote moves a ref on srv just before it forwards a SyncPush, as a
// concurrent writer would.
type movingRemote struct {
	remote syncflow.Remote
	srv    *projecttest.Server
	view   string
	target string
}

func (m *movingRemote) SyncPull(ctx context.Context, w syncproto.Wants) (syncproto.Pack, error) {
	return m.remote.SyncPull(ctx, w)
}

func (m *movingRemote) SyncPush(ctx context.Context, p syncproto.Pack) (project.PushSummary, error) {
	m.srv.SetRef(m.view, m.target)
	return m.remote.SyncPush(ctx, p)
}

func (m *movingRemote) BindProjectIdentity(ctx context.Context, doc json.RawMessage) (project.BindResult, error) {
	return m.remote.BindProjectIdentity(ctx, doc)
}

func TestPushRefMovedBeforeSend(t *testing.T) {
	srv := projecttest.NewServer()
	r := &movingRemote{remote: newClient(t, srv), srv: srv, view: "dev", target: "elsewhere"}
	res, err := syncflow.Push(t.Context(), r, devChain(), sourceOf("c1", "c2"))

	var se *syncflow.StageError
	require.ErrorAs(t, err, &se)
	require.Equal(t, syncflow.StageSend, se.Stage)
	require.False(t, se.Landed())
	require.ErrorIs(t, err, remote.ErrConflict)
	require.Equal(t, []request{getCode, postCode}, requests(srv))
	require.Len(t, res.Plan.Views, 1)
	require.Zero(t, res.PushedChanges)
}

func TestPushRenamedLeaf(t *testing.T) {
	srv := projecttest.NewServer()
	c := newClient(t, srv)
	chain := []syncflow.LocalView{
		{Manifest: manifest("dev", ident.ScopeShared, "", "c1")},
		{Manifest: manifest("feature", ident.ScopeDraft, "dev", "c1", "c2"), RemoteName: "feat"},
	}
	res, err := syncflow.Push(t.Context(), c, chain, sourceOf("c1", "c2"))
	require.NoError(t, err)
	require.Equal(t, []request{getCode, postCode, getCode}, requests(srv))
	require.Equal(t, 2, res.PushedChanges)

	codec := syncproto.NewCodec()
	for _, i := range []int{0, 2} {
		w, err := codec.DecodeWants(bytes.NewReader(srv.Requests()[i].Body))
		require.NoError(t, err)
		require.Equal(t, syncproto.Advertise("dev", "feat"), w)
	}
	_, ok := srv.Ref("feat")
	require.True(t, ok)
	_, ok = srv.Ref("feature")
	require.False(t, ok)
}

// fakeRemote answers each call from its fields and counts the calls.
type fakeRemote struct {
	pulls   []syncproto.Pack
	pullErr []error
	pushErr error
	bind    project.BindResult
	bindErr error

	pulled, pushed, bound int
}

func (f *fakeRemote) SyncPull(context.Context, syncproto.Wants) (syncproto.Pack, error) {
	i := f.pulled
	f.pulled++
	var err error
	if i < len(f.pullErr) {
		err = f.pullErr[i]
	}
	if err != nil {
		return syncproto.Pack{}, err
	}
	if i < len(f.pulls) {
		return f.pulls[i], nil
	}
	return syncproto.Pack{}, nil
}

func (f *fakeRemote) SyncPush(context.Context, syncproto.Pack) (project.PushSummary, error) {
	f.pushed++
	return project.PushSummary{Stored: 3, RefsMoved: 1}, f.pushErr
}

func (f *fakeRemote) BindProjectIdentity(context.Context, json.RawMessage) (project.BindResult, error) {
	f.bound++
	return f.bind, f.bindErr
}

func TestPushStageErrors(t *testing.T) {
	boom := errors.New("boom")

	t.Run("advertise", func(t *testing.T) {
		f := &fakeRemote{pullErr: []error{boom}}
		res, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1", "c2"))
		var se *syncflow.StageError
		require.ErrorAs(t, err, &se)
		require.Equal(t, syncflow.StageAdvertise, se.Stage)
		require.ErrorIs(t, err, boom)
		require.Empty(t, res.Plan.Views)
		require.Equal(t, 0, f.pushed)
	})

	t.Run("bind fails", func(t *testing.T) {
		f := &fakeRemote{bindErr: boom}
		_, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1", "c2"),
			syncflow.WithIdentity(json.RawMessage(firstIdentity)))
		var se *syncflow.StageError
		require.ErrorAs(t, err, &se)
		require.Equal(t, syncflow.StageBind, se.Stage)
		require.ErrorIs(t, err, boom)
		require.Equal(t, 0, f.pushed)
	})

	t.Run("bind conflict without a readable binding", func(t *testing.T) {
		f := &fakeRemote{bind: project.BindResult{Outcome: project.BindConflict, ServerCode: "X"}}
		_, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1", "c2"),
			syncflow.WithIdentity(json.RawMessage(firstIdentity)))
		var ic *syncflow.IdentityConflict
		require.ErrorAs(t, err, &ic)
		require.Equal(t, &syncflow.IdentityConflict{Code: "X"}, ic)
		require.Equal(t, 0, f.pushed)
	})

	t.Run("bind with an unknown outcome", func(t *testing.T) {
		f := &fakeRemote{}
		_, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1", "c2"),
			syncflow.WithIdentity(json.RawMessage(firstIdentity)))
		var se *syncflow.StageError
		require.ErrorAs(t, err, &se)
		require.Equal(t, syncflow.StageBind, se.Stage)
		require.Equal(t, 0, f.pushed)
	})

	t.Run("send", func(t *testing.T) {
		f := &fakeRemote{pushErr: boom}
		_, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1", "c2"))
		var se *syncflow.StageError
		require.ErrorAs(t, err, &se)
		require.Equal(t, syncflow.StageSend, se.Stage)
		require.ErrorIs(t, err, boom)
		require.Equal(t, 1, f.pulled)
	})

	t.Run("verify read fails", func(t *testing.T) {
		f := &fakeRemote{pullErr: []error{nil, boom}}
		res, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1", "c2"))
		var se *syncflow.StageError
		require.ErrorAs(t, err, &se)
		require.Equal(t, syncflow.StageVerify, se.Stage)
		require.True(t, se.Landed())
		require.ErrorIs(t, err, boom)
		require.Contains(t, err.Error(), "the pack landed")
		require.Equal(t, 2, res.PushedChanges)
		require.Equal(t, project.PushSummary{Stored: 3, RefsMoved: 1}, res.Summary)
	})

	t.Run("verify finds the view missing", func(t *testing.T) {
		f := &fakeRemote{}
		_, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1", "c2"))
		var se *syncflow.StageError
		require.ErrorAs(t, err, &se)
		require.Equal(t, syncflow.StageVerify, se.Stage)
		var ve *syncflow.VerifyError
		require.ErrorAs(t, err, &ve)
		require.Equal(t, syncflow.StageVerifyUnion, ve.Stage)
	})

	t.Run("build", func(t *testing.T) {
		f := &fakeRemote{}
		_, err := syncflow.Push(t.Context(), f, devChain(), sourceOf("c1"))
		require.Error(t, err)
		var se *syncflow.StageError
		require.False(t, errors.As(err, &se))
		require.Equal(t, 0, f.pushed)
	})
}

func TestPushEmptyChain(t *testing.T) {
	srv := projecttest.NewServer()
	res, err := syncflow.Push(t.Context(), newClient(t, srv), nil, sourceOf())
	require.NoError(t, err)
	require.Equal(t, []request{getCode}, requests(srv))
	require.Empty(t, res.Plan.Views)
}
