package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
)

// The Rust client's names for the writes, used as [remote.Error.Op].
const (
	opUploadChange         = "upload_change"
	opUploadChangeStreamed = "upload_change_streamed"
	opStoreChange          = "store_change"
	opPutViewManifest      = "put_view_manifest"
	opUploadTag            = "upload_tag"
	opForkView             = "fork_view"
	opUploadAttestation    = "upload_attestation"
	opUploadProvenance     = "upload_provenance"
)

// The request Content-Types of the writes
// (docs/remote/transport.md#methods-and-content-types).
const (
	contentTypeBytes = "application/octet-stream"
	contentTypeText  = "text/plain"
)

// minHashToken is the fewest bytes a hash token in a server's error text
// has: fewer than a Blake3 change hash's 52 base32 characters, and enough
// to keep ordinary words out.
const minHashToken = 40

// UploadChange uploads the change file data, whose hash is hash, and
// inserts it into view (docs/remote/endpoints/project.md#upload_change).
//
// The request is a POST of the project URL with the query
// insert={hash}&view={view}, the Content-Type application/octet-stream and
// data as the body. Only 200 succeeds, and its body is ignored.
//
// 404 is a [remote.KindRepositoryNotFound]. A 400 or 500 whose body holds
// both "missing" and "dependenc", matched case-sensitively, and at least
// one hash token is a [remote.KindMissingDependencies] whose MissingHashes
// are every token [HashTokens] finds in the body, the dependent change's
// as well as the missing one's; without a token it is a [remote.KindHTTP].
// The other failures are those every write shares; see [Client.ForkView].
func (c *Client) UploadChange(ctx context.Context, hash, view string, data []byte) error {
	return c.send(ctx, write{
		op:          opUploadChange,
		build:       insertQuery(hash, view),
		contentType: contentTypeBytes,
		body:        data,
		refuse:      refuseUploadChange,
	})
}

// UploadChangeFrom uploads a change file of exactly size bytes read from r,
// whose hash is hash, and inserts it into view: the request
// [Client.UploadChange] sends, with the body streamed from r rather than
// held in memory (docs/remote/endpoints/project.md#upload_change_streamed).
// Its failures are UploadChange's.
//
// The Content-Length is size, and r is read once, as the body is sent; it
// is never closed. r is never read past size+1 bytes. The send fails with a
// [remote.KindConnectionFailed] when r holds fewer than size bytes, and
// when it holds more, which the extra byte read after the first size
// reveals; a server that has already answered once it took the first size
// bytes may make that a success instead. A size of 0 sends no body and does
// not read r. A nil r or a negative size is a [remote.KindOther], and
// nothing is sent. net/http may still be reading r after UploadChangeFrom
// returns, when the server answers before the upload ends, so do not reuse
// or close r afterwards on the assumption that the upload has stopped
// reading it.
func (c *Client) UploadChangeFrom(ctx context.Context, hash, view string, r io.Reader, size int64) error {
	if r == nil {
		return &remote.Error{
			Kind:    remote.KindOther,
			Op:      opUploadChangeStreamed,
			Method:  http.MethodPost,
			URL:     c.url.String(),
			Message: "legacy: UploadChangeFrom: nil io.Reader",
		}
	}
	return c.send(ctx, write{
		op:           opUploadChangeStreamed,
		build:        insertQuery(hash, view),
		contentType:  contentTypeBytes,
		stream:       &exactReader{r: r, left: size},
		streamLength: size,
		refuse:       refuseUploadChange,
	})
}

// errStreamTooLong is the cause of a failed [Client.UploadChangeFrom] whose
// reader holds more bytes than its size.
var errStreamTooLong = errors.New("legacy: the reader holds more than size bytes")

// exactReader reads the left bytes r should hold, and then reads one more
// byte to check that r is done, failing with errStreamTooLong if it is
// not. net/http reads a body past its Content-Length to the end, into
// io.Discard, before it reports one that is too long; without this, an
// endless reader would be read until the request's context ended.
type exactReader struct {
	r    io.Reader
	left int64
}

func (e *exactReader) Read(p []byte) (int, error) {
	if e.left <= 0 {
		var probe [1]byte
		switch _, err := io.ReadFull(e.r, probe[:]); err {
		case nil:
			return 0, errStreamTooLong
		case io.EOF:
			return 0, io.EOF
		default:
			return 0, err
		}
	}
	if int64(len(p)) > e.left {
		p = p[:e.left]
	}
	n, err := e.r.Read(p)
	e.left -= int64(n)
	return n, err
}

// insertQuery builds the query insert={hash}&view={view}.
func insertQuery(hash, view string) func(*wire.Query) error {
	return func(q *wire.Query) error {
		if err := q.Add("insert", hash); err != nil {
			return err
		}
		return q.Add("view", view)
	}
}

