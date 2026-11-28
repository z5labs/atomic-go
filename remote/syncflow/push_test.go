package syncflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/syncflow"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/tags"
	"github.com/z5labs/atomic-go/remote/views"
)

func manifest(name string, scope ident.Scope, parent string, log ...string) views.Manifest {
	m := views.Manifest{Name: name, Scope: scope, Parent: parent, Changes: changes(log...)}
	m.State = m.Fold()
	return m
}

func advertised(m views.Manifest) syncflow.RemoteView {
	s := views.FromManifest(m, "")
	return syncflow.RemoteView{Name: m.Name, Target: s.Key(), Manifest: &m}
}

func ptr(s string) *string {
	return &s
}

func TestPlanPushRows(t *testing.T) {
	local := manifest("dev", ident.ScopeShared, "", "c1", "c2")
	view := syncflow.LocalView{Manifest: local}

	testCases := []struct {
		name string
		adv  syncflow.Advertisement
		want syncflow.ViewPlan
	}{
		{
			name: "not advertised",
			adv:  syncflow.Advertisement{},
			want: syncflow.ViewPlan{View: view, Store: changes("c1", "c2"), Declare: true},
		},
		{
			name: "advertised without its manifest",
			adv:  syncflow.Advertisement{"dev": {Name: "dev", Target: "t0"}},
			want: syncflow.ViewPlan{View: view, Store: changes("c1", "c2"), Declare: true, ExpectOld: ptr("t0")},
		},
		{
			name: "the remote lacks a change",
			adv:  syncflow.Advertisement{"dev": advertised(manifest("dev", ident.ScopeShared, "", "c1"))},
			want: syncflow.ViewPlan{View: view, Store: changes("c2"), Declare: true, ExpectOld: ptr(advertised(manifest("dev", ident.ScopeShared, "", "c1")).Target)},
		},
		{
			name: "the remote lists every change",
			adv:  syncflow.Advertisement{"dev": advertised(manifest("dev", ident.ScopeShared, "", "c2", "c1", "c0"))},
			want: syncflow.ViewPlan{View: view, ExpectOld: ptr(advertised(manifest("dev", ident.ScopeShared, "", "c2", "c1", "c0")).Target)},
		},
		{
			name: "diverged",
			adv:  syncflow.Advertisement{"dev": advertised(manifest("dev", ident.ScopeShared, "", "c1", "r1"))},
			want: syncflow.ViewPlan{View: view, Store: changes("c2"), Declare: true, Diverged: true, ExpectOld: ptr(advertised(manifest("dev", ident.ScopeShared, "", "c1", "r1")).Target)},
		},
		{
			name: "scope in another case",
			adv:  syncflow.Advertisement{"dev": advertised(manifest("dev", "SHARED", "", "c1", "c2"))},
			want: syncflow.ViewPlan{View: view, ExpectOld: ptr(advertised(manifest("dev", "SHARED", "", "c1", "c2")).Target)},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := syncflow.PlanPush([]syncflow.LocalView{view}, tc.adv)
			require.NoError(t, err)
			require.Equal(t, syncflow.Plan{Views: []syncflow.ViewPlan{tc.want}}, plan)
		})
	}
}

func TestPlanPushConflict(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1")}
	feature := syncflow.LocalView{Manifest: manifest("feature", ident.ScopeDraft, "dev", "c1", "c2")}

	testCases := []struct {
		name string
		adv  syncflow.Advertisement
		want syncflow.Conflict
	}{
		{
			name: "scope",
			adv:  syncflow.Advertisement{"dev": advertised(manifest("dev", ident.ScopeDraft, "", "c1"))},
			want: syncflow.Conflict{View: "dev", Field: syncflow.ConflictScope, Local: "shared", Remote: "draft"},
		},
		{
			name: "parent",
			adv:  syncflow.Advertisement{"feature": advertised(manifest("feature", ident.ScopeDraft, "main", "c1"))},
			want: syncflow.Conflict{View: "feature", Field: syncflow.ConflictParent, Local: "dev", Remote: "main"},
		},
		{
			name: "the first conflict stops planning",
			adv: syncflow.Advertisement{
				"dev":     advertised(manifest("dev", ident.ScopeShared, "base", "c1")),
				"feature": advertised(manifest("feature", ident.ScopeShared, "dev", "c1")),
			},
			want: syncflow.Conflict{View: "dev", Field: syncflow.ConflictParent, Local: "", Remote: "base"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := syncflow.PlanPush([]syncflow.LocalView{dev, feature}, tc.adv)
			var c *syncflow.Conflict
			require.ErrorAs(t, err, &c)
			require.Equal(t, tc.want, *c)
			require.Empty(t, plan.Views)
		})
	}
}

