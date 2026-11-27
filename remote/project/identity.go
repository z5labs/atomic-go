package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
)

// The Rust client's names for the project identity requests, used as
// [remote.Error.Op].
const (
	opFetchProjectIdentity = "fetch_project_identity"
	opBindProjectIdentity  = "bind_project_identity"
)

// identityQuery is the query both identity requests send
// (docs/remote/endpoints/project.md#project-identity).
const identityQuery = "project-identity=1"

// maxIdentityBody is the most bytes of a 200 [Client.FetchProjectIdentity]
// body that are read. An identity document is a few hundred bytes.
const maxIdentityBody = 1 << 20

// IdentityState is what a remote said about its project identity binding.
type IdentityState int

const (
	// IdentityUnknown: the binding could not be read. A
	// [Client.FetchProjectIdentity] never returns it; it is the state of a
	// [BindResult.Remote] whose follow-up read failed or was unsupported.
	IdentityUnknown IdentityState = iota

	// IdentityUnsupported: the remote predates the identity endpoint.
	IdentityUnsupported

	// IdentityUnbound: the project has no identity bound yet.
	IdentityUnbound

	// IdentityBound: the project has an identity, in
	// [RemoteIdentity.Document].
	IdentityBound
)

var identityStateNames = [...]string{
	IdentityUnknown:     "Unknown",
	IdentityUnsupported: "Unsupported",
	IdentityUnbound:     "Unbound",
	IdentityBound:       "Bound",
}

// String returns the state's name, such as "Bound", or "IdentityState(n)"
// for a value that is not one of the constants.
func (s IdentityState) String() string {
	if s >= 0 && int(s) < len(identityStateNames) {
		return identityStateNames[s]
	}
	return "IdentityState(" + strconv.Itoa(int(s)) + ")"
}

// RemoteIdentity is a remote's project identity binding.
type RemoteIdentity struct {
	// State is what the remote said.
	State IdentityState

	// Document is the bound identity document, exactly the bytes of the
	// response's identity member as the server sent them, white space
	// included. It is set only when State is [IdentityBound].
	Document json.RawMessage
}

// BindOutcome is the result of offering an identity with
// [Client.BindProjectIdentity]. Its zero value is not an outcome.
type BindOutcome int

const (
	// BindBound: the identity is bound now, or was already bound
	// identically.
	BindBound BindOutcome = iota + 1

	// BindUnsupported: the remote predates the identity endpoint.
	BindUnsupported

	// BindConflict: the remote holds a different identity, or says it
	// does.
	BindConflict
)

var bindOutcomeNames = [...]string{
	BindBound:       "Bound",
	BindUnsupported: "Unsupported",
	BindConflict:    "Conflict",
}

// String returns the outcome's name, such as "Conflict", or
// "BindOutcome(n)" for a value that is not one of the constants.
func (o BindOutcome) String() string {
	if o > 0 && int(o) < len(bindOutcomeNames) {
		return bindOutcomeNames[o]
	}
	return "BindOutcome(" + strconv.Itoa(int(o)) + ")"
}

// BindResult is what [Client.BindProjectIdentity] learned.
type BindResult struct {
	// Outcome is the result of the bind.
	Outcome BindOutcome

	// ServerCode and ServerMessage are the code and message of a
	// [BindConflict]'s response body, read flat or nested under "error"
	// (docs/remote/endpoints/project.md#bindservererror). Each is "" when
	// the body does not carry it as a string.
	ServerCode, ServerMessage string

	// Remote is the binding a [BindConflict]'s follow-up read found:
	// [IdentityBound] with its document, [IdentityUnbound] when the server
	// contradicts its own conflict, or [IdentityUnknown] when the read
	// failed or was unsupported. It is the zero RemoteIdentity for any
	// other outcome.
	Remote RemoteIdentity
}

