package project

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
	"github.com/z5labs/atomic-go/remote/syncproto"
)

// The Rust client's names for the sync requests, used as [remote.Error.Op].
const (
	opSyncPush = "sync_push"
	opSyncPull = "sync_pull"
)

// PushSummary is the JSON body of a successful [Client.SyncPush]
// (docs/remote/endpoints/project.md#pushsummary). Both fields are optional
// on the wire and default to 0.
type PushSummary struct {
	// Stored is the number of objects stored or already present.
	Stored uint64 `json:"stored"`

	// RefsMoved is the number of view refs the compare-and-swap batch
	// moved.
	RefsMoved uint64 `json:"refs_moved"`
}

// ObjectSink receives the objects of a pull streamed by
// [Client.SyncPullTo], one at a time and in the order the server sent
// them.
type ObjectSink interface {
	// PutObject stores obj. obj.Bytes is not retained by the pull after
	// PutObject returns, so the sink may keep it. An error aborts the
	// pull.
	PutObject(ctx context.Context, obj syncproto.ObjectRecord) error
}

// syncHeader returns the two headers both sync requests add
// (docs/remote/endpoints/project.md#sync-protocol).
func syncHeader() http.Header {
	return http.Header{
		syncproto.ProtocolHeader: {syncproto.ProtocolVersion},
		"Content-Type":           {syncproto.ContentType},
	}
}

// SyncPush sends p to the project, POSTed to the bare project URL as the
// binary sync protocol, and returns the server's summary
// (docs/remote/endpoints/project.md#sync_push). The body is encoded and
// buffered before anything is sent, and sent with its exact length.
//
// A pack that will not encode fails with a [remote.KindProtocol] before
// anything is sent.
//
// 200 and 201 succeed. Their body is read as a [PushSummary]; a body that
// cannot be read, or is not one, gives a zero PushSummary and no error, as
// in the Rust client, and is logged once at Debug level. At most
// [remote.MaxErrorBody] bytes of it are read.
//
// A 409, which the Rust client describes as a divergent, non-fast-forward
// ref move, is a [remote.KindProtocol] carrying the response body. 401 and
// 403 are [remote.KindAuthenticationFailed], and any other status, 404
// included, is a [remote.KindHTTP] (docs/remote/errors.md#project-protocol-failures).
// A request that cannot be sent is a [remote.KindConnectionFailed].
func (c *Client) SyncPush(ctx context.Context, p syncproto.Pack) (PushSummary, error) {
	body, err := c.codec.EncodePack(p)
	if err != nil {
		return PushSummary{}, c.encodeError(opSyncPush, http.MethodPost, "failed to encode sync pack: ", err)
	}

	resp, err := c.requester.Do(ctx, wire.Request{
		Op:     opSyncPush,
		Method: http.MethodPost,
		URL:    c.url.URL(),
		Header: syncHeader(),
		Body:   body,
	})
	if err != nil {
		return PushSummary{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return c.readSummary(ctx, resp), nil
	case http.StatusConflict:
		text := wire.ErrorBody(resp)
		return PushSummary{}, &remote.Error{
			Kind:             remote.KindProtocol,
			Op:               opSyncPush,
			Method:           http.MethodPost,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			Message:          text,
			Body:             text,
			ServerMinVersion: resp.MinVersion,
		}
	default:
		return PushSummary{}, wire.StatusError(opSyncPush, resp, wire.ErrorBody(resp))
	}
}

// readSummary reads a successful push's body as a PushSummary, or logs why
// it could not and returns a zero one.
func (c *Client) readSummary(ctx context.Context, resp *wire.Response) PushSummary {
	b, err := io.ReadAll(io.LimitReader(resp.Body, remote.MaxErrorBody))
	var s PushSummary
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		c.logger.DebugContext(ctx, "sync push succeeded but its summary could not be read; reporting zero counts",
			slog.String("url", resp.URL),
			slog.Int("status", resp.StatusCode),
			slog.String("error", err.Error()),
		)
		return PushSummary{}
	}
	return s
}

