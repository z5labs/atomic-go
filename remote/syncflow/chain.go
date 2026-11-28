package syncflow

import (
	"fmt"
	"slices"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/views"
)

// ChainView is one view of a chain [ResolveChain] read from a pack.
type ChainView struct {
	// Name is the view name.
	Name string

	// Target is the key of the view's snapshot, its ref's NewTarget.
	Target string

	// Snapshot is the view's snapshot, as the pack carries it.
	Snapshot views.Snapshot

	// Manifest is Snapshot read back as a manifest under Name. Its state is
	// checked against its log only with [VerifyFold].
	Manifest views.Manifest

	// SetIDMismatch reports that Snapshot's own_set_id is not the SetId of
	// its log, or is not base32. Atomic warns about this and carries on, so
	// it is a flag rather than an error.
	SetIDMismatch bool
}

// ResolveOption changes what [ResolveChain] checks.
type ResolveOption func(*resolveOptions)

type resolveOptions struct {
	verifyFold bool
}

// VerifyFold makes [ResolveChain] check each manifest's declared state
// against the fold of its log with [views.Manifest.Verify], as clone does.
// Without it the state is not checked, as pull does not check it.
func VerifyFold() ResolveOption {
	return func(o *resolveOptions) {
		o.verifyFold = true
	}
}

// ResolveChain reads the chain of leaf from pack, the answer to a full pull
// or clone, as Atomic's pull and clone rebuild it: leaf's ref and snapshot,
// then its parent's, up to a view with no parent. The chain is returned
// root first, the order its views are applied in, and leaf is last.
//
// Each view's ref is found by name and its snapshot by the ref's NewTarget,
// among the pack's View objects; one that does not decode as a snapshot is
// skipped, as if the pack did not carry it. A ref named twice keeps its
// last target.
//
// It fails, returning no chain:
//
//   - with an error wrapping [ErrViewNotInPack] when the pack has no ref
//     for leaf or an ancestor, or no snapshot for one's target. When the
//     pack carries a View object under that target that does not decode,
//     the error wraps its decode failure as well;
//   - with an error wrapping [ErrViewCycle] when the chain reaches a view
//     it has already passed through;
//   - with an error wrapping the *[views.ManifestError] when a snapshot's
//     state or a change hash is not base32, or, with [VerifyFold], its
//     declared state disagrees with the fold of its log.
//
// A snapshot whose own_set_id disagrees with its log only sets
// [ChainView.SetIDMismatch].
func ResolveChain(pack syncproto.Pack, leaf string, opts ...ResolveOption) ([]ChainView, error) {
	var o resolveOptions
	for _, opt := range opts {
		opt(&o)
	}

	targets := make(map[string]string, len(pack.Refs))
	for _, ref := range pack.Refs {
		targets[ref.Name] = ref.NewTarget
	}
	snapshots, failures := indexSnapshots(pack)

	var chain []ChainView
	seen := make(map[string]bool)
	for name := leaf; ; {
		if seen[name] {
			return nil, fmt.Errorf("%w: view %q is its own ancestor in the chain of %q", ErrViewCycle, name, leaf)
		}
		seen[name] = true

		target, ok := targets[name]
		if !ok {
			return nil, fmt.Errorf("%w: no ref for view %q in the chain of %q", ErrViewNotInPack, name, leaf)
		}
		s, ok := snapshots[target]
		if !ok {
			if err, bad := failures[target]; bad {
				return nil, fmt.Errorf("%w: snapshot %q of view %q: %w", ErrViewNotInPack, target, name, err)
			}
			return nil, fmt.Errorf("%w: no snapshot %q for view %q in the chain of %q", ErrViewNotInPack, target, name, leaf)
		}
		m, err := s.Manifest(name)
		if err == nil && o.verifyFold {
			err = m.Verify()
		}
		if err != nil {
			return nil, fmt.Errorf("syncflow: view %q: %w", name, err)
		}
		chain = append(chain, ChainView{
			Name:          name,
			Target:        target,
			Snapshot:      s,
			Manifest:      m,
			SetIDMismatch: setIDMismatch(s.OwnSetID, m.Changes),
		})

		if s.ParentView == "" {
			break
		}
		name = s.ParentView
	}
	slices.Reverse(chain)
	return chain, nil
}

// setIDMismatch reports whether declared is not the base32 SetId of log.
func setIDMismatch(declared string, log []ident.Hash) bool {
	id, err := ident.ParseSetID(declared)
	return err != nil || id != ident.SetIDOf(log)
}
