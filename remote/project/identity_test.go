package project_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project"
)

const identityURI = projectPath + "?project-identity=1"

// boundDocument is the identity document of the bound wire-test body in
// docs/remote/endpoints/project.md#fetch_project_identity, with its white
// space as written there.
const boundDocument = `{"@context": {"atomic": "https://atomic.dev/ns/workflow#", "dcterms": "http://purl.org/dc/terms/"}, "@type": "atomic:ProjectIdentity", "atomic:projectId": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "atomic:schemaVersion": 1, "dcterms:created": "2026-10-26T00:00:00Z"}`

// boundBody is the bound wire-test body itself.
const boundBody = `{"bound": true, "hash": "abababababababababababababababababababababababababababababababab", "identity": ` + boundDocument + `}`

// exchange is one request a sequenceHandler saw.
type exchange struct {
	method     string
	requestURI string
	header     http.Header
	body       []byte
}

// sequenceHandler answers the nth request with responders[n] and records
// every request in got. A request past the last responder is a 599.
func sequenceHandler(got *[]exchange, responders ...func(http.ResponseWriter)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*got = append(*got, exchange{method: r.Method, requestURI: r.RequestURI, header: r.Header.Clone(), body: body})
		n := len(*got) - 1
		if n >= len(responders) {
			w.WriteHeader(599)
			return
		}
		responders[n](w)
	})
}

func TestFetchProjectIdentityRequest(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, []byte(`{"bound": false}`))))

	_, err := c.FetchProjectIdentity(context.Background())
	require.NoError(t, err)

	require.Equal(t, 1, got.calls)
	require.Equal(t, http.MethodGet, got.method)
	require.Equal(t, identityURI, got.requestURI)
	require.Empty(t, got.header.Values("Atomic-Protocol"))
	require.Empty(t, got.header.Values("Content-Type"))
	require.Empty(t, got.body)
}

func TestFetchProjectIdentityWireBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want project.RemoteIdentity
	}{
		{name: "bound", body: boundBody, want: project.RemoteIdentity{State: project.IdentityBound, Document: json.RawMessage(boundDocument)}},
		{name: "unbound", body: `{"bound": false}`, want: project.RemoteIdentity{State: project.IdentityUnbound}},
		{name: "older server", body: `{"workspace": "w", "project": "p", "stack": "dev"}`, want: project.RemoteIdentity{State: project.IdentityUnsupported}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(tc.body), "Content-Type", "application/json")))

			got, err := c.FetchProjectIdentity(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want.State, got.State)
			// The bytes, not merely an equivalent document.
			require.Equal(t, string(tc.want.Document), string(got.Document))
		})
	}
}

func TestFetchProjectIdentityShapes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		state project.IdentityState
	}{
		{name: "unbound whatever else it holds", body: `{"bound": false, "identity": {"x": 1}}`, state: project.IdentityUnbound},
		{name: "bound without identity", body: `{"bound": true}`, state: project.IdentityUnsupported},
		{name: "bound with a null identity", body: `{"bound": true, "identity": null}`, state: project.IdentityUnsupported},
		{name: "null bound", body: `{"bound": null, "identity": {}}`, state: project.IdentityUnsupported},
		{name: "empty object", body: `{}`, state: project.IdentityUnsupported},
		{name: "member names are case-sensitive", body: `{"Bound": true, "Identity": {}}`, state: project.IdentityUnsupported},
		{name: "white space around the body", body: " \n{\"bound\":false}\n", state: project.IdentityUnbound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(tc.body))))

			got, err := c.FetchProjectIdentity(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.state, got.State)
			if tc.state != project.IdentityBound {
				require.Nil(t, got.Document)
			}
		})
	}
}

func TestFetchProjectIdentityInvalidResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "not JSON", body: "ok"},
		{name: "empty", body: ""},
		{name: "not an object", body: `[{"bound": true}]`},
		{name: "null", body: `null`},
		{name: "bound is a string", body: `{"bound":"yes"}`},
		{name: "bound is a number", body: `{"bound": 1}`},
		{name: "identity is a string", body: `{"bound": true, "identity": "doc"}`},
		{name: "identity is an array", body: `{"bound": true, "identity": []}`},
		{name: "trailing data", body: `{"bound": false} {}`},
		{name: "repeated bound", body: `{"bound": false, "bound": true}`},
		{name: "invalid UTF-8", body: "{\"bound\": false, \"x\": \"\xff\"}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(tc.body))))

			got, err := c.FetchProjectIdentity(context.Background())
			require.Equal(t, project.RemoteIdentity{}, got)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.Equal(t, "fetch_project_identity", re.Op)
			require.Equal(t, http.MethodGet, re.Method)
			require.Equal(t, "https://acme.example.com"+identityURI, re.URL)
			require.True(t, strings.HasPrefix(re.Message, "invalid project-identity response: "), re.Message)
			require.True(t, strings.HasPrefix(err.Error(), "Protocol error: invalid project-identity response: "), err.Error())
		})
	}
}

func TestFetchProjectIdentityTooLarge(t *testing.T) {
	body := `{"bound": false, "pad": "` + strings.Repeat("x", 1<<20) + `"}`
	c, _ := newClient(t, recordingHandler(new(seen), respond(http.StatusOK, []byte(body))))
	_, err := c.FetchProjectIdentity(context.Background())
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.Contains(t, re.Message, "larger than")
}

func TestFetchProjectIdentityReadFailure(t *testing.T) {
	body := &trackingBody{r: strings.NewReader(`{"bound":`), err: errSentinel}
	c, _ := newClientWithDoer(t, bodyDoer(http.StatusOK, body))

	_, err := c.FetchProjectIdentity(context.Background())
	requireRemoteError(t, err, remote.KindProtocol)
	require.ErrorIs(t, err, errSentinel)
	require.True(t, body.closed)
}

func TestFetchProjectIdentityStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		state  project.IdentityState
		kind   remote.Kind // 0 means no error
		auth   bool
	}{
		{status: http.StatusNotFound, state: project.IdentityUnsupported},
		{status: http.StatusMethodNotAllowed, state: project.IdentityUnsupported},
		{status: http.StatusNotImplemented, state: project.IdentityUnsupported},
		{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, auth: true},
		{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, auth: true},
		{status: http.StatusInternalServerError, kind: remote.KindHTTP},
		{status: http.StatusCreated, kind: remote.KindHTTP},
		{status: http.StatusConflict, kind: remote.KindHTTP},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c, _ := newClient(t, recordingHandler(new(seen), respond(tc.status, []byte("nope"))))

			got, err := c.FetchProjectIdentity(context.Background())
			if tc.kind == 0 {
				require.NoError(t, err)
				require.Equal(t, project.RemoteIdentity{State: tc.state}, got)
				return
			}
			require.Equal(t, project.RemoteIdentity{}, got)
			re := requireRemoteError(t, err, tc.kind)
			require.Equal(t, "fetch_project_identity", re.Op)
			require.Equal(t, tc.status, re.StatusCode)
			require.Equal(t, "nope", re.Message)
			require.Equal(t, tc.auth, re.IsAuth())
		})
	}
}

func TestBindProjectIdentityRequest(t *testing.T) {
	var got seen
	c, _ := newClient(t, recordingHandler(&got, respond(http.StatusCreated, nil)))

	// Odd spacing and member order, to show the bytes are sent as given.
	doc := []byte("{ \"b\":1,\n  \"a\" : [ true ] }")
	res, err := c.BindProjectIdentity(context.Background(), doc)
	require.NoError(t, err)
	require.Equal(t, project.BindResult{Outcome: project.BindBound}, res)

	require.Equal(t, 1, got.calls)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, identityURI, got.requestURI)
	require.Equal(t, "application/json", got.header.Get("Content-Type"))
	require.Empty(t, got.header.Values("Atomic-Protocol"))
	require.Equal(t, doc, got.body)
	require.Equal(t, int64(len(doc)), got.contentLength)
}