// SyncPull asks the project for the objects and refs w names and returns
// the pack the server answers with (docs/remote/endpoints/project.md#sync_pull).
// The whole pack is held in memory; [Client.SyncPullTo] holds one object at
// a time.
//
// The request is a GET of the bare project URL whose body is w, encoded,
// sent with its exact length and the same two sync headers as
// [Client.SyncPush]. Wants that will not encode fail with a
// [remote.KindProtocol] before anything is sent.
//
// Only 200 succeeds, and its Content-Type is not checked. 401 and 403 are
// [remote.KindAuthenticationFailed], and any other status, 404 and 409
// included, is a [remote.KindHTTP].
//
// A 200 body that is not valid zstd, or does not decompress to a pack, is a
// [remote.KindVersionMismatch] carrying the server's minimum version, if it
// sent one, and the client's. One that inflates past the Client's maximum
// pack size ([WithMaxPackSize]) is a [remote.KindProtocol] "failed to
// decode sync pack: …". A body that cannot be read, or a request that cannot
// be sent, is a [remote.KindConnectionFailed]
// (docs/remote/errors.md#decode-failures-and-version-mismatch).
func (c *Client) SyncPull(ctx context.Context, w syncproto.Wants) (syncproto.Pack, error) {
	resp, err := c.pull(ctx, w)
	if err != nil {
		return syncproto.Pack{}, err
	}
	defer resp.Body.Close()

	p, err := c.codec.DecodePack(resp.Body)
	if err != nil {
		return syncproto.Pack{}, c.decodeError(ctx, resp, err)
	}
	return p, nil
}

// SyncPullTo sends the same request as [Client.SyncPull], but streams the
// pack: each object is passed to sink as soon as it is decoded, so only one
// is held in memory at a time. It returns the pack's refs once every object
// has been stored.
//
// It fails as SyncPull does. Objects are stored as they arrive, so a body
// found to be bad after some objects were stored has still stored them.
//
// If sink returns an error the pull stops at once, the response body is
// closed, and the error returned is a [remote.KindOther] wrapping the
// sink's error, so [errors.Is] and [errors.As] reach it. A nil sink fails
// with a [remote.KindOther] before anything is sent.
func (c *Client) SyncPullTo(ctx context.Context, w syncproto.Wants, sink ObjectSink) ([]syncproto.RefRecord, error) {
	if sink == nil {
		return nil, &remote.Error{
			Kind:    remote.KindOther,
			Op:      opSyncPull,
			Method:  http.MethodGet,
			URL:     c.url.String(),
			Message: "project: SyncPullTo: nil ObjectSink",
		}
	}

	resp, err := c.pull(ctx, w)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	refs, err := c.codec.StreamPack(resp.Body, func(obj syncproto.ObjectRecord) error {
		if err := sink.PutObject(ctx, obj); err != nil {
			return &sinkError{err: err}
		}
		return nil
	})
	if err == nil {
		return refs, nil
	}
	var se *sinkError
	if errors.As(err, &se) {
		return nil, &remote.Error{
			Kind:    remote.KindOther,
			Op:      opSyncPull,
			Method:  http.MethodGet,
			URL:     resp.URL,
			Message: "failed to store pulled object: " + se.err.Error(),
			Err:     se.err,
		}
	}
	return nil, c.decodeError(ctx, resp, err)
}

// sinkError marks an error that came from the ObjectSink, so that it is not
// mistaken for one from the body.
type sinkError struct {
	err error
}

func (e *sinkError) Error() string {
	return e.err.Error()
}

// pull sends the sync_pull request for w and returns a 200 response; any
// other status is returned as its error, the body already closed.
func (c *Client) pull(ctx context.Context, w syncproto.Wants) (*wire.Response, error) {
	body, err := c.codec.EncodeWants(w)
	if err != nil {
		return nil, c.encodeError(opSyncPull, http.MethodGet, "failed to encode sync wants: ", err)
	}

	resp, err := c.requester.Do(ctx, wire.Request{
		Op:     opSyncPull,
		Method: http.MethodGet,
		URL:    c.url.URL(),
		Header: syncHeader(),
		Body:   body,
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, wire.StatusError(opSyncPull, resp, wire.ErrorBody(resp))
	}
	return resp, nil
}

// decodeError maps the failure to decode a pulled pack to the error the
// Rust client returns for it.
func (c *Client) decodeError(ctx context.Context, resp *wire.Response, err error) *remote.Error {
	e := &remote.Error{
		Op:               opSyncPull,
		Method:           http.MethodGet,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		ServerMinVersion: resp.MinVersion,
		Err:              err,
	}
	var de *syncproto.DecodeError
	switch {
	case errors.Is(err, syncproto.ErrTooLarge):
		e.Kind = remote.KindProtocol
		e.Message = "failed to decode sync pack: " + err.Error()
	case errors.As(err, &de):
		e.Kind = remote.KindVersionMismatch
		e.Message = err.Error()
		e.ClientVersion = c.clientVersion
	default:
		// The body could not be read: the connection failed, the
		// request's context ended, or a Content-Encoding did not decode.
		e.Kind = remote.KindConnectionFailed
		e.ContextDone = ctx.Err() != nil
	}
	return e
}

// encodeError is the error for a request body that will not encode, which
// fails before anything is sent.
func (c *Client) encodeError(op, method, prefix string, err error) *remote.Error {
	return &remote.Error{
		Kind:    remote.KindProtocol,
		Op:      op,
		Method:  method,
		URL:     c.url.String(),
		Message: prefix + err.Error(),
		Err:     err,
	}
}
