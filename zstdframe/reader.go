package zstdframe

import (
	"errors"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Reader decodes a stream of concatenated zstd frames, such as a sync body
// or a zstd-encoded HTTP response, without holding it in memory whole.
//
// It produces at most the limit given to [NewReader]. The limit counts
// decoded output, not compressed input: a Read that would pass it returns
// the bytes up to the limit and an error wrapping [ErrLimit], and every Read
// after that returns the same error.
//
// The window a frame may ask for is capped at [MaxWindow] whatever the
// limit, so a small limit still reads the 2 MiB windows Atomic writes. A
// frame asking for more is an error wrapping [ErrLimit].
type Reader struct {
	d     *zstd.Decoder
	limit int64
	n     int64
	err   error
}

// NewReader returns a Reader decoding r, producing at most limit bytes. Any
// limit of 0 or more is accepted.
func NewReader(r io.Reader, limit int64) (*Reader, error) {
	if r == nil {
		return nil, errors.New("zstdframe: new reader: nil io.Reader")
	}
	if limit < 0 {
		return nil, fmt.Errorf("zstdframe: new reader: negative limit %d", limit)
	}
	d, err := zstd.NewReader(r,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(MaxWindow),
		zstd.WithDecoderMaxWindow(MaxWindow),
		// Without this, klauspost decodes a small *bytes.Buffer whole
		// with DecodeAll, inside NewReader, and caps its output at
		// MaxWindow rather than streaming it.
		zstd.WithDecodeBuffersBelow(0),
	)
	if err != nil {
		return nil, fmt.Errorf("zstdframe: new reader: %w", mapErr(err))
	}
	return &Reader{d: d, limit: limit}, nil
}

// Read reads decoded bytes into p. At the end of the stream it returns
// [io.EOF].
func (r *Reader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if len(p) == 0 {
		return 0, nil
	}
	remaining := r.limit - r.n
	if int64(len(p)) > remaining {
		// One byte past the limit is enough to tell that the stream
		// would pass it.
		p = p[:remaining+1]
	}
	n, err := r.d.Read(p)
	if int64(n) > remaining {
		r.n = r.limit
		r.err = fmt.Errorf("zstdframe: read: %w: more than %d bytes", ErrLimit, r.limit)
		return int(remaining), r.err
	}
	r.n += int64(n)
	if err != nil && err != io.EOF {
		err = fmt.Errorf("zstdframe: read: %w", mapErr(err))
	}
	return n, err
}

// Close releases the Reader's decoder state. It does not close the
// io.Reader given to [NewReader].
func (r *Reader) Close() error {
	r.d.Close()
	return nil
}