func TestPlanPushRenamesByName(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1")}
	feature := syncflow.LocalView{Manifest: manifest("feature", ident.ScopeDraft, "dev", "c1", "c2"), RemoteName: "feat"}
	require.Equal(t, "dev", dev.Name())
	require.Equal(t, "feat", feature.Name())

	// The leaf's advertisement is found under its remote name, not its
	// local one.
	adv := syncflow.Advertisement{
		"feature": advertised(manifest("feature", ident.ScopeShared, "", "c1", "c2")),
		"feat":    advertised(manifest("feat", ident.ScopeDraft, "dev", "c1", "c2")),
	}
	plan, err := syncflow.PlanPush([]syncflow.LocalView{dev, feature}, adv)
	require.NoError(t, err)
	require.Len(t, plan.Views, 2)
	require.False(t, plan.Views[1].Declare)
	require.Equal(t, adv["feat"].Target, *plan.Views[1].ExpectOld)
}

func TestPlanPushStoresEachChangeOnce(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1", "c2")}
	feature := syncflow.LocalView{Manifest: manifest("feature", ident.ScopeDraft, "dev", "c1", "c2", "c3")}
	chain := []syncflow.LocalView{dev, feature}

	t.Run("neither view advertised", func(t *testing.T) {
		plan, err := syncflow.PlanPush(chain, syncflow.Advertisement{})
		require.NoError(t, err)
		require.Equal(t, syncflow.Plan{Views: []syncflow.ViewPlan{
			{View: dev, Store: changes("c1", "c2"), Declare: true},
			{View: feature, Store: changes("c3"), Declare: true},
		}}, plan)
	})

	t.Run("an earlier view's advertisement is subtracted", func(t *testing.T) {
		remoteDev := advertised(manifest("dev", ident.ScopeShared, "", "c1"))
		plan, err := syncflow.PlanPush(chain, syncflow.Advertisement{"dev": remoteDev})
		require.NoError(t, err)
		require.Equal(t, syncflow.Plan{Views: []syncflow.ViewPlan{
			{View: dev, Store: changes("c2"), Declare: true, ExpectOld: ptr(remoteDev.Target)},
			{View: feature, Store: changes("c3"), Declare: true},
		}}, plan)
	})

	t.Run("a no-op parent", func(t *testing.T) {
		remoteDev := advertised(manifest("dev", ident.ScopeShared, "", "c1", "c2"))
		plan, err := syncflow.PlanPush(chain, syncflow.Advertisement{"dev": remoteDev})
		require.NoError(t, err)
		require.Equal(t, syncflow.Plan{Views: []syncflow.ViewPlan{
			{View: dev, ExpectOld: ptr(remoteDev.Target)},
			{View: feature, Store: changes("c3"), Declare: true},
		}}, plan)
	})
}

func TestPlanPushStoreAll(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1", "c2")}
	feature := syncflow.LocalView{Manifest: manifest("feature", ident.ScopeDraft, "dev", "c1", "c2", "c3")}
	remoteDev := advertised(manifest("dev", ident.ScopeShared, "", "c1"))
	remoteFeature := advertised(manifest("feature", ident.ScopeDraft, "dev", "c1", "c2"))
	adv := syncflow.Advertisement{"dev": remoteDev, "feature": remoteFeature}

	plan, err := syncflow.PlanPush([]syncflow.LocalView{dev, feature}, adv, syncflow.StoreAll())
	require.NoError(t, err)
	require.Equal(t, syncflow.Plan{Views: []syncflow.ViewPlan{
		{View: dev, Store: changes("c1", "c2"), Declare: true, ExpectOld: ptr(remoteDev.Target)},
		{View: feature, Store: changes("c3"), Declare: true, ExpectOld: ptr(remoteFeature.Target)},
	}}, plan)

	// Without StoreAll the advertised changes are left out.
	plan, err = syncflow.PlanPush([]syncflow.LocalView{dev, feature}, adv)
	require.NoError(t, err)
	require.Equal(t, changes("c2"), plan.Views[0].Store)
	require.Equal(t, changes("c3"), plan.Views[1].Store)

	// A view with nothing to declare stores nothing, even with StoreAll.
	plan, err = syncflow.PlanPush([]syncflow.LocalView{dev}, syncflow.Advertisement{"dev": advertised(dev.Manifest)}, syncflow.StoreAll())
	require.NoError(t, err)
	require.Equal(t, syncflow.Plan{Views: []syncflow.ViewPlan{{View: dev, ExpectOld: ptr(advertised(dev.Manifest).Target)}}}, plan)
}