// FetchProjectIdentity reads the project identity the remote has bound
// (docs/remote/endpoints/project.md#fetch_project_identity). The request
// is a GET of the project URL with the query project-identity=1, no body
// and no Atomic-Protocol header.
//
// A 200 body is read as the JSON object IdentityResponse
// (docs/remote/endpoints/project.md#identityresponse). "bound": true with
// an "identity" member is [IdentityBound], with the member's bytes as the
// document; "bound": false is [IdentityUnbound], whatever else the body
// holds; a body with no "bound" member, or "bound": true and no
// "identity", is [IdentityUnsupported], an older server's answer. A member
// whose value is null counts as absent, as in the Rust client.
//
// A 200 body that is not that object is a [remote.KindProtocol] "invalid
// project-identity response: …": one that is not JSON, not an object, or
// larger than 1 MiB, or whose "bound" is not a boolean or "identity" not
// an object, or which repeats either member. A body that cannot be read is
// a [remote.KindConnectionFailed], as it is for every other request here;
// the Rust client reports it as a ProtocolError, which would make a
// cancelled or timed-out read look like a server fault.
//
// 404, 405 and 501 are [IdentityUnsupported] and no error. 401 and 403 are
// [remote.KindAuthenticationFailed] and any other status is a
// [remote.KindHTTP]. Unlike the Rust client, a newer
// [remote.MinVersionHeader] is logged as a warning here too.
func (c *Client) FetchProjectIdentity(ctx context.Context) (RemoteIdentity, error) {
	resp, err := c.requester.Do(ctx, wire.Request{
		Op:     opFetchProjectIdentity,
		Method: http.MethodGet,
		URL:    c.url.WithRawQuery(identityQuery),
	})
	if err != nil {
		return RemoteIdentity{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return RemoteIdentity{State: IdentityUnsupported}, nil
	default:
		return RemoteIdentity{}, wire.StatusError(opFetchProjectIdentity, resp, wire.ErrorBody(resp))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIdentityBody+1))
	if err != nil {
		return RemoteIdentity{}, &remote.Error{
			Kind:             remote.KindConnectionFailed,
			Op:               opFetchProjectIdentity,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			ServerMinVersion: resp.MinVersion,
			ContextDone:      ctx.Err() != nil,
			Err:              err,
		}
	}
	var id RemoteIdentity
	if len(body) > maxIdentityBody {
		err = fmt.Errorf("body is larger than %d bytes", maxIdentityBody)
	} else {
		id, err = parseIdentityResponse(body)
	}
	if err != nil {
		return RemoteIdentity{}, &remote.Error{
			Kind:             remote.KindProtocol,
			Op:               opFetchProjectIdentity,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			Message:          "invalid project-identity response: " + err.Error(),
			ServerMinVersion: resp.MinVersion,
			Err:              err,
		}
	}
	return id, nil
}

// parseIdentityResponse reads a 200 identity body by the rules on
// [Client.FetchProjectIdentity].
func parseIdentityResponse(body []byte) (RemoteIdentity, error) {
	members, err := topLevelMembers(body)
	if err != nil {
		return RemoteIdentity{}, err
	}

	var bound *bool
	if raw, ok := members["bound"]; ok && !isNull(raw) {
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return RemoteIdentity{}, fmt.Errorf("bound is %s, not a boolean", jsonType(raw))
		}
		bound = &b
	}
	var identity json.RawMessage
	if raw, ok := members["identity"]; ok && !isNull(raw) {
		if raw[0] != '{' {
			return RemoteIdentity{}, fmt.Errorf("identity is %s, not an object", jsonType(raw))
		}
		identity = raw
	}

	switch {
	case bound != nil && *bound && identity != nil:
		return RemoteIdentity{State: IdentityBound, Document: identity}, nil
	case bound != nil && !*bound:
		return RemoteIdentity{State: IdentityUnbound}, nil
	default:
		return RemoteIdentity{State: IdentityUnsupported}, nil
	}
}

// topLevelMembers returns the members of the JSON object body, each value's
// bytes exactly as sent. Names are matched exactly, unlike encoding/json's
// struct decoding, and a repeated bound or identity member is refused, as
// serde's derived decoder refuses it.
func topLevelMembers(body []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(body) {
		return nil, errors.New("body is not valid UTF-8")
	}
	if !json.Valid(body) {
		return nil, errors.New("body is not valid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("body is %s, not an object", jsonType(bytes.TrimSpace(body)))
	}
	members := make(map[string]json.RawMessage)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		if _, dup := members[name]; dup && (name == "bound" || name == "identity") {
			return nil, fmt.Errorf("duplicate member %q", name)
		}
		members[name] = raw
	}
	return members, nil
}

// isNull reports whether raw, a valid JSON value, is null.
func isNull(raw json.RawMessage) bool {
	return string(raw) == "null"
}

