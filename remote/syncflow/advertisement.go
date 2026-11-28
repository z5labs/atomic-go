package syncflow

import (
	"fmt"

	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/views"
)

// RemoteView is one view a remote advertises.
type RemoteView struct {
	// Name is the view name.
	Name string

	// Target is the key of the view's current snapshot, the base a push
	// compares and swaps against.
	Target string

	// Manifest is the view's manifest, read from its snapshot and
	// verified, or nil when the pack does not carry the snapshot.
	Manifest *views.Manifest
}

// Advertisement is a remote's ref advertisement: the views it holds, by
// name.
type Advertisement map[string]RemoteView

// ParseAdvertisement reads pack, the answer to a refs-only pull, as Atomic's
// push reads its ref advertisement.
//
// Every ref becomes a [RemoteView], its Target the ref's NewTarget; a ref
// named twice keeps its last target, and only that target is read. When
// the pack carries a View object keyed by that target, its snapshot is read
// back as a manifest under the ref's name with [views.Snapshot.Manifest]
// and checked with [views.Manifest.Verify], and set as Manifest. When it
// does not, Manifest is nil and Target is kept. View objects that do not
// decode as a snapshot, or whose bytes do not have their key by
// [VerifyKey], are skipped, as if the pack did not carry them.
//
// A carried snapshot whose state is not base32, or whose declared state
// disagrees with the fold of its log, ends the parse with an error wrapping
// the *[views.ManifestError].
func ParseAdvertisement(pack syncproto.Pack) (Advertisement, error) {
	snapshots, _ := indexSnapshots(pack)
	ad := make(Advertisement, len(pack.Refs))
	for _, ref := range pack.Refs {
		ad[ref.Name] = RemoteView{Name: ref.Name, Target: ref.NewTarget}
	}
	for name, rv := range ad {
		s, ok := snapshots[rv.Target]
		if !ok {
			continue
		}
		m, err := s.Manifest(name)
		if err == nil {
			err = m.Verify()
		}
		if err != nil {
			return nil, fmt.Errorf("syncflow: advertised view %q: %w", name, err)
		}
		rv.Manifest = &m
		ad[name] = rv
	}
	return ad, nil
}

// indexSnapshots decodes pack's View objects, returning those that decode
// and have their key by [VerifyKey], by key, and, for each key with none
// that does, the first failure.
func indexSnapshots(pack syncproto.Pack) (map[string]views.Snapshot, map[string]error) {
	snapshots := make(map[string]views.Snapshot)
	failures := make(map[string]error)
	for _, obj := range pack.Objects {
		if obj.Family != syncproto.View {
			continue
		}
		if _, ok := snapshots[obj.Key]; ok {
			continue
		}
		s, err := views.ParseSnapshot(obj.Bytes)
		if err == nil {
			err = VerifyKey(obj)
		}
		if err != nil {
			if _, ok := failures[obj.Key]; !ok {
				failures[obj.Key] = err
			}
			continue
		}
		snapshots[obj.Key] = s
		delete(failures, obj.Key)
	}
	return snapshots, failures
}
