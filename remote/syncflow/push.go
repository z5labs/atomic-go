package syncflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/syncproto"
)

// Remote is the project endpoint [Push] talks to. *[project.Client]
// satisfies it; a test can wrap one, or stand in for it.
type Remote interface {
	// SyncPull answers w with a pack, as [project.Client.SyncPull] does.
	SyncPull(ctx context.Context, w syncproto.Wants) (syncproto.Pack, error)

	// SyncPush sends p and returns the server's summary, as
	// [project.Client.SyncPush] does.
	SyncPush(ctx context.Context, p syncproto.Pack) (project.PushSummary, error)

	// BindProjectIdentity offers doc for the remote to bind, as
	// [project.Client.BindProjectIdentity] does.
	BindProjectIdentity(ctx context.Context, doc json.RawMessage) (project.BindResult, error)
}

// PushOption changes how [Push] pushes.
type PushOption func(*pushOptions)

type pushOptions struct {
	plan     []PlanOption
	dryRun   bool
	identity json.RawMessage
}

// WithPlanOptions passes opts to [PlanPush]. Repeated, the options of every
// call are passed, in order.
func WithPlanOptions(opts ...PlanOption) PushOption {
	return func(o *pushOptions) {
		o.plan = append(o.plan, opts...)
	}
}

// DryRun makes [Push] stop once it has planned, after the advertisement and
// before anything is built, bound or sent.
func DryRun() PushOption {
	return func(o *pushOptions) {
		o.dryRun = true
	}
}

// WithIdentity makes [Push] offer doc, the local project identity document,
// for the remote to bind before it sends the pack. Without it, or with an
// empty doc, nothing is bound, as Atomic's push binds only when the
// repository has an identity. The last call wins.
func WithIdentity(doc json.RawMessage) PushOption {
	return func(o *pushOptions) {
		o.identity = doc
	}
}

// The stages a [StageError] can name, after the network steps of Atomic's
// push; see Push sequence in docs/remote/flows.md.
const (
	// StageAdvertise: request 1, the ref advertisement, failed or its
	// answer did not parse. Nothing was sent.
	StageAdvertise = "advertise"

	// StageBind: requests 2 and 2b, the project identity bind, failed or
	// found the remote bound to a different identity. Nothing was sent.
	StageBind = "bind"

	// StageSend: request 3, the pack, failed. Whether any of it landed is
	// not known; see Partial failure and repeats in docs/remote/flows.md.
	StageSend = "send"

	// StageVerify: request 4, the re-read advertisement, failed, did not
	// parse, or did not show what was pushed. The pack had already been
	// accepted.
	StageVerify = "verify"
)

// StageError reports the step of a [Push] that failed.
type StageError struct {
	// Stage is [StageAdvertise], [StageBind], [StageSend] or
	// [StageVerify].
	Stage string

	// Err is why: the [Remote]'s error, a parse error, an
	// *[IdentityConflict] for [StageBind] or a *[VerifyError] for
	// [StageVerify].
	Err error
}

// Error implements the error interface. A [StageVerify] error says that
// the pack landed.
func (e *StageError) Error() string {
	if e.Stage == StageVerify {
		return fmt.Sprintf("syncflow: push %s: the pack landed, but remote verification failed: %v", e.Stage, e.Err)
	}
	return fmt.Sprintf("syncflow: push %s: %v", e.Stage, e.Err)
}

// Unwrap returns Err, so [errors.Is] and [errors.As] reach it.
func (e *StageError) Unwrap() error {
	return e.Err
}

// Landed reports whether the remote accepted the pack before the push
// failed, which is so only at [StageVerify]. At [StageSend] it is false,
// but the remote may still have stored some of the pack.
func (e *StageError) Landed() bool {
	return e.Stage == StageVerify
}

// IdentityConflict reports a remote bound to a project identity other than
// the one [WithIdentity] offered.
type IdentityConflict struct {
	// Code and Message are the server's code and message for the
	// conflict, each "" when it sent none; see [project.BindResult].
	Code, Message string

	// Document is the identity the remote holds, read after the conflict,
	// or nil when that read found none or failed.
	Document json.RawMessage
}

// Error implements the error interface.
func (c *IdentityConflict) Error() string {
	msg := "the remote is bound to a different project identity"
	if c.Code != "" {
		msg += ": " + c.Code
	}
	if c.Message != "" {
		msg += ": " + c.Message
	}
	return "syncflow: " + msg
}

