package legacy_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project/legacy"
	"github.com/z5labs/atomic-go/remote/remotetest"
)

// The two hashes of the body in
// docs/remote/errors.md#server-text-the-client-recognises: the dependent
// change and the change it depends on.
const (
	dependentHash = "GJGC6OK6JT77OB2PDGFYEC2BUISJWQNQVT345SNIZ2CKKXGV652A"
	missingHash   = "FUGMXMFD7FJ6QARZ4RML2W5W6W3RGE2K7ILTZ55XKQ4Z3XZ4YQ4A"
)

// manifestMissingBody is the server text the Rust client's test calls "the
// exact shape apply_view_manifest produces".
const manifestMissingBody = "missing changes: Manifest for view 'orange-night-44fb': change " + dependentHash +
	" depends on " + missingHash + ", which is neither earlier in the log nor present locally"

// uploadMissingBody holds both of upload_change's trigger words and a hash.
const uploadMissingBody = "missing dependency " + missingHash + " for change " + dependentHash

func TestHashTokens(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		want []string
	}{
		{name: "the documented server body", msg: manifestMissingBody, want: []string{dependentHash, missingHash}},
		{name: "words only", msg: "missing changes not present locally on the REMOTE server"},
		{name: "a run of zeros", msg: "error 00000000000000000000000000000000000000000000000000"},
		{
			name: "a run holding a 1 beside a hash",
			msg:  strings.Repeat("A", 44) + "1 " + missingHash,
			want: []string{missingHash},
		},
		{name: "a run holding an 8 is not split", msg: strings.Repeat("A", 40) + "8" + strings.Repeat("B", 40)},
		{name: "a run holding a 9 is not split", msg: strings.Repeat("A", 40) + "9" + strings.Repeat("B", 40)},
		{name: "39 bytes", msg: strings.Repeat("A", 39)},
		{name: "40 bytes", msg: strings.Repeat("A", 40), want: []string{strings.Repeat("A", 40)}},
		{name: "lower case splits a run", msg: strings.Repeat("A", 30) + "a" + strings.Repeat("B", 30)},
		{name: "punctuation ends a token", msg: "(" + missingHash + ")," + dependentHash + ".", want: []string{missingHash, dependentHash}},
		{name: "non-ASCII ends a token", msg: missingHash + "é" + dependentHash, want: []string{missingHash, dependentHash}},
		{name: "empty", msg: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, legacy.HashTokens(tc.msg))
		})
	}
}

// countingReader counts the bytes read from it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestUploadRequests(t *testing.T) {
	for _, tc := range []struct {
		name        string
		requestURI  string
		contentType string
		body        string
		do          func(c *legacy.Client) error
	}{
		{
			name:        "UploadChange",
			requestURI:  codePath + "?insert=HASH&view=dev",
			contentType: "application/octet-stream",
			body:        "change\x00bytes",
			do: func(c *legacy.Client) error {
				return c.UploadChange(context.Background(), "HASH", "dev", []byte("change\x00bytes"))
			},
		},
		{
			name:        "StoreChange",
			requestURI:  codePath + "?store=HASH",
			contentType: "application/octet-stream",
			body:        "change bytes",
			do: func(c *legacy.Client) error {
				return c.StoreChange(context.Background(), "HASH", []byte("change bytes"))
			},
		},
		{
			name:        "PutViewManifest",
			requestURI:  codePath + "?view-manifest=dev",
			contentType: "text/plain",
			body:        "manifest\ttext\n",
			do: func(c *legacy.Client) error {
				return c.PutViewManifest(context.Background(), "dev", "manifest\ttext\n")
			},
		},
		{
			name:        "UploadTag",
			requestURI:  codePath + "?tagup=STATE&view=dev",
			contentType: "application/octet-stream",
			body:        "short tag",
			do: func(c *legacy.Client) error {
				return c.UploadTag(context.Background(), "STATE", "dev", []byte("short tag"))
			},
		},
		{
			name:        "UploadAttestation",
			requestURI:  codePath + "?attest=HASH",
			contentType: "application/octet-stream",
			body:        "attestation",
			do: func(c *legacy.Client) error {
				return c.UploadAttestation(context.Background(), "HASH", []byte("attestation"))
			},
		},
		{
			name:        "UploadProvenance",
			requestURI:  codePath + "?provenance=HASH",
			contentType: "application/octet-stream",
			body:        "provenance",
			do: func(c *legacy.Client) error {
				return c.UploadProvenance(context.Background(), "HASH", []byte("provenance"))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := strings.TrimPrefix(tc.requestURI, codePath+"?")
			c, s := serve(t, map[string]reply{query: {body: "ignored"}})

			require.NoError(t, tc.do(c))
			require.Len(t, s.seen, 1)
			seen := s.seen[0]
			require.Equal(t, http.MethodPost, seen.method)
			require.Equal(t, tc.requestURI, seen.requestURI)
			require.Equal(t, []string{tc.contentType}, seen.header.Values("Content-Type"))
			require.Equal(t, int64(len(tc.body)), seen.contentLength)
			require.Equal(t, tc.body, string(seen.body))
			require.Equal(t, []string{"atomic-" + clientVersion}, seen.header.Values("User-Agent"))
			require.Equal(t, []string{"zstd, gzip, deflate"}, seen.header.Values("Accept-Encoding"))
		})
	}
}