func TestPlanPushEmptyChain(t *testing.T) {
	plan, err := syncflow.PlanPush(nil, syncflow.Advertisement{})
	require.NoError(t, err)
	require.Empty(t, plan.Views)
}

// mapSource is a PackSource backed by maps.
type mapSource struct {
	changes  map[ident.Hash][]byte
	sidecars map[ident.Hash][]syncflow.Sidecar
	tags     map[string][]tags.Record

	sidecarErr, tagErr error

	tagViews []string
}

func (s *mapSource) Change(_ context.Context, h ident.Hash) ([]byte, error) {
	b, ok := s.changes[h]
	if !ok {
		return nil, errors.New("no such change")
	}
	return b, nil
}

func (s *mapSource) SidecarsCovering(_ context.Context, h ident.Hash) ([]syncflow.Sidecar, error) {
	if s.sidecarErr != nil {
		return nil, s.sidecarErr
	}
	return s.sidecars[h], nil
}

func (s *mapSource) Tags(_ context.Context, view string) ([]tags.Record, error) {
	s.tagViews = append(s.tagViews, view)
	if s.tagErr != nil {
		return nil, s.tagErr
	}
	return s.tags[view], nil
}

func hash(name string) ident.Hash {
	return ident.Sum([]byte(name))
}

func sidecar(f syncproto.Family, body string, covers ...string) syncflow.Sidecar {
	b := []byte(body)
	return syncflow.Sidecar{
		Object: syncproto.ObjectRecord{Family: f, Key: syncflow.SidecarKey(b), Bytes: b},
		Covers: changes(covers...),
	}
}

func changeObject(name string) syncproto.ObjectRecord {
	return syncproto.ObjectRecord{Family: syncproto.Change, Key: syncflow.ChangeKey(hash(name)), Bytes: []byte("file " + name)}
}

func tagObject(t *testing.T, r tags.Record) syncproto.ObjectRecord {
	t.Helper()
	b, err := r.MarshalPostcard()
	require.NoError(t, err)
	return syncproto.ObjectRecord{Family: syncproto.Tag, Key: r.Key(), Bytes: b}
}

