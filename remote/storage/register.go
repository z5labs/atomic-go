package storage

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
)

// registerOp is the [remote.Error.Op] of every error [Register] returns.
const registerOp = "register"

// singleTenantMode is the mode a single-tenant server reports.
const singleTenantMode = "single"

// RegisterResponse is what a server reports when it registers an identity
// (docs/remote/endpoints/storage.md#execute).
//
// Each field is the reply's member of the same name in snake_case, read
// from the reply's data member when it has one and from the top-level
// object otherwise, so both an [ApiResponse] envelope and a bare object
// work. A field is nil when its member is absent or holds anything but a
// JSON string, as the Rust client reads it.
//
// [ApiResponse]: https://github.com/z5labs/atomic-go/blob/main/docs/remote/endpoints/storage.md#apiresponse
type RegisterResponse struct {
	// TenantID is the ID of the tenant the registration created.
	TenantID *string

	// Slug is the tenant's org slug. The Rust client falls back to the
	// identity's name when it is nil.
	Slug *string

	// BaseURL is the tenant's base URL.
	BaseURL *string

	// Mode is the server's tenancy mode; see [RegisterResponse.SingleTenant].
	Mode *string

	// Role is the org role the identity received on a single-tenant server.
	Role *string
}

// SingleTenant reports whether the server is single-tenant: whether Mode is
// "single". Any other mode, or none, means multi-tenant, as an older server
// that reports no mode is.
func (r RegisterResponse) SingleTenant() bool {
	return r.Mode != nil && *r.Mode == singleTenantMode
}

// Register sends reg to server, POSTing its JSON to {server}/register, and
// returns what the server reports (docs/remote/endpoints/storage.md#execute).
//
// The request goes out as the Rust CLI sends it: through a plain client
// rather than a [Client]'s pipeline, with Content-Type: application/json as
// the only header Register sets. It sets no User-Agent, so net/http sends
// none, and no credentials, because the signature in reg is the credential,
// so d must not add any: it must not be an [*http.Client] whose transport is
// an auth.Transport. Whatever else d's transport adds is d's to decide; an
// [http.Transport] adds Accept-Encoding: gzip and decodes the reply, as
// reqwest does with its gzip feature. The response's minimum-version header
// is not checked and nothing is logged.
//
// Trailing '/' characters are removed from server's path, so
// "https://atomic.storage/" posts to https://atomic.storage/register. server
// must be an absolute http or https URL with a host and no userinfo, query
// or fragment, and its path must hold no "." or ".." segment.
//
// d and server are required. A nil d or server, or a server Register
// refuses, is refused before anything is sent, with one error made with
// [errors.Join] that names every problem, each wrapping [ErrInvalidArgument].
//
// # Failures
//
// Every other failure is a [*remote.Error] whose Op is "register".
//
// A request that cannot be sent, or a response body that cannot be read
// whatever its status, is a [remote.KindConnectionFailed] whose cause is the
// failure, so a cancelled context matches [context.Canceled].
//
// A status outside 2xx is a [remote.KindHTTP], 401 and 403 included, whose
// Message is the body's top-level string message, else the string
// error.message, else "Server returned {status}", where {status} is the
// code and its reason phrase, as in "Server returned 502 Bad Gateway". The
// [remote.Error] sentinels work from the status, so a 409, which the Rust
// client reports as a public key already registered, matches
// [remote.ErrConflict]. Body holds at most [remote.MaxErrorBody] bytes of
// the body.
//
// A 2xx body that is not JSON, or is longer than [MaxResponseBody] bytes, is
// a [remote.KindProtocol]. A 2xx body that is JSON but not an object is not
// an error: every field of the response is nil.
func Register(ctx context.Context, d Doer, server *url.URL, reg identity.Registration) (RegisterResponse, error) {
	u, err := registerURL(d, server)
	if err != nil {
		return RegisterResponse{}, err
	}

	// json/v2 escapes no HTML characters, as serde_json does not.
	body, err := json.Marshal(reg)
	if err != nil {
		return RegisterResponse{}, &remote.Error{
			Kind:    remote.KindOther,
			Op:      registerOp,
			Method:  http.MethodPost,
			URL:     u.String(),
			Message: "storage: marshal registration: " + err.Error(),
			Err:     err,
		}
	}

	// An empty UserAgent sends no User-Agent at all, and an empty
	// ClientVersion turns off the minimum-version check, so the discarded
	// logger is never written to.
	requester := wire.NewRequester(d, slog.New(slog.DiscardHandler), wire.Profile{
		Header:     http.Header{"Content-Type": {contentType}},
		MaxDecoded: MaxResponseBody,
	})
	resp, err := requester.Do(ctx, wire.Request{
		Op:     registerOp,
		Method: http.MethodPost,
		URL:    u,
		Body:   body,
	})
	if err != nil {
		return RegisterResponse{}, err
	}
	defer resp.Body.Close()

	// The Rust client reads the body before it looks at the status, so a
	// body that fails to read fails the same way on every status.
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBody+1))
	if err != nil {
		return RegisterResponse{}, &remote.Error{
			Kind:             remote.KindConnectionFailed,
			Op:               registerOp,
			Method:           http.MethodPost,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			ServerMinVersion: resp.MinVersion,
			ContextDone:      ctx.Err() != nil,
			Err:              err,
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return RegisterResponse{}, registerStatusError(resp, b)
	}
	if len(b) > MaxResponseBody {
		return RegisterResponse{}, registerProtocolError(resp, "response body is larger than "+strconv.Itoa(MaxResponseBody)+" bytes", nil)
	}
	out, err := parseRegisterResponse(b)
	if err != nil {
		return RegisterResponse{}, registerProtocolError(resp, "invalid server response: "+err.Error()+" (body: "+quote(b)+")", err)
	}
	return out, nil
}

