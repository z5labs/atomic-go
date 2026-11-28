package syncflow

import (
	"fmt"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/views"
)

// LocalView is one view of the local chain a push sends.
type LocalView struct {
	// Manifest is the view's manifest as stored locally: its own name,
	// scope, parent and change log.
	Manifest views.Manifest

	// RemoteName is the name the view has on the remote, or "" when it has
	// its local name there.
	//
	// Atomic's push renames only the leaf: to_view names the leaf on the
	// remote, and every ancestor keeps its local name, so its parent link
	// still names a view the remote knows. To push as Atomic does, set
	// RemoteName on the leaf alone. See Before the first request in
	// docs/remote/flows.md.
	RemoteName string
}

// Name returns the name v has on the remote: RemoteName, or Manifest.Name
// when RemoteName is "". [PlanPush] looks v up in the advertisement under
// it, and [BuildPack] names v's ref by it.
func (v LocalView) Name() string {
	if v.RemoteName != "" {
		return v.RemoteName
	}
	return v.Manifest.Name
}

// PlanOption changes how [PlanPush] plans.
type PlanOption func(*planOptions)

type planOptions struct {
	storeAll bool
}

// StoreAll makes [PlanPush] store a declared view's whole local log, as
// Atomic's push does with all, rather than only the changes the remote does
// not already list. A change is still stored once across the chain, and a
// view with nothing to declare still stores nothing.
func StoreAll() PlanOption {
	return func(o *planOptions) {
		o.storeAll = true
	}
}

// ViewPlan is what a push does for one view of the chain.
type ViewPlan struct {
	// View is the local view.
	View LocalView

	// Store holds the changes the pack carries for this view, in log
	// order. It is empty when Declare is false.
	Store []ident.Hash

	// Declare reports that the push moves the view's ref to a snapshot
	// minted from its local manifest. A view that does not declare is a
	// no-op: the pack carries nothing for it.
	Declare bool

	// Diverged reports that the local and remote logs each list changes the
	// other lacks. The push goes ahead regardless, as Atomic's does; see
	// Diverged views in docs/remote/flows.md.
	Diverged bool

	// ExpectOld is the target the remote advertised for the view, the base
	// its ref move compares and swaps against, or nil when the view was not
	// advertised.
	ExpectOld *string
}

// Plan is what a push sends: one [ViewPlan] per view of the chain, root
// first, no-op views included.
type Plan struct {
	Views []ViewPlan
}

// The fields a [Conflict] can name.
const (
	ConflictScope  = "scope"
	ConflictParent = "parent"
)

// Conflict reports a view whose identity differs between the local chain
// and the remote: its scope or its parent. Atomic's push returns it in its
// response rather than as an error, and sends nothing.
type Conflict struct {
	// View is the view's remote name.
	View string

	// Field is what differs: [ConflictScope] or [ConflictParent].
	Field string

	// Local and Remote are the field's values on each side. A parent is ""
	// for a root view.
	Local, Remote string
}

// Error implements the error interface.
func (c *Conflict) Error() string {
	return fmt.Sprintf("syncflow: view %q: local %s %q differs from remote %s %q", c.View, c.Field, c.Local, c.Field, c.Remote)
}

// PlanPush decides what a push of chain sends, given adv, the remote's
// advertisement of it, as Atomic's push plans. chain is the local view and
// its ancestors, root first, and each view is looked up in adv under its
// [LocalView.Name].
//
// Each view is planned by the row of Planning what to send in
// docs/remote/flows.md that its advertisement matches:
//
//   - A view adv does not list, or lists without a manifest, stores its
//     log and declares.
//   - A view whose remote manifest has another scope, or another parent,
//     ends planning: PlanPush returns an empty plan and a *[Conflict]
//     naming it. Scopes are compared as draft or not, in any case.
//   - A view whose remote manifest has its scope and parent stores the
//     changes of its log that the manifest lacks, and declares unless
//     every change of its log is already listed. When the remote also lists
//     changes the local log lacks, Diverged is set and the plan is the
//     same.
//
// A view that declares stores the changes of its log, in log order, that no
// advertised manifest of it or of an earlier view lists and that no earlier
// view stores; with [StoreAll], only the last is subtracted. So a change in
// more than one view's log, such as one a draft inherits, is stored once,
// by the first view that stores it. Atomic stores such a change once per
// view; one copy reaches the same objects.
//
// ExpectOld is the advertised target of every view adv lists, whether or
// not it carries the manifest.
func PlanPush(chain []LocalView, adv Advertisement, opts ...PlanOption) (Plan, error) {
	var o planOptions
	for _, opt := range opts {
		opt(&o)
	}

	advertised := make(map[ident.Hash]bool)
	stored := make(map[ident.Hash]bool)
	plan := Plan{Views: make([]ViewPlan, 0, len(chain))}
	for _, v := range chain {
		vp := ViewPlan{View: v, Declare: true}
		local := v.Manifest
		rv, ok := adv[v.Name()]
		if ok {
			target := rv.Target
			vp.ExpectOld = &target
		}
		if ok && rv.Manifest != nil {
			remote := rv.Manifest
			if local.Scope.IsDraft() != remote.Scope.IsDraft() {
				return Plan{}, &Conflict{View: v.Name(), Field: ConflictScope, Local: string(local.Scope), Remote: string(remote.Scope)}
			}
			if local.Parent != remote.Parent {
				return Plan{}, &Conflict{View: v.Name(), Field: ConflictParent, Local: local.Parent, Remote: remote.Parent}
			}
			localOnly := !subset(local.Changes, setOf(remote.Changes))
			vp.Declare = localOnly
			vp.Diverged = localOnly && !subset(remote.Changes, setOf(local.Changes))
			for _, h := range remote.Changes {
				advertised[h] = true
			}
		}
		if vp.Declare {
			for _, h := range local.Changes {
				if stored[h] || (advertised[h] && !o.storeAll) {
					continue
				}
				stored[h] = true
				vp.Store = append(vp.Store, h)
			}
		}
		plan.Views = append(plan.Views, vp)
	}
	return plan, nil
}

// setOf returns the changes of log as a set.
func setOf(log []ident.Hash) map[ident.Hash]bool {
	set := make(map[ident.Hash]bool, len(log))
	for _, h := range log {
		set[h] = true
	}
	return set
}

// subset reports whether every change of log is in set.
func subset(log []ident.Hash, set map[ident.Hash]bool) bool {
	for _, h := range log {
		if !set[h] {
			return false
		}
	}
	return true
}
