package storage

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
)

// ErrNoData is the cause of the [remote.KindProtocol] for a successful
// response whose envelope says success: true but carries no data.
var ErrNoData = errors.New("storage: response had success=true but no data")

// FailureError is the cause of the [remote.KindProtocol] for a 2xx response
// whose envelope says success: false.
type FailureError struct {
	// Status is the response's status.
	Status int

	// Err is the envelope's error, or nil when it had none.
	Err *APIError
}

// Error returns "{code}: {message}" from the envelope's error, or "request
// failed with no error details" when it had none, as the Rust client does.
func (e *FailureError) Error() string {
	if e.Err == nil {
		return "request failed with no error details"
	}
	return e.Err.Code + ": " + e.Err.Message
}

// maxQuoted is the most bytes of an unparsable body the error quotes, as the
// Rust client quotes it.
const maxQuoted = 2000

// request is one management API request.
type request struct {
	// op names the client method sending it, such as "get_workspace". It
	// becomes [remote.Error.Op].
	op string

	// method is the HTTP method.
	method string

	// path is the escaped path after the base URL, starting with '/'.
	// Every variable part of it comes from [segment] or [component].
	path string

	// query is the raw query, without its '?', sent as it is.
	query string

	// body, when it is not nil, is marshalled as JSON and sent.
	body any
}

// call sends r with c and returns the data of its response's envelope. A
// DELETE returns the zero T. See [Client] for how each failure maps to a
// [remote.Kind].
//
// A path [Client.requestURL] refuses is an error wrapping
// [ErrInvalidArgument], and a body that does not marshal is a
// [remote.KindOther]; nothing is sent for either.
func call[T any](ctx context.Context, c *Client, r request) (T, error) {
	var zero T
	u, err := c.requestURL(r.path, r.query)
	if err != nil {
		return zero, err
	}

	var body []byte
	if r.body != nil {
		// json/v2 escapes no HTML characters, as serde_json does not.
		body, err = json.Marshal(r.body)
		if err != nil {
			return zero, &remote.Error{
				Kind:    remote.KindOther,
				Op:      r.op,
				Method:  r.method,
				URL:     u.String(),
				Message: "storage: marshal request body: " + err.Error(),
				Err:     err,
			}
		}
	}

	// wire checks the minimum-version header, and logs its advice, before
	// anything here looks at the status.
	resp, err := c.requester.Do(ctx, wire.Request{
		Op:     r.op,
		Method: r.method,
		URL:    u,
		Body:   body,
	})
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return zero, statusError(r, resp)
	}
	if r.method == http.MethodDelete {
		return zero, nil
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBody+1))
	if err != nil {
		return zero, &remote.Error{
			Kind:             remote.KindConnectionFailed,
			Op:               r.op,
			Method:           r.method,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			ServerMinVersion: resp.MinVersion,
			ContextDone:      ctx.Err() != nil,
			Err:              err,
		}
	}
	if len(b) > MaxResponseBody {
		return zero, protocolError(r, resp, "response body is larger than "+strconv.Itoa(MaxResponseBody)+" bytes", nil)
	}

	var env apiResponse[T]
	if err := json.Unmarshal(b, &env); err != nil {
		return zero, protocolError(r, resp, "invalid response JSON: "+err.Error()+" (body: "+quote(b)+")", err)
	}
	if !env.Success {
		fe := &FailureError{Status: resp.StatusCode, Err: env.Error}
		perr := protocolError(r, resp, fe.Error(), fe)
		if env.Error != nil {
			perr.Code = env.Error.Code
		}
		return zero, perr
	}
	if env.Data == nil {
		return zero, protocolError(r, resp, "response had success=true but no data", ErrNoData)
	}
	return *env.Data, nil
}

// statusError is the [remote.KindHTTP] for a response whose status is
// outside 2xx, with the server's code and message when its body carries
// them (docs/remote/errors.md#storage-api-json).
func statusError(r request, resp *wire.Response) *remote.Error {
	body := wire.ErrorBody(resp)
	e := &remote.Error{
		Kind:             remote.KindHTTP,
		Op:               r.op,
		Method:           r.method,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		Message:          "HTTP " + strconv.Itoa(resp.StatusCode),
		Body:             body,
		ServerMinVersion: resp.MinVersion,
	}
	if apiErr, ok := errorBody([]byte(body)); ok {
		e.Code = apiErr.Code
		e.Message = apiErr.Message
	}
	return e
}

// errorBody returns the error an error response's body carries, looked for
// in the Rust client's order: an envelope with an error, then a bare error.
func errorBody(b []byte) (*APIError, bool) {
	var env apiResponse[jsontext.Value]
	if err := json.Unmarshal(b, &env); err == nil && env.Error != nil {
		return env.Error, true
	}
	var bare APIError
	if err := json.Unmarshal(b, &bare); err == nil {
		return &bare, true
	}
	return nil, false
}

// protocolError is the [remote.KindProtocol] for a 2xx response the client
// will not accept.
func protocolError(r request, resp *wire.Response, msg string, err error) *remote.Error {
	return &remote.Error{
		Kind:             remote.KindProtocol,
		Op:               r.op,
		Method:           r.method,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		Message:          msg,
		ServerMinVersion: resp.MinVersion,
		Err:              err,
	}
}

// quote returns b for an error message: whole when it is at most maxQuoted
// bytes, and otherwise cut at a character boundary no later than that and
// followed by how long it was.
func quote(b []byte) string {
	if len(b) <= maxQuoted {
		return string(b)
	}
	cut := maxQuoted
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	return string(b[:cut]) + "… (" + strconv.Itoa(len(b)) + " bytes total)"
}
