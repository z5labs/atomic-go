package projecttest_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/project/projecttest"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/views"
)

// TestServer_WithProjectClient runs a push, a pull, the view inventory and
// both identity requests through project.Client, so the Server is known to
// answer what the client in this module sends and reads.
func TestServer_WithProjectClient(t *testing.T) {
	ctx := t.Context()
	srv := projecttest.NewServer()
	u, _, err := serverurl.ParseProject(codeURL)
	require.NoError(t, err)
	c, err := project.New(u, remotetest.Client(srv), remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)

	h, ch := change("pushed")
	snap, view := snapshot("", h)
	summary, err := c.SyncPush(ctx, syncproto.Pack{
		Objects: []syncproto.ObjectRecord{ch, view},
		Refs:    []syncproto.RefRecord{{Name: "dev", NewTarget: view.Key}},
	})
	require.NoError(t, err)
	require.Equal(t, project.PushSummary{Stored: 2, RefsMoved: 1}, summary)

	_, err = c.SyncPush(ctx, syncproto.Pack{Refs: []syncproto.RefRecord{{Name: "dev", NewTarget: view.Key}}})
	var rerr *remote.Error
	require.ErrorAs(t, err, &rerr)
	require.Equal(t, remote.KindProtocol, rerr.Kind)
	require.Contains(t, rerr.Message, "ref CAS refused")

	pulled, err := c.SyncPull(ctx, syncproto.All([]string{ch.Key}))
	require.NoError(t, err)
	require.Equal(t, []syncproto.ObjectRecord{view}, pulled.Objects)
	require.Equal(t, []syncproto.RefRecord{{Name: "dev", NewTarget: view.Key}}, pulled.Refs)

	infos, err := c.ListViewRefs(ctx)
	require.NoError(t, err)
	require.Equal(t, []views.Info{{
		Name: "dev", Scope: ident.ScopeShared, ChangeCount: 1, State: snap.MerkleState, SetID: snap.OwnSetID,
	}}, infos)

	id, err := c.FetchProjectIdentity(ctx)
	require.NoError(t, err)
	require.Equal(t, project.IdentityUnbound, id.State)

	bound, err := c.BindProjectIdentity(ctx, json.RawMessage(firstDocument))
	require.NoError(t, err)
	require.Equal(t, project.BindBound, bound.Outcome)

	conflict, err := c.BindProjectIdentity(ctx, json.RawMessage(secondDocument))
	require.NoError(t, err)
	require.Equal(t, project.BindConflict, conflict.Outcome)
	require.Equal(t, "IDENTITY_CONFLICT", conflict.ServerCode)
	require.Equal(t, project.IdentityBound, conflict.Remote.State)
	require.Equal(t, firstDocument, string(conflict.Remote.Document))
}

// TestWithoutIdentityEndpoint_WithProjectClient checks that project.Client
// reads the option as an older server.
func TestWithoutIdentityEndpoint_WithProjectClient(t *testing.T) {
	ctx := t.Context()
	srv := projecttest.NewServer(projecttest.WithoutIdentityEndpoint())
	u, _, err := serverurl.ParseProject(codeURL)
	require.NoError(t, err)
	c, err := project.New(u, remotetest.Client(srv), remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)

	id, err := c.FetchProjectIdentity(ctx)
	require.NoError(t, err)
	require.Equal(t, project.IdentityUnsupported, id.State)
	bound, err := c.BindProjectIdentity(ctx, json.RawMessage(firstDocument))
	require.NoError(t, err)
	require.Equal(t, project.BindUnsupported, bound.Outcome)
}
