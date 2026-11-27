package project

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
	"github.com/z5labs/atomic-go/zstdframe"
)

// The Rust client's names for the change download requests, used as
// [remote.Error.Op].
const (
	opDownloadChange       = "download_change"
	opDownloadChangeToFile = "download_change_to_file"
)

// copyBufferSize is the size of the buffer a download is copied through.
const copyBufferSize = 32 << 10

// DownloadChange returns the change file whose hash is h, read whole into
// memory (docs/remote/endpoints/project.md#download_change).
//
// The request is a GET of the project URL with the query change={h},
// where {h} is h's canonical base32 text, [ident.Hash.String]. It sends no
// body and no Atomic-Protocol header. Only 200 succeeds, and its body is
// returned as it arrived, after any Content-Encoding is decoded.
//
// The bytes are not verified: nothing checks that they are a change file,
// or that they hash to h. Verify them with changefile.Verify before
// trusting them.
//
// Unlike the Rust client, which reads the body without a limit, a body
// larger than the Client's maximum pack size ([WithMaxPackSize]) is a
// [remote.KindProtocol]. Use [Client.DownloadChangeTo] to stream a change
// of any size.
//
// 404 is a [remote.KindChangeNotFound] whose Subject is h's text, and so
// matches [remote.ErrNotFound]. 401 and 403 are
// [remote.KindAuthenticationFailed], and any other status is a
// [remote.KindHTTP]. A body that cannot be read is a
// [remote.KindConnectionFailed], except that one sent with a zstd
// Content-Encoding that inflates past the maximum pack size is a
// [remote.KindProtocol].
func (c *Client) DownloadChange(ctx context.Context, h ident.Hash) ([]byte, error) {
	resp, err := c.requestChange(ctx, opDownloadChange, h)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	n, err := copyChange(&buf, io.LimitReader(resp.Body, c.maxPackSize+1))
	if err != nil {
		return nil, c.copyError(ctx, opDownloadChange, resp, err)
	}
	if n > c.maxPackSize {
		return nil, &remote.Error{
			Kind:             remote.KindProtocol,
			Op:               opDownloadChange,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			Message:          "change file is larger than " + strconv.FormatInt(c.maxPackSize, 10) + " bytes",
			ServerMinVersion: resp.MinVersion,
		}
	}
	return buf.Bytes(), nil
}

// DownloadChangeTo streams the change file whose hash is h into w and
// returns the number of bytes written to it
// (docs/remote/endpoints/project.md#download_change_to_file). The Rust
// client writes the change to a file path; taking an [io.Writer] instead
// leaves where the bytes go, a file, a hasher or a buffer, to the caller,
// and the Client never touches a filesystem.
//
// It sends the same request as [Client.DownloadChange] and fails the same
// ways, except that the body is not held in memory and has no size limit
// beyond the cap on a zstd Content-Encoding ([WithMaxPackSize]). The bytes
// are not verified; see DownloadChange.
//
// A failure to write to w is a [remote.KindOther] wrapping w's error, so
// [errors.Is] and [errors.As] reach it. A failure part way through, of
// either kind, returns the bytes already written to w, which then holds an
// incomplete change. A nil w fails with a [remote.KindOther] before
// anything is sent.
func (c *Client) DownloadChangeTo(ctx context.Context, h ident.Hash, w io.Writer) (int64, error) {
	if w == nil {
		return 0, &remote.Error{
			Kind:    remote.KindOther,
			Op:      opDownloadChangeToFile,
			Method:  http.MethodGet,
			URL:     c.url.String(),
			Message: "project: DownloadChangeTo: nil io.Writer",
		}
	}

	resp, err := c.requestChange(ctx, opDownloadChangeToFile, h)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	n, err := copyChange(w, resp.Body)
	if err != nil {
		return n, c.copyError(ctx, opDownloadChangeToFile, resp, err)
	}
	return n, nil
}

// requestChange sends the download_change request for h and returns a 200
// response; any other status is returned as its error, the body already
// closed.
func (c *Client) requestChange(ctx context.Context, op string, h ident.Hash) (*wire.Response, error) {
	subject := h.String()
	var q wire.Query
	if err := q.Add("change", subject); err != nil {
		// A hash's base32 text holds nothing a query refuses.
		return nil, &remote.Error{
			Kind:    remote.KindOther,
			Op:      op,
			Method:  http.MethodGet,
			URL:     c.url.String(),
			Message: err.Error(),
			Err:     err,
		}
	}

	resp, err := c.requester.Do(ctx, wire.Request{
		Op:     op,
		Method: http.MethodGet,
		URL:    c.url.WithRawQuery(q.String()),
	})
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return resp, nil
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, &remote.Error{
			Kind:             remote.KindChangeNotFound,
			Op:               op,
			Method:           http.MethodGet,
			URL:              resp.URL,
			StatusCode:       resp.StatusCode,
			Subject:          subject,
			ServerMinVersion: resp.MinVersion,
		}
	default:
		err := wire.StatusError(op, resp, wire.ErrorBody(resp))
		_ = resp.Body.Close()
		return nil, err
	}
}

// writeError marks an error that came from the destination writer, so that
// it is not mistaken for one from the body.
type writeError struct {
	err error
}

func (e *writeError) Error() string {
	return e.err.Error()
}

// copyChange copies r to w, as [io.Copy] does, but returns a failure to
// write as a *writeError so the caller can tell it from a failure to read.
func copyChange(w io.Writer, r io.Reader) (int64, error) {
	buf := make([]byte, copyBufferSize)
	var written int64
	for {
		nr, rerr := r.Read(buf)
		if nr > 0 {
			nw, werr := w.Write(buf[:nr])
			if nw < 0 || nw > nr {
				nw = 0
				if werr == nil {
					werr = errors.New("invalid write result")
				}
			}
			written += int64(nw)
			if werr != nil {
				return written, &writeError{err: werr}
			}
			if nw != nr {
				return written, &writeError{err: io.ErrShortWrite}
			}
		}
		if rerr == io.EOF {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}

// copyError maps a failure to copy a downloaded change to its error.
func (c *Client) copyError(ctx context.Context, op string, resp *wire.Response, err error) *remote.Error {
	e := &remote.Error{
		Op:               op,
		Method:           http.MethodGet,
		URL:              resp.URL,
		StatusCode:       resp.StatusCode,
		ServerMinVersion: resp.MinVersion,
		Err:              err,
	}
	var we *writeError
	switch {
	case errors.As(err, &we):
		e.Kind = remote.KindOther
		e.Message = "failed to write change file: " + we.err.Error()
		e.Err = we.err
	case errors.Is(err, zstdframe.ErrLimit):
		// A zstd Content-Encoding that inflated past the maximum pack
		// size: reading again would fail the same way.
		e.Kind = remote.KindProtocol
		e.Message = "failed to decode change file: " + err.Error()
	default:
		// The body could not be read: the connection failed, the
		// request's context ended, or a Content-Encoding did not decode.
		e.Kind = remote.KindConnectionFailed
		e.ContextDone = ctx.Err() != nil
	}
	return e
}