// refuseUploadChange classifies a failed upload_change or
// upload_change_streamed.
func refuseUploadChange(f failure) *remote.Error {
	switch f.resp.StatusCode {
	case http.StatusNotFound:
		return f.err(remote.KindRepositoryNotFound)
	case http.StatusBadRequest, http.StatusInternalServerError:
		if strings.Contains(f.body, "missing") && strings.Contains(f.body, "dependenc") {
			return f.missingDependencies()
		}
	}
	return nil
}

// StoreChange stores the change file data, whose hash is hash, without
// inserting it into any view
// (docs/remote/endpoints/project.md#store_change). The Rust client's doc
// comment says the server stores it content-addressed and answers 200 when
// it already has it; [Client.PutViewManifest] then declares the views it
// belongs to.
//
// The request is a POST of the project URL with the query store={hash},
// the Content-Type application/octet-stream and data as the body. Only 200
// succeeds, and its body is ignored. 404 is a
// [remote.KindRepositoryNotFound]. Unlike [Client.UploadChange], nothing
// reads the text of a 400 or 500: both are a [remote.KindHTTP]. The other
// failures are those every write shares; see [Client.ForkView].
func (c *Client) StoreChange(ctx context.Context, hash string, data []byte) error {
	return c.send(ctx, write{
		op: opStoreChange,
		build: func(q *wire.Query) error {
			return q.Add("store", hash)
		},
		contentType: contentTypeBytes,
		body:        data,
		refuse:      refuseRepositoryNotFound,
	})
}

// PutViewManifest declares view's identity and change log with the
// manifest text manifest, sent exactly as given;
// [views.Manifest.MarshalText] writes one
// (docs/remote/endpoints/project.md#put_view_manifest).
//
// The request is a POST of the project URL with the query
// view-manifest={view}, the Content-Type text/plain and manifest as the
// body. Only 200 succeeds, and its body is ignored.
//
// 404 is a [remote.KindRepositoryNotFound]. 409 is a [remote.KindProtocol]
// whose Message is the body; the Rust client's comment names divergence,
// an identity mismatch and a state mismatch as its causes. A 400 whose
// body holds both "missing" and "change", matched case-sensitively, and at
// least one hash token is a [remote.KindMissingDependencies] listing every
// token [HashTokens] finds; without a token it is a [remote.KindHTTP]. A
// 500 is a [remote.KindHTTP] whatever its body says, unlike
// [Client.UploadChange]'s. The other failures are those every write
// shares; see [Client.ForkView].
func (c *Client) PutViewManifest(ctx context.Context, view, manifest string) error {
	return c.send(ctx, write{
		op: opPutViewManifest,
		build: func(q *wire.Query) error {
			return q.Add("view-manifest", view)
		},
		contentType: contentTypeText,
		body:        []byte(manifest),
		refuse: func(f failure) *remote.Error {
			switch f.resp.StatusCode {
			case http.StatusNotFound:
				return f.err(remote.KindRepositoryNotFound)
			case http.StatusConflict:
				return f.protocol(f.body)
			case http.StatusBadRequest:
				if strings.Contains(f.body, "missing") && strings.Contains(f.body, "change") {
					return f.missingDependencies()
				}
			}
			return nil
		},
	})
}

// UploadTag uploads the short tag data data of the tag at the Merkle state
// state in view (docs/remote/endpoints/project.md#upload_tag). data is sent
// as given: unlike the body [Client.DownloadTag] reads, it has no length
// prefix, and none is added.
//
// The request is a POST of the project URL with the query
// tagup={state}&view={view}, the Content-Type application/octet-stream and
// data as the body. Only 200 succeeds, and its body is ignored.
//
// 404 is a [remote.KindViewNotFound] whose Subject is view, the view being
// tagged. A 400 or 500 whose body holds "Wrong state" or "state mismatch",
// matched case-sensitively, is a [remote.KindStateMismatch] whose Subject
// is state; like the Rust client, it never learns the remote's state, and
// its text reports it as "unknown". Any other 400 or 500 is a
// [remote.KindHTTP]. The other failures are those every write shares; see
// [Client.ForkView].
func (c *Client) UploadTag(ctx context.Context, state, view string, data []byte) error {
	return c.send(ctx, write{
		op: opUploadTag,
		build: func(q *wire.Query) error {
			if err := q.Add("tagup", state); err != nil {
				return err
			}
			return q.Add("view", view)
		},
		contentType: contentTypeBytes,
		body:        data,
		refuse: func(f failure) *remote.Error {
			switch f.resp.StatusCode {
			case http.StatusNotFound:
				e := f.err(remote.KindViewNotFound)
				e.Subject = view
				return e
			case http.StatusBadRequest, http.StatusInternalServerError:
				if strings.Contains(f.body, "Wrong state") || strings.Contains(f.body, "state mismatch") {
					e := f.err(remote.KindStateMismatch)
					e.Subject = state
					return e
				}
			}
			return nil
		},
	})
}

