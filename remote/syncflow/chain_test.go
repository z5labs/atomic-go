package syncflow_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/changefixture"
	"github.com/z5labs/atomic-go/remote/syncflow"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/views"
)

// devManifest is a shared root view with two changes.
func devManifest() views.Manifest {
	m := views.Manifest{Name: "dev", Scope: ident.ScopeShared, Changes: changes("c1", "c2")}
	m.State = m.Fold()
	return m
}

// featureManifest is a draft of dev with one change of its own after the
// two it inherits.
func featureManifest() views.Manifest {
	m := views.Manifest{Name: "feature", Scope: ident.ScopeDraft, Parent: "dev", Changes: changes("c1", "c2", "c3")}
	m.State = m.Fold()
	return m
}

func viewObject(s views.Snapshot) syncproto.ObjectRecord {
	return syncproto.ObjectRecord{Family: syncproto.View, Key: s.Key(), Bytes: s.MarshalCanonical()}
}

func ref(name string, s views.Snapshot) syncproto.RefRecord {
	return syncproto.RefRecord{Name: name, NewTarget: s.Key()}
}

// devAndFeature is a full pull of feature: both views' refs and snapshots,
// with the worked example's change body among them.
func devAndFeature() (pack syncproto.Pack, dev, feature views.Snapshot) {
	dev = views.FromManifest(devManifest(), "")
	feature = views.FromManifest(featureManifest(), "")
	pack = syncproto.Pack{
		Objects: []syncproto.ObjectRecord{
			{Family: syncproto.Change, Key: changefixture.WorkedExampleHash, Bytes: changefixture.WorkedExample()},
			viewObject(dev),
			viewObject(feature),
		},
		Refs: []syncproto.RefRecord{ref("dev", dev), ref("feature", feature)},
	}
	return pack, dev, feature
}

func TestParseAdvertisement(t *testing.T) {
	pack, dev, feature := devAndFeature()

	ad, err := syncflow.ParseAdvertisement(pack)
	require.NoError(t, err)

	devM, featureM := devManifest(), featureManifest()
	require.Equal(t, syncflow.Advertisement{
		"dev":     {Name: "dev", Target: dev.Key(), Manifest: &devM},
		"feature": {Name: "feature", Target: feature.Key(), Manifest: &featureM},
	}, ad)
}

func TestParseAdvertisementAbsentSnapshot(t *testing.T) {
	pack, dev, feature := devAndFeature()
	pack.Objects = []syncproto.ObjectRecord{viewObject(dev)}

	ad, err := syncflow.ParseAdvertisement(pack)
	require.NoError(t, err)
	require.Len(t, ad, 2)
	require.NotNil(t, ad["dev"].Manifest)
	require.Equal(t, syncflow.RemoteView{Name: "feature", Target: feature.Key()}, ad["feature"])
}

func TestParseAdvertisementSkipsUndecodableViews(t *testing.T) {
	pack, _, feature := devAndFeature()
	pack.Objects = append(pack.Objects, syncproto.ObjectRecord{Family: syncproto.View, Key: "garbage", Bytes: []byte("{")})
	pack.Refs = append(pack.Refs, syncproto.RefRecord{Name: "broken", NewTarget: "garbage"})
	// An undecodable object under a key a good one also carries does not
	// hide the good one.
	pack.Objects = append([]syncproto.ObjectRecord{{Family: syncproto.View, Key: feature.Key(), Bytes: []byte("{")}}, pack.Objects...)

	ad, err := syncflow.ParseAdvertisement(pack)
	require.NoError(t, err)
	require.Equal(t, syncflow.RemoteView{Name: "broken", Target: "garbage"}, ad["broken"])
	require.NotNil(t, ad["feature"].Manifest)
}

func TestParseAdvertisementFoldMismatch(t *testing.T) {
	pack, dev, _ := devAndFeature()
	bad := dev
	bad.MerkleState = ident.Sum([]byte("not the fold")).String()
	pack.Objects = append(pack.Objects, viewObject(bad))
	pack.Refs[0] = ref("dev", bad)

	_, err := syncflow.ParseAdvertisement(pack)
	var me *views.ManifestError
	require.ErrorAs(t, err, &me)
	require.Equal(t, views.ManifestStateMismatch, me.Kind)
}

func TestParseAdvertisementEmpty(t *testing.T) {
	ad, err := syncflow.ParseAdvertisement(syncproto.Pack{})
	require.NoError(t, err)
	require.Empty(t, ad)
}

func TestResolveChain(t *testing.T) {
	pack, dev, feature := devAndFeature()

	for _, opts := range [][]syncflow.ResolveOption{nil, {syncflow.VerifyFold()}} {
		chain, err := syncflow.ResolveChain(pack, "feature", opts...)
		require.NoError(t, err)
		require.Equal(t, []syncflow.ChainView{
			{Name: "dev", Target: dev.Key(), Snapshot: dev, Manifest: devManifest()},
			{Name: "feature", Target: feature.Key(), Snapshot: feature, Manifest: featureManifest()},
		}, chain)
	}

	chain, err := syncflow.ResolveChain(pack, "dev")
	require.NoError(t, err)
	require.Len(t, chain, 1)
	require.Equal(t, "dev", chain[0].Name)

	// Every object the chain names still verifies against its key.
	for _, obj := range pack.Objects {
		require.NoError(t, syncflow.VerifyKey(obj))
	}
}