// jsonType names the type of raw, a valid JSON value with no surrounding
// white space, for an error message.
func jsonType(raw []byte) string {
	if len(raw) == 0 {
		return "empty"
	}
	switch raw[0] {
	case '{':
		return "an object"
	case '[':
		return "an array"
	case '"':
		return "a string"
	case 't', 'f':
		return "a boolean"
	case 'n':
		return "null"
	default:
		return "a number"
	}
}

// BindProjectIdentity offers doc, a project identity document, for the
// remote to bind, first write wins
// (docs/remote/endpoints/project.md#bind_project_identity).
//
// doc is sent verbatim, as the body of a POST of the project URL with the
// query project-identity=1, a Content-Type of application/json and no
// Atomic-Protocol header. The Rust client re-serializes the document
// instead; whether the server canonicalizes it before hashing is an open
// question, so its bytes are not changed here. A doc that is not valid
// JSON fails with a [remote.KindProtocol] before anything is sent.
//
// 200 and 201 are [BindBound], and 404, 405 and 501 [BindUnsupported],
// neither with an error; the body is not read. 401 and 403 are
// [remote.KindAuthenticationFailed] and any other status but 409 is a
// [remote.KindHTTP].
//
// A 409 is [BindConflict], and no error. Its body is read for the server's
// code and message (see [BindResult]), and then the remote's binding is
// read with [Client.FetchProjectIdentity] into [BindResult.Remote]. That
// second request failing does not fail the bind: Remote's state is then
// [IdentityUnknown], as it is when the read reports [IdentityUnsupported],
// and the failure is logged at Debug level.
//
// Unlike the Rust client, a newer [remote.MinVersionHeader] is logged as a
// warning here too, once for each response: a 409 and its follow-up read
// can log it twice.
func (c *Client) BindProjectIdentity(ctx context.Context, doc json.RawMessage) (BindResult, error) {
	if err := json.Unmarshal(doc, new(json.RawMessage)); err != nil {
		return BindResult{}, &remote.Error{
			Kind:    remote.KindProtocol,
			Op:      opBindProjectIdentity,
			Method:  http.MethodPost,
			URL:     c.url.WithRawQuery(identityQuery).String(),
			Message: "identity is not valid JSON: " + err.Error(),
			Err:     err,
		}
	}

	resp, err := c.requester.Do(ctx, wire.Request{
		Op:     opBindProjectIdentity,
		Method: http.MethodPost,
		URL:    c.url.WithRawQuery(identityQuery),
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   doc,
	})
	if err != nil {
		return BindResult{}, err
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		resp.Body.Close()
		return BindResult{Outcome: BindBound}, nil
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		resp.Body.Close()
		return BindResult{Outcome: BindUnsupported}, nil
	case http.StatusConflict:
	default:
		defer resp.Body.Close()
		return BindResult{}, wire.StatusError(opBindProjectIdentity, resp, wire.ErrorBody(resp))
	}

	// Close the 409 before the follow-up read, so its connection can serve
	// that read.
	code, message := parseBindServerError(wire.ErrorBody(resp))
	resp.Body.Close()

	result := BindResult{Outcome: BindConflict, ServerCode: code, ServerMessage: message}
	id, err := c.FetchProjectIdentity(ctx)
	switch {
	case err != nil:
		c.logger.DebugContext(ctx, "project identity conflict; the remote's binding could not be read",
			slog.String("url", resp.URL),
			slog.String("error", err.Error()),
		)
	case id.State != IdentityUnsupported:
		result.Remote = id
	}
	return result, nil
}

// parseBindServerError reads a 409 bind body's code and message
// (docs/remote/endpoints/project.md#bindservererror) as the Rust client
// does: from the "error" member when the body has one, whatever its value,
// and otherwise from the top level. A member that is not a string, and
// every member of a body that is not a JSON object, is "".
func parseBindServerError(body string) (code, message string) {
	var top any
	if err := json.Unmarshal([]byte(body), &top); err != nil {
		return "", ""
	}
	obj, _ := top.(map[string]any)
	if nested, ok := obj["error"]; ok {
		obj, _ = nested.(map[string]any)
	}
	code, _ = obj["code"].(string)
	message, _ = obj["message"].(string)
	return code, message
}