// ForkView creates the view target on the remote from the existing view
// source and returns how many changes the server reports it copied
// (docs/remote/endpoints/project.md#fork_view). The Rust client's doc
// comment says the server duplicates no data and copies the log in one
// transaction.
//
// The request is a POST of the project URL with the query
// fork_from={source}&view={target}, with no body and no Content-Type. Only
// 200 succeeds. Its body must be JSON, and changes is its "changes" member
// read as an unsigned 64-bit integer; it is 0 when the body is not an
// object, the member is absent, or its value is not an unsigned integer
// written without a fraction or an exponent. Nothing else in the body is
// read. A body that is not JSON is a [remote.KindProtocol] "Invalid fork
// response: {error}", whose text is Go's JSON error rather than the Rust
// client's, and which keeps the body, at most [remote.MaxErrorBody] bytes
// of it, in Body.
//
// 404 is a [remote.KindViewNotFound] whose Subject is source, the view
// being forked. 400 is a [remote.KindProtocol] whose Message is the body.
//
// Every write fails in these ways too. A view, hash or state that a query
// cannot carry unchanged, such as one holding '#' or '&', or one starting
// or ending with a space, is a [remote.KindOther], and nothing is sent
// (docs/remote/transport.md#building-request-urls). 401 and 403 are
// [remote.KindAuthenticationFailed], and any status a write does not name
// is a [remote.KindHTTP], both carrying the body as text
// (docs/remote/errors.md#project-protocol-failures). Every error made from
// a failed status keeps the body, at most [remote.MaxErrorBody] bytes of
// it, in Body. A request that cannot be sent is a
// [remote.KindConnectionFailed]; so is a fork body that cannot be read,
// and one larger than 2 GiB is a [remote.KindProtocol].
func (c *Client) ForkView(ctx context.Context, source, target string) (changes uint64, err error) {
	err = c.send(ctx, write{
		op: opForkView,
		build: func(q *wire.Query) error {
			if err := q.Add("fork_from", source); err != nil {
				return err
			}
			return q.Add("view", target)
		},
		ok: func(r response) error {
			changes, err = parseForkResponse(r.data)
			if err != nil {
				e := r.protocol("Invalid fork response: "+err.Error(), err)
				e.Body = string(r.data[:min(len(r.data), remote.MaxErrorBody)])
				return e
			}
			return nil
		},
		refuse: func(f failure) *remote.Error {
			switch f.resp.StatusCode {
			case http.StatusNotFound:
				e := f.err(remote.KindViewNotFound)
				e.Subject = source
				return e
			case http.StatusBadRequest:
				return f.protocol(f.body)
			}
			return nil
		},
	})
	if err != nil {
		return 0, err
	}
	return changes, nil
}

// parseForkResponse reads the changes count of a fork_view body as the
// Rust client does: any JSON value parses, and the count is its "changes"
// member when that is an unsigned 64-bit integer, or 0.
func parseForkResponse(body []byte) (uint64, error) {
	var v json.RawMessage
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, err
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(v, &obj) != nil {
		// Valid JSON, but not an object.
		return 0, nil
	}
	changes, err := strconv.ParseUint(string(obj["changes"]), 10, 64)
	if err != nil {
		return 0, nil
	}
	return changes, nil
}

// UploadAttestation uploads the serialized attestation data, whose hash is
// hash (docs/remote/endpoints/project.md#upload_attestation).
//
// The request is a POST of the project URL with the query attest={hash},
// the Content-Type application/octet-stream and data as the body. Only 200
// succeeds, and its body is ignored. 404 is a
// [remote.KindRepositoryNotFound], and 400 a [remote.KindProtocol] "Failed
// to upload attestation: {body}". The other failures are those every write
// shares; see [Client.ForkView].
func (c *Client) UploadAttestation(ctx context.Context, hash string, data []byte) error {
	return c.uploadObject(ctx, opUploadAttestation, "attest", hash, data, "Failed to upload attestation: ")
}

// UploadProvenance uploads the serialized provenance graph data, whose
// hash is hash (docs/remote/endpoints/project.md#upload_provenance).
//
// The request is a POST of the project URL with the query
// provenance={hash}, the Content-Type application/octet-stream and data as
// the body. Only 200 succeeds, and its body is ignored. 404 is a
// [remote.KindRepositoryNotFound], and 400 a [remote.KindProtocol] "Failed
// to upload provenance graph: {body}". The other failures are those every
// write shares; see [Client.ForkView].
func (c *Client) UploadProvenance(ctx context.Context, hash string, data []byte) error {
	return c.uploadObject(ctx, opUploadProvenance, "provenance", hash, data, "Failed to upload provenance graph: ")
}