func TestResolveChainFoldMismatch(t *testing.T) {
	pack, _, feature := devAndFeature()
	bad := feature
	bad.MerkleState = ident.Sum([]byte("not the fold")).String()
	pack.Objects = append(pack.Objects, viewObject(bad))
	pack.Refs[1] = ref("feature", bad)

	chain, err := syncflow.ResolveChain(pack, "feature")
	require.NoError(t, err)
	require.Len(t, chain, 2)
	require.Equal(t, bad, chain[1].Snapshot)

	chain, err = syncflow.ResolveChain(pack, "feature", syncflow.VerifyFold())
	require.Nil(t, chain)
	var me *views.ManifestError
	require.ErrorAs(t, err, &me)
	require.Equal(t, views.ManifestStateMismatch, me.Kind)
}

func TestResolveChainSetIDMismatch(t *testing.T) {
	pack, _, feature := devAndFeature()
	bad := feature
	bad.OwnSetID = ident.SetIDOf(changes("c3")).String()
	pack.Objects = append(pack.Objects, viewObject(bad))
	pack.Refs[1] = ref("feature", bad)

	chain, err := syncflow.ResolveChain(pack, "feature", syncflow.VerifyFold())
	require.NoError(t, err)
	require.False(t, chain[0].SetIDMismatch)
	require.True(t, chain[1].SetIDMismatch)

	unparsable := feature
	unparsable.OwnSetID = "not a set id"
	pack.Objects = append(pack.Objects, viewObject(unparsable))
	pack.Refs[1] = ref("feature", unparsable)

	chain, err = syncflow.ResolveChain(pack, "feature")
	require.NoError(t, err)
	require.True(t, chain[1].SetIDMismatch)
}

func TestResolveChainMissingAncestor(t *testing.T) {
	t.Run("no ref", func(t *testing.T) {
		pack, _, _ := devAndFeature()
		pack.Refs = pack.Refs[1:]

		chain, err := syncflow.ResolveChain(pack, "feature")
		require.Nil(t, chain)
		require.ErrorIs(t, err, syncflow.ErrViewNotInPack)
		require.ErrorContains(t, err, `"dev"`)
	})

	t.Run("no snapshot", func(t *testing.T) {
		pack, _, feature := devAndFeature()
		pack.Objects = []syncproto.ObjectRecord{viewObject(feature)}

		_, err := syncflow.ResolveChain(pack, "feature")
		require.ErrorIs(t, err, syncflow.ErrViewNotInPack)
	})

	t.Run("an undecodable snapshot", func(t *testing.T) {
		pack, dev, feature := devAndFeature()
		pack.Objects = []syncproto.ObjectRecord{
			viewObject(feature),
			{Family: syncproto.View, Key: dev.Key(), Bytes: []byte("{")},
		}

		_, err := syncflow.ResolveChain(pack, "feature")
		require.ErrorIs(t, err, syncflow.ErrViewNotInPack)
		require.ErrorIs(t, err, views.ErrInvalidSnapshot)
	})

	t.Run("a snapshot under another family", func(t *testing.T) {
		pack, dev, feature := devAndFeature()
		pack.Objects = []syncproto.ObjectRecord{
			viewObject(feature),
			{Family: syncproto.Attest, Key: dev.Key(), Bytes: dev.MarshalCanonical()},
		}

		_, err := syncflow.ResolveChain(pack, "feature")
		require.ErrorIs(t, err, syncflow.ErrViewNotInPack)
	})

	t.Run("no leaf", func(t *testing.T) {
		pack, _, _ := devAndFeature()

		_, err := syncflow.ResolveChain(pack, "release")
		require.ErrorIs(t, err, syncflow.ErrViewNotInPack)
	})
}

func TestResolveChainCycle(t *testing.T) {
	a := views.FromManifest(views.Manifest{Name: "a", Scope: ident.ScopeDraft, Parent: "b", Changes: changes("c1")}, "")
	b := views.FromManifest(views.Manifest{Name: "b", Scope: ident.ScopeDraft, Parent: "a", Changes: changes("c2")}, "")
	self := views.FromManifest(views.Manifest{Name: "self", Scope: ident.ScopeDraft, Parent: "self"}, "")
	pack := syncproto.Pack{
		Objects: []syncproto.ObjectRecord{viewObject(a), viewObject(b), viewObject(self)},
		Refs:    []syncproto.RefRecord{ref("a", a), ref("b", b), ref("self", self)},
	}

	for _, leaf := range []string{"a", "b", "self"} {
		t.Run(leaf, func(t *testing.T) {
			chain, err := syncflow.ResolveChain(pack, leaf)
			require.Nil(t, chain)
			require.ErrorIs(t, err, syncflow.ErrViewCycle)
		})
	}
}

func TestResolveChainKeepsTheLastRef(t *testing.T) {
	pack, dev, _ := devAndFeature()
	older := views.FromManifest(views.Manifest{Name: "dev", Scope: ident.ScopeShared, Changes: changes("c1")}, "")
	pack.Objects = append(pack.Objects, viewObject(older))
	pack.Refs = append([]syncproto.RefRecord{ref("dev", older)}, pack.Refs...)

	chain, err := syncflow.ResolveChain(pack, "dev")
	require.NoError(t, err)
	require.Equal(t, dev.Key(), chain[0].Target)
}