func TestBindProjectIdentityRefusesInvalidJSON(t *testing.T) {
	for _, doc := range []string{"", "{", `{"a":1} x`, "not json"} {
		t.Run(doc, func(t *testing.T) {
			var got seen
			c, _ := newClient(t, recordingHandler(&got, respond(http.StatusOK, nil)))

			res, err := c.BindProjectIdentity(context.Background(), json.RawMessage(doc))
			require.Equal(t, project.BindResult{}, res)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.Equal(t, "bind_project_identity", re.Op)
			require.Equal(t, http.MethodPost, re.Method)
			require.Contains(t, re.Message, "identity is not valid JSON")
			require.Zero(t, got.calls, "nothing is sent")
		})
	}
}

func TestBindProjectIdentityStatuses(t *testing.T) {
	for _, tc := range []struct {
		status  int
		outcome project.BindOutcome
		kind    remote.Kind // 0 means no error
		auth    bool
	}{
		{status: http.StatusOK, outcome: project.BindBound},
		{status: http.StatusCreated, outcome: project.BindBound},
		{status: http.StatusNotFound, outcome: project.BindUnsupported},
		{status: http.StatusMethodNotAllowed, outcome: project.BindUnsupported},
		{status: http.StatusNotImplemented, outcome: project.BindUnsupported},
		{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, auth: true},
		{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, auth: true},
		{status: http.StatusInternalServerError, kind: remote.KindHTTP},
		{status: http.StatusBadRequest, kind: remote.KindHTTP},
		{status: http.StatusAccepted, kind: remote.KindHTTP},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var got []exchange
			c, _ := newClient(t, sequenceHandler(&got, respond(tc.status, []byte("nope"))))

			res, err := c.BindProjectIdentity(context.Background(), []byte(boundDocument))
			require.Len(t, got, 1, "only a 409 sends a second request")
			if tc.kind == 0 {
				require.NoError(t, err)
				require.Equal(t, project.BindResult{Outcome: tc.outcome}, res)
				return
			}
			require.Equal(t, project.BindResult{}, res)
			re := requireRemoteError(t, err, tc.kind)
			require.Equal(t, "bind_project_identity", re.Op)
			require.Equal(t, http.MethodPost, re.Method)
			require.Equal(t, "https://acme.example.com"+identityURI, re.URL)
			require.Equal(t, tc.status, re.StatusCode)
			require.Equal(t, "nope", re.Message)
			require.Equal(t, tc.auth, re.IsAuth())
		})
	}
}

