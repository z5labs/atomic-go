package syncflow

import (
	"fmt"

	"github.com/z5labs/atomic-go/ident"
)

// The stages a [VerifyError] can name, as Atomic's push reports them.
const (
	// StageVerifyUnion: a declared view is not advertised after the push.
	StageVerifyUnion = "verify-union"

	// StageVerifyPatch: a declared view is advertised after the push
	// without every change of its local log.
	StageVerifyPatch = "verify-patch"
)

// VerifyError reports a declared view that the remote does not hold as a
// push left it.
type VerifyError struct {
	// Stage is [StageVerifyUnion] or [StageVerifyPatch].
	Stage string

	// View is the view's remote name.
	View string

	// Missing holds, for [StageVerifyPatch], the changes of the local log
	// that the remote does not list, in log order. It is nil for
	// [StageVerifyUnion].
	Missing []ident.Hash
}

// Error implements the error interface.
func (e *VerifyError) Error() string {
	if e.Stage == StageVerifyUnion {
		return fmt.Sprintf("syncflow: %s: view %q is not advertised after the push", e.Stage, e.View)
	}
	return fmt.Sprintf("syncflow: %s: view %q is advertised without %d of its local changes", e.Stage, e.View, len(e.Missing))
}

// VerifyPush checks after, the remote's advertisement re-read once the pack
// has been sent, against plan, as Atomic's push does; see Request 4: verify
// in docs/remote/flows.md.
//
// Every view plan declares must be advertised under its [LocalView.Name],
// or VerifyPush returns a *[VerifyError] of stage [StageVerifyUnion], and
// its advertised manifest must list every change of its local log, or a
// *[VerifyError] of stage [StageVerifyPatch] naming those it lacks. A view
// advertised without its manifest lists no changes. Views are checked root
// first, and the first that fails is returned.
//
// A remote that lists more changes than the local log passes, since a
// concurrent writer may have widened the set. Views that do not declare are
// not checked.
func VerifyPush(plan Plan, after Advertisement) error {
	for _, vp := range plan.Views {
		if !vp.Declare {
			continue
		}
		name := vp.View.Name()
		rv, ok := after[name]
		if !ok {
			return &VerifyError{Stage: StageVerifyUnion, View: name}
		}
		var listed map[ident.Hash]bool
		if rv.Manifest != nil {
			listed = setOf(rv.Manifest.Changes)
		}
		var missing []ident.Hash
		for _, h := range vp.View.Manifest.Changes {
			if !listed[h] {
				missing = append(missing, h)
			}
		}
		if len(missing) > 0 {
			return &VerifyError{Stage: StageVerifyPatch, View: name, Missing: missing}
		}
	}
	return nil
}