func TestUploadTagSendsTheDataWithoutALengthPrefix(t *testing.T) {
	c, s := serve(t, map[string]reply{"tagup=STATE&view=dev": {}})
	require.NoError(t, c.UploadTag(context.Background(), "STATE", "dev", []byte("tag")))
	require.Len(t, s.seen, 1)
	require.Equal(t, []byte("tag"), s.seen[0].body)
}

func TestUploadChangeFrom(t *testing.T) {
	const data = "streamed change bytes"
	c, s := serve(t, map[string]reply{"insert=HASH&view=dev": {}})
	r := &countingReader{r: strings.NewReader(data)}

	require.NoError(t, c.UploadChangeFrom(context.Background(), "HASH", "dev", r, int64(len(data))))
	require.Equal(t, len(data), r.n)
	require.Len(t, s.seen, 1)
	seen := s.seen[0]
	require.Equal(t, http.MethodPost, seen.method)
	require.Equal(t, codePath+"?insert=HASH&view=dev", seen.requestURI)
	require.Equal(t, []string{"application/octet-stream"}, seen.header.Values("Content-Type"))
	require.Equal(t, int64(len(data)), seen.contentLength)
	require.Empty(t, seen.header.Values("Transfer-Encoding"))
	require.Equal(t, data, string(seen.body))
}

func TestUploadChangeFromAShortReader(t *testing.T) {
	c, s := serve(t, map[string]reply{"insert=HASH&view=dev": {}})
	err := c.UploadChangeFrom(context.Background(), "HASH", "dev", strings.NewReader("short"), 10)
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.Equal(t, "upload_change_streamed", re.Op)
	require.Empty(t, s.seen)
}

func TestUploadChangeFromRefusesBadArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    io.Reader
		size int64
	}{
		{name: "nil reader", r: nil, size: 4},
		{name: "negative size", r: strings.NewReader("data"), size: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := serve(t, nil)
			err := c.UploadChangeFrom(context.Background(), "HASH", "dev", tc.r, tc.size)
			re := requireRemoteError(t, err, remote.KindOther)
			require.Equal(t, "upload_change_streamed", re.Op)
			require.Equal(t, http.MethodPost, re.Method)
			require.Empty(t, s.seen)
		})
	}
}

