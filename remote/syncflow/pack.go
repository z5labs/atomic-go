package syncflow

import (
	"context"
	"fmt"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/tags"
	"github.com/z5labs/atomic-go/remote/views"
)

// ChangeSource reads the change files a push sends.
type ChangeSource interface {
	// Change returns the bytes of the change file of h, as stored.
	Change(ctx context.Context, h ident.Hash) ([]byte, error)
}

// Sidecar is an attestation or provenance graph and the changes it covers.
type Sidecar struct {
	// Object is the sidecar as the pack carries it: a [syncproto.Attest]
	// or [syncproto.Provenance] object, under its key.
	Object syncproto.ObjectRecord

	// Covers holds the changes the sidecar attests or explains.
	Covers []ident.Hash
}

// SidecarSource reads the sidecars a push sends.
type SidecarSource interface {
	// SidecarsCovering returns the sidecars that cover h, attestations and
	// provenance graphs alike, in any order.
	SidecarsCovering(ctx context.Context, h ident.Hash) ([]Sidecar, error)
}

// TagSource reads the tags a push sends.
type TagSource interface {
	// Tags returns the tags of the view with the local name view, in the
	// order the pack carries them.
	Tags(ctx context.Context, view string) ([]tags.Record, error)
}

// PackSource is everything [BuildPack] reads from the local repository.
type PackSource interface {
	ChangeSource
	SidecarSource
	TagSource
}

// BuildPack builds the pack that carries plan, reading what it sends from
// src, in the order Atomic's push builds it; see Building the pack in
// docs/remote/flows.md. Its objects are:
//
//  1. for each declared view, root first, a [syncproto.Change] object for
//     each change it stores, in Store order and keyed by [ChangeKey], then a
//     [syncproto.View] object holding its snapshot, minted by
//     [views.FromManifest] from its local manifest with the advertised
//     target as prev;
//  2. every [syncproto.Attest] sidecar, then every [syncproto.Provenance]
//     sidecar, that src returns for a stored change, in the order the
//     stored changes and src return them. A sidecar is carried only when it
//     covers a stored change and every change it covers is in the log of
//     some view of the chain, and once per key;
//  3. every tag src returns for the leaf, under the leaf's local name, as
//     src returns them. A tag's view is not renamed, so it keeps the leaf's
//     local name although the leaf's ref uses its remote one, and its key,
//     [tags.Record.Key], is computed from the local name too.
//
// Its refs are one [syncproto.RefRecord] per declared view, root first:
// named by [LocalView.Name], with ExpectOld from the plan and NewTarget the
// key of the view's snapshot.
//
// Views that do not declare add nothing, whatever their Store holds. Tags
// are added on every push, so the pack is empty only when no view declares
// and the leaf has no tags; Atomic's push then sends nothing more.
//
// An error from src ends the build, wrapped. So does a sidecar of another
// family, and a tag [tags.Record.MarshalPostcard] refuses.
func BuildPack(ctx context.Context, plan Plan, src PackSource) (syncproto.Pack, error) {
	var pack syncproto.Pack
	union := make(map[ident.Hash]bool)
	var stored []ident.Hash
	storedSet := make(map[ident.Hash]bool)
	for _, vp := range plan.Views {
		for _, h := range vp.View.Manifest.Changes {
			union[h] = true
		}
		if !vp.Declare {
			continue
		}
		for _, h := range vp.Store {
			b, err := src.Change(ctx, h)
			if err != nil {
				return syncproto.Pack{}, fmt.Errorf("syncflow: change %s of view %q: %w", h, vp.View.Name(), err)
			}
			pack.Objects = append(pack.Objects, syncproto.ObjectRecord{Family: syncproto.Change, Key: ChangeKey(h), Bytes: b})
			stored = append(stored, h)
			storedSet[h] = true
		}
		var prev string
		if vp.ExpectOld != nil {
			prev = *vp.ExpectOld
		}
		snap := views.FromManifest(vp.View.Manifest, prev)
		key := snap.Key()
		pack.Objects = append(pack.Objects, syncproto.ObjectRecord{Family: syncproto.View, Key: key, Bytes: snap.MarshalCanonical()})
		ref := syncproto.RefRecord{Name: vp.View.Name(), NewTarget: key}
		if vp.ExpectOld != nil {
			old := *vp.ExpectOld
			ref.ExpectOld = &old
		}
		pack.Refs = append(pack.Refs, ref)
	}

	sidecars, err := collectSidecars(ctx, src, stored, storedSet, union)
	if err != nil {
		return syncproto.Pack{}, err
	}
	pack.Objects = append(pack.Objects, sidecars...)

	if len(plan.Views) == 0 {
		return pack, nil
	}
	leaf := plan.Views[len(plan.Views)-1].View.Manifest.Name
	recs, err := src.Tags(ctx, leaf)
	if err != nil {
		return syncproto.Pack{}, fmt.Errorf("syncflow: tags of view %q: %w", leaf, err)
	}
	for _, r := range recs {
		b, err := r.MarshalPostcard()
		if err != nil {
			return syncproto.Pack{}, fmt.Errorf("syncflow: tag %q of view %q: %w", r.Name, leaf, err)
		}
		pack.Objects = append(pack.Objects, syncproto.ObjectRecord{Family: syncproto.Tag, Key: r.Key(), Bytes: b})
	}
	return pack, nil
}

// collectSidecars returns the sidecars src returns for the stored changes
// that a push carries, attestations first, deduplicated by key.
func collectSidecars(ctx context.Context, src SidecarSource, stored []ident.Hash, storedSet, union map[ident.Hash]bool) ([]syncproto.ObjectRecord, error) {
	var attest, prov []syncproto.ObjectRecord
	seen := map[syncproto.Family]map[string]bool{
		syncproto.Attest:     {},
		syncproto.Provenance: {},
	}
	for _, h := range stored {
		scs, err := src.SidecarsCovering(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("syncflow: sidecars covering change %s: %w", h, err)
		}
		for _, sc := range scs {
			obj := sc.Object
			keys, ok := seen[obj.Family]
			if !ok {
				return nil, fmt.Errorf("syncflow: sidecar %q covering change %s is a %v object, not an attestation or provenance graph", obj.Key, h, obj.Family)
			}
			if keys[obj.Key] || !carried(sc.Covers, storedSet, union) {
				continue
			}
			keys[obj.Key] = true
			if obj.Family == syncproto.Attest {
				attest = append(attest, obj)
			} else {
				prov = append(prov, obj)
			}
		}
	}
	return append(attest, prov...), nil
}

// carried reports whether a sidecar covering covers goes into the pack: it
// covers a stored change, and every change it covers is in union.
func carried(covers []ident.Hash, stored, union map[ident.Hash]bool) bool {
	coversStored := false
	for _, h := range covers {
		if !union[h] {
			return false
		}
		coversStored = coversStored || stored[h]
	}
	return coversStored
}