// uploadObject POSTs data with the query {key}={hash}. A 404 is a
// [remote.KindRepositoryNotFound] and a 400 a [remote.KindProtocol] whose
// message is refused and then the body.
func (c *Client) uploadObject(ctx context.Context, op, key, hash string, data []byte, refused string) error {
	return c.send(ctx, write{
		op: op,
		build: func(q *wire.Query) error {
			return q.Add(key, hash)
		},
		contentType: contentTypeBytes,
		body:        data,
		refuse: func(f failure) *remote.Error {
			switch f.resp.StatusCode {
			case http.StatusNotFound:
				return f.err(remote.KindRepositoryNotFound)
			case http.StatusBadRequest:
				return f.protocol(refused + f.body)
			}
			return nil
		},
	})
}

// HashTokens returns every hash token in msg, a server's error text, in
// the order they appear, or nil when it holds none
// (docs/remote/errors.md#server-text-the-client-recognises).
//
// As in the Rust client, msg is split at every byte that is not an ASCII
// upper-case letter or digit, and each piece of at least 40 bytes made only
// of the base32 alphabet, A to Z and 2 to 7, is a token. A piece holding a
// 0, 1, 8 or 9 is dropped whole, never split into shorter runs. A change
// hash is 52 such characters; the 40-byte minimum keeps ordinary words
// out.
func HashTokens(msg string) []string {
	var tokens []string
	for _, piece := range strings.FieldsFunc(msg, func(r rune) bool {
		return !('A' <= r && r <= 'Z' || '0' <= r && r <= '9')
	}) {
		if len(piece) >= minHashToken && !strings.ContainsAny(piece, "0189") {
			tokens = append(tokens, piece)
		}
	}
	return tokens
}

// write is one write request.
type write struct {
	op    string
	build func(*wire.Query) error

	// contentType is the request's Content-Type, or "" for none.
	contentType string

	// body, or stream and streamLength, are the request body; see
	// [wire.Request].
	body         []byte
	stream       io.Reader
	streamLength int64

	// ok reads a 200 response's body; nil ignores it.
	ok func(response) error

	// refuse classifies a failed status other than 401 and 403, and
	// returns nil to leave it a [remote.KindHTTP].
	refuse func(failure) *remote.Error
}

// failure is a write's failed response, with its body read as text.
type failure struct {
	op   string
	resp *wire.Response
	body string
}

// err returns an error of kind for f, keeping its body.
func (f failure) err(kind remote.Kind) *remote.Error {
	return &remote.Error{
		Kind:             kind,
		Op:               f.op,
		Method:           http.MethodPost,
		URL:              f.resp.URL,
		StatusCode:       f.resp.StatusCode,
		Body:             f.body,
		ServerMinVersion: f.resp.MinVersion,
	}
}

// protocol returns the [remote.KindProtocol] for f with message msg.
func (f failure) protocol(msg string) *remote.Error {
	e := f.err(remote.KindProtocol)
	e.Message = msg
	return e
}

// missingDependencies returns the [remote.KindMissingDependencies] listing
// the hash tokens of f's body, or nil when it has none.
func (f failure) missingDependencies() *remote.Error {
	tokens := HashTokens(f.body)
	if len(tokens) == 0 {
		return nil
	}
	e := f.err(remote.KindMissingDependencies)
	e.MissingHashes = tokens
	return e
}

// refuseRepositoryNotFound makes a 404 a [remote.KindRepositoryNotFound].
func refuseRepositoryNotFound(f failure) *remote.Error {
	if f.resp.StatusCode == http.StatusNotFound {
		return f.err(remote.KindRepositoryNotFound)
	}
	return nil
}

// send POSTs w to the project URL with the query w.build writes.
func (c *Client) send(ctx context.Context, w write) error {
	u, err := c.queryURL(w.op, http.MethodPost, w.build)
	if err != nil {
		return err
	}
	var header http.Header
	if w.contentType != "" {
		header = http.Header{"Content-Type": {w.contentType}}
	}
	resp, err := c.requester.Do(ctx, wire.Request{
		Op:           w.op,
		Method:       http.MethodPost,
		URL:          u,
		Header:       header,
		Body:         w.body,
		Stream:       w.stream,
		StreamLength: w.streamLength,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		if w.ok == nil {
			return nil
		}
		r, err := c.readBody(ctx, response{op: w.op, method: http.MethodPost, resp: resp})
		if err != nil {
			return err
		}
		return w.ok(r)
	}

	body := wire.ErrorBody(resp)
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden && w.refuse != nil {
		if e := w.refuse(failure{op: w.op, resp: resp, body: body}); e != nil {
			return e
		}
	}
	return wire.StatusError(w.op, resp, body)
}