// PushResult is what a [Push] did.
type PushResult struct {
	// Plan is what the push planned. It is empty when it ended with a
	// Conflict or at [StageAdvertise].
	Plan Plan

	// Conflict is the view whose scope or parent differs from the remote,
	// or nil. A push that finds one sends nothing and returns no error, as
	// Atomic's push returns it in its response.
	Conflict *Conflict

	// Summary is the server's answer to the pack, or the zero
	// PushSummary when none was sent.
	Summary project.PushSummary

	// PushedChanges is the number of [syncproto.Change] objects in the
	// pack sent, or 0 when none was sent.
	PushedChanges int
}

// Push pushes chain, the local view and its ancestors, root first, to r,
// reading what it sends from src, in the order Atomic's push handler runs;
// see Push sequence in docs/remote/flows.md:
//
//  1. Advertise: SyncPull of [syncproto.Advertise] naming each view's
//     [LocalView.Name], read with [ParseAdvertisement].
//  2. Plan with [PlanPush] and the options of [WithPlanOptions]. A
//     *[Conflict] ends the push in PushResult.Conflict, with no error.
//     [DryRun] ends it here too.
//  3. Build with [BuildPack]. An empty pack ends the push: nothing is
//     bound or sent.
//  4. Bind, when [WithIdentity] gave a document, with
//     BindProjectIdentity. [project.BindBound] and
//     [project.BindUnsupported] carry on; [project.BindConflict] ends the
//     push with an *[IdentityConflict].
//  5. Send the pack with SyncPush.
//  6. Verify: advertise again, as in 1, and check the answer with
//     [VerifyPush].
//
// An empty chain has nothing to push and sends nothing: advertising no
// names would ask for every view on the remote.
//
// A failure at a network step is a *[StageError] naming it and wrapping
// the cause, so [errors.Is] reaches the sentinels of a
// [github.com/z5labs/atomic-go/remote.Error], such as remote.ErrConflict
// for a ref the server refused to move. A failure at
// [StageVerify] means the pack landed; see [StageError.Landed]. A send that
// fails is reported as it is: Push neither re-reads the advertisement nor
// retries, since what a failed send leaves on the remote is not known.
//
// An error from src while building is returned wrapped, not as a
// *StageError, since nothing was sent. The PushResult returned with an
// error holds what the push had learned by then.
func Push(ctx context.Context, r Remote, chain []LocalView, src PackSource, opts ...PushOption) (PushResult, error) {
	var o pushOptions
	for _, opt := range opts {
		opt(&o)
	}
	var res PushResult
	if len(chain) == 0 {
		return res, nil
	}
	names := make([]string, len(chain))
	for i, v := range chain {
		names[i] = v.Name()
	}

	adv, err := advertise(ctx, r, names)
	if err != nil {
		return res, &StageError{Stage: StageAdvertise, Err: err}
	}

	plan, err := PlanPush(chain, adv, o.plan...)
	var conflict *Conflict
	if errors.As(err, &conflict) {
		res.Conflict = conflict
		return res, nil
	}
	if err != nil {
		return res, err
	}
	res.Plan = plan
	if o.dryRun {
		return res, nil
	}

	pack, err := BuildPack(ctx, plan, src)
	if err != nil {
		return res, err
	}
	if len(pack.Objects) == 0 && len(pack.Refs) == 0 {
		return res, nil
	}

	if len(o.identity) > 0 {
		if err := bind(ctx, r, o.identity); err != nil {
			return res, &StageError{Stage: StageBind, Err: err}
		}
	}

	summary, err := r.SyncPush(ctx, pack)
	if err != nil {
		return res, &StageError{Stage: StageSend, Err: err}
	}
	res.Summary = summary
	for _, obj := range pack.Objects {
		if obj.Family == syncproto.Change {
			res.PushedChanges++
		}
	}

	after, err := advertise(ctx, r, names)
	if err == nil {
		err = VerifyPush(plan, after)
	}
	if err != nil {
		return res, &StageError{Stage: StageVerify, Err: err}
	}
	return res, nil
}

// advertise reads r's ref advertisement of the views names.
func advertise(ctx context.Context, r Remote, names []string) (Advertisement, error) {
	pack, err := r.SyncPull(ctx, syncproto.Advertise(names...))
	if err != nil {
		return nil, err
	}
	return ParseAdvertisement(pack)
}

// bind offers doc to r, returning an *IdentityConflict when r holds
// another identity.
func bind(ctx context.Context, r Remote, doc json.RawMessage) error {
	res, err := r.BindProjectIdentity(ctx, doc)
	if err != nil {
		return err
	}
	switch res.Outcome {
	case project.BindBound, project.BindUnsupported:
		return nil
	case project.BindConflict:
		c := &IdentityConflict{Code: res.ServerCode, Message: res.ServerMessage}
		if res.Remote.State == project.IdentityBound {
			c.Document = res.Remote.Document
		}
		return c
	default:
		return fmt.Errorf("syncflow: bind returned unknown outcome %v", res.Outcome)
	}
}