// registerURL returns {server}/register, with server's trailing '/'
// characters removed, or every reason d and server cannot be used.
func registerURL(d Doer, server *url.URL) (*url.URL, error) {
	var errs []error
	if d == nil {
		errs = append(errs, fmt.Errorf("%w: the Doer is required", ErrInvalidArgument))
	}
	var trimmed *url.URL
	if server == nil {
		errs = append(errs, fmt.Errorf("%w: the server URL is required", ErrInvalidArgument))
	} else {
		var serverErrs []error
		trimmed, serverErrs = checkBase("the server URL", server)
		errs = append(errs, serverErrs...)
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("storage: Register: %w", errors.Join(errs...))
	}
	u := *trimmed
	u.RawPath = trimmed.EscapedPath() + "/register"
	u.Path = trimmed.Path + "/register"
	return &u, nil
}

// registerStatusError is the [remote.KindHTTP] for a registration whose
// status is outside 2xx, with the message the Rust CLI reports for body.
func registerStatusError(resp *wire.Response, body []byte) *remote.Error {
	shown := body
	if len(shown) > remote.MaxErrorBody {
		shown = shown[:remote.MaxErrorBody]
	}
	return &remote.Error{
		Kind:             remote.KindHTTP,
		Op:               registerOp,
		Method:           http.MethodPost,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		Message:          registerErrorMessage(resp.StatusCode, body),
		Body:             string(shown),
		ServerMinVersion: resp.MinVersion,
	}
}

// registerErrorMessage returns the body's top-level string message, else
// its string error.message, else "Server returned {status}" written as the
// Rust http crate writes a status: its code and its reason phrase.
func registerErrorMessage(status int, body []byte) string {
	if top, err := decodeLoose(body); err == nil {
		members := objectMembers(top)
		if m, ok := stringMember(members, "message"); ok {
			return m
		}
		if m, ok := stringMember(objectMembers(members["error"]), "message"); ok {
			return m
		}
	}
	return "Server returned " + strconv.Itoa(status) + " " + reasonPhrase(status)
}

// reasonPhrase returns the reason phrase the Rust http crate gives status:
// net/http's, except for the codes whose phrase the crate writes
// differently, and "<unknown status code>" for a code with none.
func reasonPhrase(status int) string {
	switch status {
	case http.StatusRequestEntityTooLarge:
		return "Payload Too Large"
	case http.StatusRequestURITooLong:
		return "URI Too Long"
	case http.StatusRequestedRangeNotSatisfiable:
		return "Range Not Satisfiable"
	}
	if reason := http.StatusText(status); reason != "" {
		return reason
	}
	return "<unknown status code>"
}

// registerProtocolError is the [remote.KindProtocol] for a 2xx registration
// response the client will not accept.
func registerProtocolError(resp *wire.Response, msg string, err error) *remote.Error {
	return &remote.Error{
		Kind:             remote.KindProtocol,
		Op:               registerOp,
		Method:           http.MethodPost,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		Message:          msg,
		ServerMinVersion: resp.MinVersion,
		Err:              err,
	}
}

// parseRegisterResponse reads a 2xx registration body as the Rust CLI does:
// from data when the body is an object holding it, whatever data holds, and
// from the body otherwise, keeping only members whose values are strings.
func parseRegisterResponse(b []byte) (RegisterResponse, error) {
	top, err := decodeLoose(b)
	if err != nil {
		return RegisterResponse{}, err
	}
	members := objectMembers(top)
	if data, ok := members["data"]; ok {
		members = objectMembers(data)
	}
	read := func(name string) *string {
		s, ok := stringMember(members, name)
		if !ok {
			return nil
		}
		return &s
	}
	return RegisterResponse{
		TenantID: read("tenant_id"),
		Slug:     read("slug"),
		BaseURL:  read("base_url"),
		Mode:     read("mode"),
		Role:     read("role"),
	}, nil
}

// looseJSON is how a registration reply is decoded: as serde_json decodes
// it into a Value, which keeps the last of a member written twice, and as
// reqwest's text() leaves it, with invalid UTF-8 replaced.
func looseJSON() json.Options {
	return json.JoinOptions(jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
}

// decodeLoose returns b as one JSON value, or the reason it is not one.
func decodeLoose(b []byte) (jsontext.Value, error) {
	var v jsontext.Value
	if err := json.Unmarshal(b, &v, looseJSON()); err != nil {
		return nil, err
	}
	return v, nil
}

// objectMembers returns the members of the JSON object v, the last of any
// name written twice winning, or nil when v is not an object.
func objectMembers(v jsontext.Value) map[string]jsontext.Value {
	if v.Kind() != '{' {
		return nil
	}
	dec := jsontext.NewDecoder(bytes.NewReader(v), looseJSON())
	if _, err := dec.ReadToken(); err != nil {
		return nil
	}
	members := make(map[string]jsontext.Value)
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil
		}
		// A token is voided by the next read, so the name is copied out
		// first.
		name := tok.String()
		val, err := dec.ReadValue()
		if err != nil {
			return nil
		}
		members[name] = val.Clone()
	}
	return members
}

// stringMember returns the member name of members when it is a JSON
// string.
func stringMember(members map[string]jsontext.Value, name string) (string, bool) {
	v, ok := members[name]
	if !ok || v.Kind() != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(v, &s, looseJSON()); err != nil {
		return "", false
	}
	return s, true
}