func TestBuildPack(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1", "c2")}
	feature := syncflow.LocalView{Manifest: manifest("feature", ident.ScopeDraft, "dev", "c1", "c2", "c3"), RemoteName: "feat"}
	remoteDev := advertised(manifest("dev", ident.ScopeShared, "", "c1"))
	plan, err := syncflow.PlanPush([]syncflow.LocalView{dev, feature}, syncflow.Advertisement{"dev": remoteDev})
	require.NoError(t, err)

	attestC2 := sidecar(syncproto.Attest, "attest c2", "c2")
	attestC1C3 := sidecar(syncproto.Attest, "attest c1 c3", "c1", "c3")
	provC3 := sidecar(syncproto.Provenance, "provenance c3", "c3")
	provC2C3 := sidecar(syncproto.Provenance, "provenance c2 c3", "c2", "c3")
	outside := sidecar(syncproto.Attest, "attest c3 c9", "c3", "c9")
	unstored := sidecar(syncproto.Provenance, "provenance c1", "c1")
	// Returned for a stored change, but covering only c1, which is in the
	// chain and not stored.
	coversUnstored := sidecar(syncproto.Attest, "attest c1", "c1")
	tag := tags.Record{
		Name:       "v1",
		View:       "feature",
		Sequence:   3,
		ChangeHash: hash("c3"),
		Timestamp:  time.Unix(1700000000, 0).UTC(),
		Kind:       tags.Release,
	}
	tag2 := tag
	tag2.Name = "v0"
	src := &mapSource{
		changes: map[ident.Hash][]byte{
			hash("c1"): []byte("file c1"),
			hash("c2"): []byte("file c2"),
			hash("c3"): []byte("file c3"),
		},
		sidecars: map[ident.Hash][]syncflow.Sidecar{
			hash("c1"): {unstored},
			hash("c2"): {provC2C3, coversUnstored, attestC2},
			// A provenance graph before an attestation, a duplicate and one
			// covering a change outside the chain.
			hash("c3"): {provC3, attestC1C3, provC2C3, outside, attestC2},
		},
		tags: map[string][]tags.Record{"feature": {tag, tag2}, "feat": {tag}},
	}

	pack, err := syncflow.BuildPack(context.Background(), plan, src)
	require.NoError(t, err)

	devSnap := views.FromManifest(dev.Manifest, remoteDev.Target)
	featureSnap := views.FromManifest(feature.Manifest, "")
	require.Equal(t, syncproto.Pack{
		Objects: []syncproto.ObjectRecord{
			changeObject("c2"),
			viewObject(devSnap),
			changeObject("c3"),
			viewObject(featureSnap),
			attestC2.Object,
			attestC1C3.Object,
			provC2C3.Object,
			provC3.Object,
			tagObject(t, tag),
			tagObject(t, tag2),
		},
		Refs: []syncproto.RefRecord{
			{Name: "dev", ExpectOld: ptr(remoteDev.Target), NewTarget: devSnap.Key()},
			{Name: "feat", NewTarget: featureSnap.Key()},
		},
	}, pack)

	// The leaf's tags are read under its local name.
	require.Equal(t, []string{"feature"}, src.tagViews)
	require.Equal(t, []string{remoteDev.Target}, devSnap.Prev)
	require.Empty(t, featureSnap.Prev)
	for _, obj := range pack.Objects {
		if obj.Family != syncproto.Change {
			require.NoError(t, syncflow.VerifyKey(obj))
		}
	}
}

func TestBuildPackEmpty(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1")}
	plan, err := syncflow.PlanPush([]syncflow.LocalView{dev}, syncflow.Advertisement{"dev": advertised(dev.Manifest)})
	require.NoError(t, err)

	src := &mapSource{}
	pack, err := syncflow.BuildPack(context.Background(), plan, src)
	require.NoError(t, err)
	require.Empty(t, pack.Objects)
	require.Empty(t, pack.Refs)
	require.Equal(t, []string{"dev"}, src.tagViews)

	// A no-op leaf's tags still go into the pack.
	tag := tags.Record{Name: "v1", View: "dev", ChangeHash: hash("c1"), Timestamp: time.Unix(1700000000, 0).UTC()}
	src = &mapSource{tags: map[string][]tags.Record{"dev": {tag}}}
	pack, err = syncflow.BuildPack(context.Background(), plan, src)
	require.NoError(t, err)
	require.Equal(t, syncproto.Pack{Objects: []syncproto.ObjectRecord{tagObject(t, tag)}}, pack)

	pack, err = syncflow.BuildPack(context.Background(), syncflow.Plan{}, &mapSource{})
	require.NoError(t, err)
	require.Empty(t, pack.Objects)
	require.Empty(t, pack.Refs)
}