func TestForkView(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want uint64
	}{
		{name: "a count", body: `{"changes":12,"view":"feature"}`, want: 12},
		{name: "the largest count", body: `{"changes":18446744073709551615}`, want: 18446744073709551615},
		{name: "white space around", body: " \n{\"changes\": 3}\n", want: 3},
		{name: "no changes member", body: `{"view":"feature"}`},
		{name: "a string count", body: `{"changes":"12"}`},
		{name: "a negative count", body: `{"changes":-1}`},
		{name: "a fraction", body: `{"changes":1.5}`},
		{name: "an exponent", body: `{"changes":1e2}`},
		{name: "past 64 bits", body: `{"changes":18446744073709551616}`},
		{name: "null count", body: `{"changes":null}`},
		{name: "an array", body: `[1,2,3]`},
		{name: "a number", body: `7`},
		{name: "null", body: `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := serve(t, map[string]reply{"fork_from=dev&view=feature": {body: tc.body}})

			got, err := c.ForkView(context.Background(), "dev", "feature")
			require.NoError(t, err)
			require.Equal(t, tc.want, got)

			require.Len(t, s.seen, 1)
			seen := s.seen[0]
			require.Equal(t, http.MethodPost, seen.method)
			require.Equal(t, codePath+"?fork_from=dev&view=feature", seen.requestURI)
			require.Empty(t, seen.header.Values("Content-Type"))
			require.Empty(t, seen.body)
			require.Zero(t, seen.contentLength)
		})
	}
}

func TestForkViewBodiesThatAreNotJSON(t *testing.T) {
	for _, body := range []string{"", "forked", `{"changes":`, `{"changes":1} trailing`} {
		t.Run(body, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{"fork_from=dev&view=feature": {body: body}})

			got, err := c.ForkView(context.Background(), "dev", "feature")
			require.Zero(t, got)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.True(t, strings.HasPrefix(re.Message, "Invalid fork response: "), re.Message)
			require.Equal(t, "fork_view", re.Op)
			require.Equal(t, http.MethodPost, re.Method)
			require.Equal(t, http.StatusOK, re.StatusCode)
			require.Error(t, re.Err)
		})
	}
}

func TestForkViewBodyTooLarge(t *testing.T) {
	s := &server{routes: map[string]reply{"fork_from=dev&view=feature": {body: `{"changes":12}`}}}
	c := newClientWithMaxBody(t, s, 5)
	_, err := c.ForkView(context.Background(), "dev", "feature")
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.Equal(t, "Protocol error: response body is larger than 5 bytes", re.Error())
	require.Equal(t, http.MethodPost, re.Method)
}

// write is one write, made against c, reduced to its error.
type write struct {
	name  string
	op    string
	query string
	do    func(c *legacy.Client) error
}

// writes is every write, each sending to view "dev", state "STATE" or hash
// "HASH"; ForkView forks "dev" into "feature".
var writes = []write{
	{name: "UploadChange", op: "upload_change", query: "insert=HASH&view=dev", do: func(c *legacy.Client) error {
		return c.UploadChange(context.Background(), "HASH", "dev", []byte("data"))
	}},
	{name: "UploadChangeFrom", op: "upload_change_streamed", query: "insert=HASH&view=dev", do: func(c *legacy.Client) error {
		return c.UploadChangeFrom(context.Background(), "HASH", "dev", strings.NewReader("data"), 4)
	}},
	{name: "StoreChange", op: "store_change", query: "store=HASH", do: func(c *legacy.Client) error {
		return c.StoreChange(context.Background(), "HASH", []byte("data"))
	}},
	{name: "PutViewManifest", op: "put_view_manifest", query: "view-manifest=dev", do: func(c *legacy.Client) error {
		return c.PutViewManifest(context.Background(), "dev", "manifest")
	}},
	{name: "UploadTag", op: "upload_tag", query: "tagup=STATE&view=dev", do: func(c *legacy.Client) error {
		return c.UploadTag(context.Background(), "STATE", "dev", []byte("tag"))
	}},
	{name: "ForkView", op: "fork_view", query: "fork_from=dev&view=feature", do: func(c *legacy.Client) error {
		_, err := c.ForkView(context.Background(), "dev", "feature")
		return err
	}},
	{name: "UploadAttestation", op: "upload_attestation", query: "attest=HASH", do: func(c *legacy.Client) error {
		return c.UploadAttestation(context.Background(), "HASH", []byte("data"))
	}},
	{name: "UploadProvenance", op: "upload_provenance", query: "provenance=HASH", do: func(c *legacy.Client) error {
		return c.UploadProvenance(context.Background(), "HASH", []byte("data"))
	}},
}

func writeNamed(t *testing.T, name string) write {
	t.Helper()
	for _, w := range writes {
		if w.name == name {
			return w
		}
	}
	require.FailNow(t, "no write named "+name)
	return write{}
}

// requireWriteError checks that err is a *remote.Error of kind from w,
// answered with status and keeping body, and returns it.
func requireWriteError(t *testing.T, err error, w write, kind remote.Kind, status int, body string) *remote.Error {
	t.Helper()
	re := requireRemoteError(t, err, kind)
	require.Equal(t, w.op, re.Op)
	require.Equal(t, http.MethodPost, re.Method)
	require.Equal(t, codeURL+"?"+w.query, re.URL)
	require.Equal(t, status, re.StatusCode)
	require.Equal(t, body, re.Body)
	return re
}

func TestWritesNotFound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    remote.Kind
		subject string
		msg     string
	}{
		{name: "UploadChange", kind: remote.KindRepositoryNotFound, msg: "Repository not found: " + codeURL + "?insert=HASH&view=dev"},
		{name: "UploadChangeFrom", kind: remote.KindRepositoryNotFound, msg: "Repository not found: " + codeURL + "?insert=HASH&view=dev"},
		{name: "StoreChange", kind: remote.KindRepositoryNotFound, msg: "Repository not found: " + codeURL + "?store=HASH"},
		{name: "PutViewManifest", kind: remote.KindRepositoryNotFound, msg: "Repository not found: " + codeURL + "?view-manifest=dev"},
		{name: "UploadTag", kind: remote.KindViewNotFound, subject: "dev", msg: "View not found: dev"},
		{name: "ForkView", kind: remote.KindViewNotFound, subject: "dev", msg: "View not found: dev"},
		{name: "UploadAttestation", kind: remote.KindRepositoryNotFound, msg: "Repository not found: " + codeURL + "?attest=HASH"},
		{name: "UploadProvenance", kind: remote.KindRepositoryNotFound, msg: "Repository not found: " + codeURL + "?provenance=HASH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := writeNamed(t, tc.name)
			c, _ := serve(t, map[string]reply{w.query: {status: http.StatusNotFound, body: "nope", header: []string{remote.MinVersionHeader, "0.1.0"}}})

			err := w.do(c)
			re := requireWriteError(t, err, w, tc.kind, http.StatusNotFound, "nope")
			require.EqualError(t, err, tc.msg)
			require.Equal(t, tc.subject, re.Subject)
			require.Equal(t, "0.1.0", re.ServerMinVersion)
			require.ErrorIs(t, err, remote.ErrNotFound)
		})
	}
}

func TestWritesStatuses(t *testing.T) {
	for _, w := range writes {
		for _, tc := range []struct {
			status   int
			kind     remote.Kind
			sentinel error
		}{
			{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrUnauthorized},
			{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrForbidden},
			{status: http.StatusCreated, kind: remote.KindHTTP},
			{status: http.StatusNoContent, kind: remote.KindHTTP},
			{status: http.StatusRequestEntityTooLarge, kind: remote.KindHTTP},
			{status: http.StatusBadGateway, kind: remote.KindHTTP},
		} {
			t.Run(w.name+"/"+http.StatusText(tc.status), func(t *testing.T) {
				// The body holds every trigger text, so only the status
				// can decide.
				body := "missing dependency change Wrong state " + missingHash
				if tc.status == http.StatusNoContent {
					body = ""
				}
				c, _ := serve(t, map[string]reply{w.query: {status: tc.status, body: body}})

				err := w.do(c)
				re := requireWriteError(t, err, w, tc.kind, tc.status, body)
				require.Equal(t, body, re.Message)
				if tc.sentinel != nil {
					require.ErrorIs(t, err, tc.sentinel)
				}
			})
		}
	}
}

func TestWritesBadRequestAndServerError(t *testing.T) {
	for _, tc := range []struct {
		write  string
		status int
		body   string
		kind   remote.Kind
		msg    string
		hashes []string
	}{
		// upload_change: "missing" and "dependenc" and a token, on 400 or 500.
		{write: "UploadChange", status: 400, body: uploadMissingBody, kind: remote.KindMissingDependencies, hashes: []string{missingHash, dependentHash}},
		{write: "UploadChange", status: 500, body: uploadMissingBody, kind: remote.KindMissingDependencies, hashes: []string{missingHash, dependentHash}},
		{write: "UploadChangeFrom", status: 400, body: uploadMissingBody, kind: remote.KindMissingDependencies, hashes: []string{missingHash, dependentHash}},
		{write: "UploadChangeFrom", status: 500, body: uploadMissingBody, kind: remote.KindMissingDependencies, hashes: []string{missingHash, dependentHash}},
		{write: "UploadChange", status: 400, body: "missing dependencies on the server", kind: remote.KindHTTP, msg: "missing dependencies on the server"},
		{write: "UploadChange", status: 500, body: "missing dependencies on the server", kind: remote.KindHTTP, msg: "missing dependencies on the server"},
		// The documented body says "depends", not "dependenc".
		{write: "UploadChange", status: 400, body: manifestMissingBody, kind: remote.KindHTTP, msg: manifestMissingBody},
		{write: "UploadChange", status: 400, body: "Missing dependency " + missingHash, kind: remote.KindHTTP, msg: "Missing dependency " + missingHash},
		{write: "UploadChange", status: 409, body: uploadMissingBody, kind: remote.KindHTTP, msg: uploadMissingBody},

		// store_change reads no text.
		{write: "StoreChange", status: 400, body: uploadMissingBody, kind: remote.KindHTTP, msg: uploadMissingBody},
		{write: "StoreChange", status: 500, body: uploadMissingBody, kind: remote.KindHTTP, msg: uploadMissingBody},

		// put_view_manifest: "missing" and "change" and a token, on 400 only.
		{write: "PutViewManifest", status: 400, body: manifestMissingBody, kind: remote.KindMissingDependencies, hashes: []string{dependentHash, missingHash}},
		{write: "PutViewManifest", status: 500, body: manifestMissingBody, kind: remote.KindHTTP, msg: manifestMissingBody},
		{write: "PutViewManifest", status: 400, body: "missing changes not present locally", kind: remote.KindHTTP, msg: "missing changes not present locally"},
		{write: "PutViewManifest", status: 400, body: "bad manifest", kind: remote.KindHTTP, msg: "bad manifest"},
		{write: "PutViewManifest", status: 409, body: "view diverged", kind: remote.KindProtocol, msg: "view diverged"},

		// upload_tag: "Wrong state" or "state mismatch", on 400 or 500.
		{write: "UploadTag", status: 400, body: "Wrong state for tag", kind: remote.KindStateMismatch},
		{write: "UploadTag", status: 500, body: "Wrong state for tag", kind: remote.KindStateMismatch},
		{write: "UploadTag", status: 400, body: "tag refused: state mismatch", kind: remote.KindStateMismatch},
		{write: "UploadTag", status: 500, body: "tag refused: state mismatch", kind: remote.KindStateMismatch},
		{write: "UploadTag", status: 400, body: "wrong state for tag", kind: remote.KindHTTP, msg: "wrong state for tag"},
		{write: "UploadTag", status: 400, body: "State Mismatch", kind: remote.KindHTTP, msg: "State Mismatch"},
		{write: "UploadTag", status: 409, body: "Wrong state for tag", kind: remote.KindHTTP, msg: "Wrong state for tag"},

		// fork_view: 400 is a protocol error with the body.
		{write: "ForkView", status: 400, body: "view feature exists", kind: remote.KindProtocol, msg: "view feature exists"},
		{write: "ForkView", status: 500, body: "boom", kind: remote.KindHTTP, msg: "boom"},

		// upload_attestation and upload_provenance: 400 is a protocol error.
		{write: "UploadAttestation", status: 400, body: "bad signature", kind: remote.KindProtocol, msg: "Failed to upload attestation: bad signature"},
		{write: "UploadAttestation", status: 500, body: "boom", kind: remote.KindHTTP, msg: "boom"},
		{write: "UploadProvenance", status: 400, body: "bad graph", kind: remote.KindProtocol, msg: "Failed to upload provenance graph: bad graph"},
		{write: "UploadProvenance", status: 500, body: "boom", kind: remote.KindHTTP, msg: "boom"},
	} {
		t.Run(tc.write+"/"+http.StatusText(tc.status)+"/"+tc.body, func(t *testing.T) {
			w := writeNamed(t, tc.write)
			c, _ := serve(t, map[string]reply{w.query: {status: tc.status, body: tc.body}})

			err := w.do(c)
			re := requireWriteError(t, err, w, tc.kind, tc.status, tc.body)
			require.Equal(t, tc.msg, re.Message)
			require.Equal(t, tc.hashes, re.MissingHashes)
			if tc.kind == remote.KindStateMismatch {
				require.Equal(t, "STATE", re.Subject)
				require.EqualError(t, err, "State mismatch: remote is at unknown, cannot tag STATE")
			}
		})
	}
}

func TestMissingDependenciesText(t *testing.T) {
	w := writeNamed(t, "PutViewManifest")
	c, _ := serve(t, map[string]reply{w.query: {status: http.StatusBadRequest, body: manifestMissingBody}})
	err := w.do(c)
	require.EqualError(t, err, "Missing 2 dependencies on remote: "+dependentHash+", "+missingHash)
}

func TestWritesRefuseUnsafeValuesBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   string
		do   func(c *legacy.Client) error
	}{
		{name: "UploadChange hash", op: "upload_change", do: func(c *legacy.Client) error {
			return c.UploadChange(context.Background(), "a&b", "dev", nil)
		}},
		{name: "UploadChange view", op: "upload_change", do: func(c *legacy.Client) error {
			return c.UploadChange(context.Background(), "HASH", "feat#1", nil)
		}},
		{name: "UploadChangeFrom", op: "upload_change_streamed", do: func(c *legacy.Client) error {
			return c.UploadChangeFrom(context.Background(), "HASH", "feat#1", strings.NewReader("x"), 1)
		}},
		{name: "StoreChange", op: "store_change", do: func(c *legacy.Client) error {
			return c.StoreChange(context.Background(), " HASH", nil)
		}},
		{name: "PutViewManifest", op: "put_view_manifest", do: func(c *legacy.Client) error {
			return c.PutViewManifest(context.Background(), "feat#1", "")
		}},
		{name: "UploadTag state", op: "upload_tag", do: func(c *legacy.Client) error {
			return c.UploadTag(context.Background(), "S&T", "dev", nil)
		}},
		{name: "UploadTag view", op: "upload_tag", do: func(c *legacy.Client) error {
			return c.UploadTag(context.Background(), "STATE", "feat#1", nil)
		}},
		{name: "ForkView source", op: "fork_view", do: func(c *legacy.Client) error {
			_, err := c.ForkView(context.Background(), "feat#1", "feature")
			return err
		}},
		{name: "ForkView target", op: "fork_view", do: func(c *legacy.Client) error {
			_, err := c.ForkView(context.Background(), "dev", "feat ")
			return err
		}},
		{name: "UploadAttestation", op: "upload_attestation", do: func(c *legacy.Client) error {
			return c.UploadAttestation(context.Background(), "H\nASH", nil)
		}},
		{name: "UploadProvenance", op: "upload_provenance", do: func(c *legacy.Client) error {
			return c.UploadProvenance(context.Background(), "H#ASH", nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := serve(t, nil)
			err := tc.do(c)
			re := requireRemoteError(t, err, remote.KindOther)
			require.Equal(t, tc.op, re.Op)
			require.Equal(t, http.MethodPost, re.Method)
			require.Equal(t, codeURL, re.URL)
			require.Empty(t, s.seen)
		})
	}
}

func TestWritesConnectionFailure(t *testing.T) {
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errSentinel
	})}
	c := newClientWithDoer(t, doer)
	for _, w := range writes {
		t.Run(w.name, func(t *testing.T) {
			err := w.do(c)
			re := requireRemoteError(t, err, remote.KindConnectionFailed)
			require.ErrorIs(t, err, errSentinel)
			require.Equal(t, w.op, re.Op)
			require.Equal(t, http.MethodPost, re.Method)
		})
	}
}

func TestForkViewReadFailure(t *testing.T) {
	body := &failingBody{r: strings.NewReader(`{"changes":`)}
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body, Request: req}, nil
	})}
	c := newClientWithDoer(t, doer)

	_, err := c.ForkView(context.Background(), "dev", "feature")
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.Equal(t, "fork_view", re.Op)
	require.Equal(t, http.MethodPost, re.Method)
	require.True(t, body.closed)
}

func TestUploadsIgnoreTheSuccessBody(t *testing.T) {
	for _, w := range writes {
		if w.name == "ForkView" {
			continue
		}
		t.Run(w.name, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{w.query: {body: "not JSON, not anything"}})
			require.NoError(t, w.do(c))
		})
	}
}
