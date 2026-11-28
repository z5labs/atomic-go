package legacy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
	"github.com/z5labs/atomic-go/remote/views"
	"github.com/z5labs/atomic-go/zstdframe"
)

// The Rust client's names for the reads, used as [remote.Error.Op].
const (
	opGetState            = "get_state"
	opGetChangelist       = "get_changelist"
	opGetID               = "get_id"
	opDownloadTag         = "download_tag"
	opListRemoteTags      = "list_remote_tags"
	opDownloadAttestation = "download_attestation"
	opDownloadProvenance  = "download_provenance"
	opGetProvenanceList   = "get_provenance_list"
	opListViews           = "list_views"
	opGetViewManifest     = "get_view_manifest"
)

// tagLengthPrefix is the size of the big-endian length that starts a
// download_tag body.
const tagLengthPrefix = 8

// State returns the view's current position and Merkle states
// (docs/remote/endpoints/project.md#get_state).
//
// The request is a GET of the project URL with the query
// view={view}&state=. Only 200 succeeds, and its whole body is parsed with
// [ParseState]; a body that does not parse is a [remote.KindProtocol]
// "Failed to parse state response: {error}", whose cause matches the
// parse error's sentinel.
//
// 404 is a [remote.KindViewNotFound] whose Subject is view. The other
// failures are those every read shares; see [Client.Changelist].
func (c *Client) State(ctx context.Context, view string) (State, error) {
	body, _, err := c.fetch(ctx, opGetState, viewNotFound(view), func(q *wire.Query) error {
		if err := q.Add("view", view); err != nil {
			return err
		}
		return q.Add("state", "")
	})
	if err != nil {
		return State{}, err
	}
	st, err := ParseState(string(body.data))
	if err != nil {
		return State{}, body.protocol("Failed to parse state response: "+err.Error(), err)
	}
	return st, nil
}

// Changelist returns the view's log from the sequence number from
// (docs/remote/endpoints/project.md#get_changelist). Whether the entry at
// from is included is the server's to decide.
//
// The request is a GET of the project URL with the query
// view={view}&changelist={from}. Only 200 succeeds. Its body is split into
// lines, blank lines are skipped and every other line is parsed with
// [ParseChangelistEntry]; one line that does not parse fails the whole
// response with a [remote.KindProtocol] "Failed to parse changelist entry:
// {error}". A body with no entries is a nil slice and no error.
//
// 404 is a [remote.KindViewNotFound] whose Subject is view.
//
// Every read fails in these ways too. A view, hash or state that a query
// cannot carry unchanged, such as one holding '#' or '&', or one starting
// or ending with a space, is a [remote.KindOther], and nothing is sent: the
// Rust client would send it unencoded and the server would read something
// else (docs/remote/transport.md#building-request-urls). 401 and 403 are [remote.KindAuthenticationFailed], and
// any status a read does not name is a [remote.KindHTTP], both carrying
// the body as text (docs/remote/errors.md#project-protocol-failures). A
// request that cannot be sent, or a body that cannot be read, is a
// [remote.KindConnectionFailed]. Unlike the Rust client, which reads a
// body without a limit, a body larger than 2 GiB, or one sent with a zstd
// Content-Encoding that inflates past it, is a [remote.KindProtocol].
func (c *Client) Changelist(ctx context.Context, view string, from ident.Sequence) ([]ChangelistEntry, error) {
	body, _, err := c.fetch(ctx, opGetChangelist, viewNotFound(view), func(q *wire.Query) error {
		if err := q.Add("view", view); err != nil {
			return err
		}
		return q.Add("changelist", from.String())
	})
	if err != nil {
		return nil, err
	}
	entries, err := parseChangelist(string(body.data))
	if err != nil {
		return nil, body.protocol("Failed to parse changelist entry: "+err.Error(), err)
	}
	return entries, nil
}

// ID returns the view's id: the 200 body of a GET of the project URL with
// the query view={view}&id, with its leading and trailing white space
// removed (docs/remote/endpoints/project.md#get_id). Its doc comment in
// the Rust client calls it the view's UUID, but nothing checks that it is
// one.
//
// 404 is a [remote.KindViewNotFound] whose Subject is view. The other
// failures are those every read shares; see [Client.Changelist].
func (c *Client) ID(ctx context.Context, view string) (string, error) {
	body, _, err := c.fetch(ctx, opGetID, viewNotFound(view), func(q *wire.Query) error {
		if err := q.Add("view", view); err != nil {
			return err
		}
		q.Flag("id")
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body.data)), nil
}

