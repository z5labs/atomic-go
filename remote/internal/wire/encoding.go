package wire

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/z5labs/atomic-go/zstdframe"
)

// decodeBody returns resp's body with every coding its Content-Encoding
// lists undone, last applied first. Each decoder starts on the body's first
// Read, an empty body decoding to an empty body, so a malformed body fails when it is read rather than here; only a
// coding the package does not know fails here.
func decodeBody(resp *http.Response, maxDecoded int64) (io.ReadCloser, error) {
	var codings []string
	for _, v := range resp.Header.Values("Content-Encoding") {
		for _, c := range strings.Split(v, ",") {
			c = strings.ToLower(strings.TrimSpace(c))
			if c == "" || c == "identity" {
				continue
			}
			switch c {
			case "zstd", "gzip", "x-gzip", "deflate":
			default:
				return nil, fmt.Errorf("unsupported Content-Encoding %q", c)
			}
			codings = append(codings, c)
		}
	}

	body := resp.Body
	if body == nil {
		body = http.NoBody
	}
	var r io.ReadCloser = body
	for i := len(codings) - 1; i >= 0; i-- {
		r = &lazyDecoder{src: r, open: opener(codings[i], maxDecoded)}
	}
	return r, nil
}

// opener returns the function that starts the decoder for coding.
func opener(coding string, maxDecoded int64) func(io.Reader) (io.ReadCloser, error) {
	switch coding {
	case "zstd":
		return func(r io.Reader) (io.ReadCloser, error) {
			return zstdframe.NewReader(r, maxDecoded)
		}
	case "deflate":
		// HTTP's deflate is the zlib format, RFC 1950, not raw deflate.
		return zlib.NewReader
	default: // gzip, x-gzip
		return func(r io.Reader) (io.ReadCloser, error) {
			return gzip.NewReader(r)
		}
	}
}

// lazyDecoder decodes src, starting its decoder on the first Read. Close
// closes the decoder, if it started, and then src.
type lazyDecoder struct {
	src  io.ReadCloser
	open func(io.Reader) (io.ReadCloser, error)
	dec  io.ReadCloser
	err  error
}

func (d *lazyDecoder) Read(p []byte) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	if d.dec == nil {
		// An empty body decodes to an empty body, whatever the coding;
		// zlib, for one, would call it truncated.
		var first [1]byte
		n, err := io.ReadFull(d.src, first[:])
		if err == io.EOF {
			d.err = io.EOF
			return 0, d.err
		}
		if err != nil {
			d.err = err
			return 0, d.err
		}
		dec, err := d.open(io.MultiReader(bytes.NewReader(first[:n]), d.src))
		if err != nil {
			d.err = fmt.Errorf("wire: decode response body: %w", err)
			return 0, d.err
		}
		d.dec = dec
	}
	return d.dec.Read(p)
}

func (d *lazyDecoder) Close() error {
	var err error
	if d.dec != nil {
		err = d.dec.Close()
	}
	if cerr := d.src.Close(); err == nil {
		err = cerr
	}
	return err
}