func TestBindProjectIdentityConflict(t *testing.T) {
	for _, tc := range []struct {
		name        string
		conflict    string
		followUp    func(http.ResponseWriter)
		wantCode    string
		wantMessage string
		wantRemote  project.RemoteIdentity
	}{
		{
			name:       "nested, then unbound",
			conflict:   `{"error": {"code": "IDENTITY_CONFLICT"}}`,
			followUp:   respond(http.StatusOK, []byte(`{"bound": false}`)),
			wantCode:   "IDENTITY_CONFLICT",
			wantRemote: project.RemoteIdentity{State: project.IdentityUnbound},
		},
		{
			name:        "flat, then unbound",
			conflict:    `{"code": "IDENTITY_CONFLICT", "message": "project already bound"}`,
			followUp:    respond(http.StatusOK, []byte(`{"bound": false}`)),
			wantCode:    "IDENTITY_CONFLICT",
			wantMessage: "project already bound",
			wantRemote:  project.RemoteIdentity{State: project.IdentityUnbound},
		},
		{
			name:        "flat, then bound",
			conflict:    `{"code": "IDENTITY_CONFLICT", "message": "project already bound"}`,
			followUp:    respond(http.StatusOK, []byte(boundBody)),
			wantCode:    "IDENTITY_CONFLICT",
			wantMessage: "project already bound",
			wantRemote:  project.RemoteIdentity{State: project.IdentityBound, Document: json.RawMessage(boundDocument)},
		},
		{
			name:        "a code that is not a string",
			conflict:    `{"code":5,"message":"m"}`,
			followUp:    respond(http.StatusOK, []byte(`{"bound": false}`)),
			wantMessage: "m",
			wantRemote:  project.RemoteIdentity{State: project.IdentityUnbound},
		},
		{
			name:       "a follow-up 500 is unknown",
			conflict:   `{"code": "IDENTITY_CONFLICT"}`,
			followUp:   respond(http.StatusInternalServerError, []byte("boom")),
			wantCode:   "IDENTITY_CONFLICT",
			wantRemote: project.RemoteIdentity{State: project.IdentityUnknown},
		},
		{
			name:       "a follow-up that is unsupported is unknown",
			conflict:   `{"code": "IDENTITY_CONFLICT"}`,
			followUp:   respond(http.StatusNotFound, nil),
			wantCode:   "IDENTITY_CONFLICT",
			wantRemote: project.RemoteIdentity{State: project.IdentityUnknown},
		},
		{
			name:       "a follow-up that does not decode is unknown",
			conflict:   `{"code": "IDENTITY_CONFLICT"}`,
			followUp:   respond(http.StatusOK, []byte(`{"bound":"yes"}`)),
			wantCode:   "IDENTITY_CONFLICT",
			wantRemote: project.RemoteIdentity{State: project.IdentityUnknown},
		},
		{
			name:       "a body that is not JSON",
			conflict:   "already bound",
			followUp:   respond(http.StatusOK, []byte(`{"bound": false}`)),
			wantRemote: project.RemoteIdentity{State: project.IdentityUnbound},
		},
		{
			name:       "a JSON value that is not an object",
			conflict:   `["IDENTITY_CONFLICT"]`,
			followUp:   respond(http.StatusOK, []byte(`{"bound": false}`)),
			wantRemote: project.RemoteIdentity{State: project.IdentityUnbound},
		},
		{
			name:       "an error member that is not an object hides the flat members",
			conflict:   `{"error": "x", "code": "IDENTITY_CONFLICT"}`,
			followUp:   respond(http.StatusOK, []byte(`{"bound": false}`)),
			wantRemote: project.RemoteIdentity{State: project.IdentityUnbound},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []exchange
			c, _ := newClient(t, sequenceHandler(&got,
				respond(http.StatusConflict, []byte(tc.conflict), "Content-Type", "application/json"),
				tc.followUp,
			))

			doc := []byte(`{"atomic:projectId": "B"}`)
			res, err := c.BindProjectIdentity(context.Background(), doc)
			require.NoError(t, err)
			require.Equal(t, project.BindConflict, res.Outcome)
			require.Equal(t, tc.wantCode, res.ServerCode)
			require.Equal(t, tc.wantMessage, res.ServerMessage)
			require.Equal(t, tc.wantRemote.State, res.Remote.State)
			require.Equal(t, string(tc.wantRemote.Document), string(res.Remote.Document))

			require.Len(t, got, 2)
			require.Equal(t, http.MethodPost, got[0].method)
			require.Equal(t, identityURI, got[0].requestURI)
			require.Equal(t, doc, got[0].body)
			require.Empty(t, got[0].header.Values("Atomic-Protocol"))
			require.Equal(t, http.MethodGet, got[1].method)
			require.Equal(t, identityURI, got[1].requestURI)
			require.Empty(t, got[1].body)
			require.Empty(t, got[1].header.Values("Atomic-Protocol"))
		})
	}
}

func TestBindProjectIdentityConnectionFailure(t *testing.T) {
	c, _ := newClientWithDoer(t, &http.Client{Transport: roundTripError(errSentinel)})
	_, err := c.BindProjectIdentity(context.Background(), []byte(`{}`))
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.Equal(t, "bind_project_identity", re.Op)
}

func TestIdentityEnumNames(t *testing.T) {
	require.Equal(t, "Unknown", project.IdentityUnknown.String())
	require.Equal(t, "Unsupported", project.IdentityUnsupported.String())
	require.Equal(t, "Unbound", project.IdentityUnbound.String())
	require.Equal(t, "Bound", project.IdentityBound.String())
	require.Equal(t, "IdentityState(9)", project.IdentityState(9).String())

	require.Equal(t, 1, int(project.BindBound))
	require.Equal(t, "Bound", project.BindBound.String())
	require.Equal(t, "Unsupported", project.BindUnsupported.String())
	require.Equal(t, "Conflict", project.BindConflict.String())
	require.Equal(t, "BindOutcome(0)", project.BindOutcome(0).String())
}