// DownloadTag returns the short tag data of the tag at the Merkle state
// state, which [Client.ListTags] lists as [TagRef.Hash]
// (docs/remote/endpoints/project.md#download_tag).
//
// The request is a GET of the project URL with the query tag={state}. Only
// 200 succeeds. Its body is an 8-byte big-endian length n and then at
// least n bytes; the n bytes after the length are returned, and anything
// after them is ignored. A body shorter than 8 bytes is a
// [remote.KindProtocol] "Tag response too short (missing length prefix)",
// and one shorter than 8+n bytes is a [remote.KindProtocol] "Tag response
// truncated: expected {n} bytes, got {length - 8}".
//
// 404 is a [remote.KindTagNotFound] whose Subject is state. The other
// failures are those every read shares; see [Client.Changelist].
func (c *Client) DownloadTag(ctx context.Context, state string) ([]byte, error) {
	body, _, err := c.fetch(ctx, opDownloadTag, notFoundAs(remote.KindTagNotFound, state), func(q *wire.Query) error {
		return q.Add("tag", state)
	})
	if err != nil {
		return nil, err
	}
	b := body.data
	if len(b) < tagLengthPrefix {
		return nil, body.protocol("Tag response too short (missing length prefix)", nil)
	}
	n := binary.BigEndian.Uint64(b[:tagLengthPrefix])
	rest := b[tagLengthPrefix:]
	if uint64(len(rest)) < n {
		return nil, body.protocol("Tag response truncated: expected "+strconv.FormatUint(n, 10)+
			" bytes, got "+strconv.Itoa(len(rest)), nil)
	}
	return rest[:n:n], nil
}

// ListTags returns the view's tags
// (docs/remote/endpoints/project.md#list_remote_tags).
//
// The request is a GET of the project URL with the query tags={view}. On
// 200 each line of the body is trimmed and blank lines are skipped. Every
// other line is split at its first space into a [TagRef]'s Hash and Name,
// so a name may hold spaces, and a line with no space is skipped without
// an error. No tags is a nil slice and no error.
//
// 404 is no tags either, not an error: the Rust client reads it as a view
// that does not exist or has no tags. The other failures are those every
// read shares; see [Client.Changelist].
func (c *Client) ListTags(ctx context.Context, view string) ([]TagRef, error) {
	body, found, err := c.fetch(ctx, opListRemoteTags, nil, func(q *wire.Query) error {
		return q.Add("tags", view)
	})
	if err != nil || !found {
		return nil, err
	}
	return parseTagRefs(string(body.data)), nil
}

// DownloadAttestation returns the serialized attestation whose hash is
// hash: the 200 body of a GET of the project URL with the query
// attest={hash}, not inspected
// (docs/remote/endpoints/project.md#download_attestation).
//
// 404 is a [remote.KindProtocol] "Attestation not found: {hash}", as the
// Rust client reports it, not a not-found kind; it still matches
// [remote.ErrNotFound] by its status. The other failures are those every
// read shares; see [Client.Changelist].
func (c *Client) DownloadAttestation(ctx context.Context, hash string) ([]byte, error) {
	return c.download(ctx, opDownloadAttestation, "attest", hash, "Attestation not found: ")
}

// DownloadProvenance returns the serialized provenance graph whose hash is
// hash: the 200 body of a GET of the project URL with the query
// provenance={hash}, not inspected
// (docs/remote/endpoints/project.md#download_provenance).
//
// 404 is a [remote.KindProtocol] "Provenance graph not found: {hash}", as
// the Rust client reports it; it still matches [remote.ErrNotFound] by its
// status. The other failures are those every read shares; see
// [Client.Changelist].
func (c *Client) DownloadProvenance(ctx context.Context, hash string) ([]byte, error) {
	return c.download(ctx, opDownloadProvenance, "provenance", hash, "Provenance graph not found: ")
}

// download sends a GET with the query {key}={hash} and returns its 200
// body. A 404 is a [remote.KindProtocol] whose message is notFound and
// then hash.
func (c *Client) download(ctx context.Context, op, key, hash, notFound string) ([]byte, error) {
	on404 := func(resp *wire.Response) error {
		return response{op: op, resp: resp}.protocol(notFound+hash, nil)
	}
	body, _, err := c.fetch(ctx, op, on404, func(q *wire.Query) error {
		return q.Add(key, hash)
	})
	if err != nil {
		return nil, err
	}
	return body.data, nil
}

// ProvenanceList returns the hash of every provenance graph the remote
// holds, in the server's order
// (docs/remote/endpoints/project.md#get_provenance_list).
//
// The request is a GET of the project URL with the query provenance-list.
// On 200 each line of the body is trimmed, blank lines are dropped and
// every other line is a hash, not checked. No hashes is a nil slice and no
// error.
//
// 404 is no hashes either, not an error. The other failures are those
// every read shares; see [Client.Changelist].
func (c *Client) ProvenanceList(ctx context.Context) ([]string, error) {
	body, found, err := c.fetch(ctx, opGetProvenanceList, nil, func(q *wire.Query) error {
		q.Flag("provenance-list")
		return nil
	})
	if err != nil || !found {
		return nil, err
	}
	return nonBlankLines(string(body.data)), nil
}