func TestBuildPackErrors(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1")}
	plan, err := syncflow.PlanPush([]syncflow.LocalView{dev}, syncflow.Advertisement{})
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("a missing change", func(t *testing.T) {
		_, err := syncflow.BuildPack(ctx, plan, &mapSource{})
		require.ErrorContains(t, err, "no such change")
	})
	t.Run("a sidecar of another family", func(t *testing.T) {
		src := &mapSource{
			changes:  map[ident.Hash][]byte{hash("c1"): []byte("file c1")},
			sidecars: map[ident.Hash][]syncflow.Sidecar{hash("c1"): {sidecar(syncproto.Tag, "tag", "c1")}},
		}
		_, err := syncflow.BuildPack(ctx, plan, src)
		require.ErrorContains(t, err, "not an attestation or provenance graph")
	})
	t.Run("a tag that does not encode", func(t *testing.T) {
		src := &mapSource{
			changes: map[ident.Hash][]byte{hash("c1"): []byte("file c1")},
			tags:    map[string][]tags.Record{"dev": {{Name: "v1", View: "dev", Kind: tags.Kind(99)}}},
		}
		_, err := syncflow.BuildPack(ctx, plan, src)
		require.ErrorIs(t, err, tags.ErrInvalidRecord)
	})
	t.Run("a failing SidecarsCovering", func(t *testing.T) {
		errSidecars := errors.New("sidecars unreadable")
		src := &mapSource{
			changes:    map[ident.Hash][]byte{hash("c1"): []byte("file c1")},
			sidecarErr: errSidecars,
		}
		pack, err := syncflow.BuildPack(ctx, plan, src)
		require.ErrorIs(t, err, errSidecars)
		require.Empty(t, pack.Objects)
		require.Empty(t, src.tagViews)
	})
	t.Run("a failing Tags", func(t *testing.T) {
		errTags := errors.New("tags unreadable")
		src := &mapSource{
			changes: map[ident.Hash][]byte{hash("c1"): []byte("file c1")},
			tagErr:  errTags,
		}
		pack, err := syncflow.BuildPack(ctx, plan, src)
		require.ErrorIs(t, err, errTags)
		require.Empty(t, pack.Objects)
	})
}

func TestVerifyPush(t *testing.T) {
	dev := syncflow.LocalView{Manifest: manifest("dev", ident.ScopeShared, "", "c1", "c2")}
	feature := syncflow.LocalView{Manifest: manifest("feature", ident.ScopeDraft, "dev", "c1", "c2", "c3"), RemoteName: "feat"}
	plan, err := syncflow.PlanPush([]syncflow.LocalView{dev, feature}, syncflow.Advertisement{})
	require.NoError(t, err)

	t.Run("extra remote changes pass", func(t *testing.T) {
		after := syncflow.Advertisement{
			"dev":  advertised(manifest("dev", ident.ScopeShared, "", "r1", "c2", "c1")),
			"feat": advertised(manifest("feat", ident.ScopeDraft, "dev", "c1", "c2", "r1", "c3")),
		}
		require.NoError(t, syncflow.VerifyPush(plan, after))
	})

	t.Run("verify-union", func(t *testing.T) {
		after := syncflow.Advertisement{
			"dev":     advertised(manifest("dev", ident.ScopeShared, "", "c1", "c2")),
			"feature": advertised(manifest("feature", ident.ScopeDraft, "dev", "c1", "c2", "c3")),
		}
		err := syncflow.VerifyPush(plan, after)
		var ve *syncflow.VerifyError
		require.ErrorAs(t, err, &ve)
		require.Equal(t, syncflow.VerifyError{Stage: syncflow.StageVerifyUnion, View: "feat"}, *ve)
	})

	t.Run("verify-patch", func(t *testing.T) {
		after := syncflow.Advertisement{
			"dev":  advertised(manifest("dev", ident.ScopeShared, "", "c2")),
			"feat": advertised(manifest("feat", ident.ScopeDraft, "dev", "c3")),
		}
		err := syncflow.VerifyPush(plan, after)
		var ve *syncflow.VerifyError
		require.ErrorAs(t, err, &ve)
		require.Equal(t, syncflow.VerifyError{Stage: syncflow.StageVerifyPatch, View: "dev", Missing: changes("c1")}, *ve)
	})

	t.Run("an advertisement without its manifest lists nothing", func(t *testing.T) {
		after := syncflow.Advertisement{
			"dev":  {Name: "dev", Target: "t1"},
			"feat": advertised(manifest("feat", ident.ScopeDraft, "dev", "c1", "c2", "c3")),
		}
		err := syncflow.VerifyPush(plan, after)
		var ve *syncflow.VerifyError
		require.ErrorAs(t, err, &ve)
		require.Equal(t, syncflow.VerifyError{Stage: syncflow.StageVerifyPatch, View: "dev", Missing: changes("c1", "c2")}, *ve)
	})

	t.Run("a view that did not declare is not checked", func(t *testing.T) {
		noop, err := syncflow.PlanPush([]syncflow.LocalView{dev}, syncflow.Advertisement{"dev": advertised(dev.Manifest)})
		require.NoError(t, err)
		require.NoError(t, syncflow.VerifyPush(noop, syncflow.Advertisement{}))
	})
}
