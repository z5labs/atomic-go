package project

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
	"github.com/z5labs/atomic-go/remote/views"
)

// opListViewRefs is the Rust client's name for the view inventory request,
// used as [remote.Error.Op].
const opListViewRefs = "list_view_refs"

// ListViewRefs returns the project's view inventory: every view the remote
// holds, with its change count, state and effective SetId
// (docs/remote/endpoints/project.md#list_view_refs).
//
// The request is a GET of the project's refs/views resource, beside /code,
// with no query, no body and no Atomic-Protocol header. Only 200 succeeds.
// Its body is read as text and parsed with [views.ParseInventory]; a body
// with no rows is a nil slice and no error.
//
// A row that does not parse is a [remote.KindProtocol] "Invalid view
// inventory row {line}: {reason}", whose cause is the
// [*views.InventoryError]. A body larger than the Client's maximum pack
// size ([WithMaxPackSize]) is a [remote.KindProtocol] too, and one that
// cannot be read is a [remote.KindConnectionFailed].
//
// 404 is a [remote.KindRepositoryNotFound]. 401 and 403 are
// [remote.KindAuthenticationFailed]: the Rust client reports them as an
// HttpError here, unlike every other request, and this client does not
// follow it in that, so a caller can test for an authentication failure
// with [remote.Error.IsAuth] whichever request failed. Any other status is a
// [remote.KindHTTP]. Unlike the Rust client, a newer [remote.MinVersionHeader]
// is logged as a warning here too.
func (c *Client) ListViewRefs(ctx context.Context) ([]views.Info, error) {
	resp, err := c.requester.Do(ctx, wire.Request{
		Op:     opListViewRefs,
		Method: http.MethodGet,
		URL:    c.url.Resource("refs/views"),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, &remote.Error{
			Kind:             remote.KindRepositoryNotFound,
			Op:               opListViewRefs,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			ServerMinVersion: resp.MinVersion,
		}
	default:
		return nil, wire.StatusError(opListViewRefs, resp, wire.ErrorBody(resp))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxPackSize+1))
	if err != nil {
		return nil, &remote.Error{
			Kind:             remote.KindConnectionFailed,
			Op:               opListViewRefs,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			ServerMinVersion: resp.MinVersion,
			ContextDone:      ctx.Err() != nil,
			Err:              err,
		}
	}
	if int64(len(body)) > c.maxPackSize {
		return nil, inventoryError(resp, "view inventory is larger than "+strconv.FormatInt(c.maxPackSize, 10)+" bytes", nil)
	}

	infos, err := views.ParseInventory(string(body))
	if err != nil {
		var ie *views.InventoryError
		if errors.As(err, &ie) {
			return nil, inventoryError(resp, ie.Error(), err)
		}
		return nil, inventoryError(resp, "invalid view inventory: "+err.Error(), err)
	}
	return infos, nil
}

// inventoryError is the [remote.KindProtocol] for a view inventory the
// client will not accept.
func inventoryError(resp *wire.Response, msg string, err error) *remote.Error {
	return &remote.Error{
		Kind:             remote.KindProtocol,
		Op:               opListViewRefs,
		Method:           http.MethodGet,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		Message:          msg,
		ServerMinVersion: resp.MinVersion,
		Err:              err,
	}
}