// ListViews returns every view on the remote
// (docs/remote/endpoints/project.md#list_views).
//
// The request is a GET of the project URL with the query views. On 200
// each line of the body is parsed with the lenient [views.ParseInfo], and
// the lines it rejects are dropped, so an older server's JSON info blob
// is no views rather than an error. No views is a nil slice and no error.
// [project.Client.ListViewRefs] reads the same rows strictly, from the
// refs/views inventory.
//
// 404 is no views either, not an error. The other failures are those
// every read shares; see [Client.Changelist].
func (c *Client) ListViews(ctx context.Context) ([]views.Info, error) {
	body, found, err := c.fetch(ctx, opListViews, nil, func(q *wire.Query) error {
		q.Flag("views")
		return nil
	})
	if err != nil || !found {
		return nil, err
	}
	var infos []views.Info
	for _, line := range strings.Split(string(body.data), "\n") {
		if info, ok := views.ParseInfo(line); ok {
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// ViewManifest returns the view's manifest text exactly as the server sent
// it, neither trimmed nor parsed; [views.ParseManifest] reads it
// (docs/remote/endpoints/project.md#get_view_manifest).
//
// The request is a GET of the project URL with the query
// view-manifest={view}. ok is false, with no error, for a 404 and for a
// 200 whose body is empty or only white space: the remote has no such
// view. A 200 whose first non-blank line holds a tab is the manifest. Any
// other 200, such as an older server's JSON info blob, is a
// [remote.KindProtocol] "server does not support view manifests
// (?view-manifest)". The other failures are those every read shares; see
// [Client.Changelist].
func (c *Client) ViewManifest(ctx context.Context, view string) (text string, ok bool, err error) {
	body, found, err := c.fetch(ctx, opGetViewManifest, nil, func(q *wire.Query) error {
		return q.Add("view-manifest", view)
	})
	if err != nil || !found {
		return "", false, err
	}
	text = string(body.data)
	if strings.TrimSpace(text) == "" {
		return "", false, nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(line, "\t") {
			break
		}
		return text, true, nil
	}
	return "", false, body.protocol("server does not support view manifests (?view-manifest)", nil)
}

// response is a response whose body has been read, for the errors a read
// makes from it.
type response struct {
	op   string
	resp *wire.Response
	data []byte
}

// protocol returns the [remote.KindProtocol] with message msg and cause
// err.
func (r response) protocol(msg string, err error) *remote.Error {
	return &remote.Error{
		Kind:             remote.KindProtocol,
		Op:               r.op,
		Method:           http.MethodGet,
		URL:              r.resp.URL,
		StatusCode:       r.resp.StatusCode,
		Message:          msg,
		ServerMinVersion: r.resp.MinVersion,
		Err:              err,
	}
}

// viewNotFound returns the 404 handler for a read of view.
func viewNotFound(view string) func(*wire.Response) error {
	return notFoundAs(remote.KindViewNotFound, view)
}

// notFoundAs returns a 404 handler giving an error of kind about subject.
func notFoundAs(kind remote.Kind, subject string) func(*wire.Response) error {
	return func(resp *wire.Response) error {
		return &remote.Error{
			Kind:             kind,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			Subject:          subject,
			ServerMinVersion: resp.MinVersion,
		}
	}
}

// fetch sends a GET of the project URL with the query build writes, and
// returns the body of a 200 response with found true.
//
// A 404 is the error on404 returns, its Op set to op; a nil on404 makes a
// 404 found false with no error. 401 and 403 are a
// [remote.KindAuthenticationFailed] and any other status a
// [remote.KindHTTP].
func (c *Client) fetch(ctx context.Context, op string, on404 func(*wire.Response) error, build func(*wire.Query) error) (body response, found bool, err error) {
	u, err := c.queryURL(op, build)
	if err != nil {
		return response{}, false, err
	}
	resp, err := c.requester.Do(ctx, wire.Request{Op: op, Method: http.MethodGet, URL: u})
	if err != nil {
		return response{}, false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		if on404 == nil {
			return response{}, false, nil
		}
		err := on404(resp)
		var re *remote.Error
		if errors.As(err, &re) {
			re.Op = op
		}
		return response{}, false, err
	default:
		return response{}, false, wire.StatusError(op, resp, wire.ErrorBody(resp))
	}

	r := response{op: op, resp: resp}
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		if errors.Is(err, zstdframe.ErrLimit) {
			// A zstd Content-Encoding that inflated past the limit:
			// reading again would fail the same way.
			return response{}, false, r.protocol("failed to decode response body: "+err.Error(), err)
		}
		return response{}, false, &remote.Error{
			Kind:             remote.KindConnectionFailed,
			Op:               op,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			ServerMinVersion: resp.MinVersion,
			ContextDone:      ctx.Err() != nil,
			Err:              err,
		}
	}
	if int64(len(data)) > c.maxBody {
		return response{}, false, r.protocol("response body is larger than "+strconv.FormatInt(c.maxBody, 10)+" bytes", nil)
	}
	r.data = data
	return r, true, nil
}

// queryURL returns the project URL with the query build writes. A value
// the query refuses is a [remote.KindOther] wrapping the refusal.
func (c *Client) queryURL(op string, build func(*wire.Query) error) (*url.URL, error) {
	var q wire.Query
	if err := build(&q); err != nil {
		return nil, &remote.Error{
			Kind:    remote.KindOther,
			Op:      op,
			Method:  http.MethodGet,
			URL:     c.url.String(),
			Message: err.Error(),
			Err:     err,
		}
	}
	return c.url.WithRawQuery(q.String()), nil
}
