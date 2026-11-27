package zstdframe

import (
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// Encoder compresses whole payloads held in memory. It is safe for
// concurrent use.
//
// Its output decodes to the input with any zstd decoder, but it is not
// byte-identical to libzstd's at the same level, and so not to what
// Atomic writes; see the open questions of
// docs/remote/change-format/container.md. Use [Stored] where the bytes
// themselves must match.
type Encoder struct {
	e *zstd.Encoder
}

// NewEncoder returns an Encoder compressing at the given zstd level, 1 to
// 22; Atomic's writer uses [DefaultLevel]. klauspost implements four
// levels, and maps each zstd level to the nearest of them.
func NewEncoder(level int) (*Encoder, error) {
	if level < 1 || level > 22 {
		return nil, fmt.Errorf("zstdframe: compression level %d is outside 1 to 22", level)
	}
	e, err := zstd.NewWriter(nil,
		zstd.WithEncoderConcurrency(1),
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)),
		zstd.WithEncoderCRC(false),
		zstd.WithZeroFrames(true),
	)
	if err != nil {
		return nil, fmt.Errorf("zstdframe: new encoder: %w", err)
	}
	return &Encoder{e: e}, nil
}

// Compress appends src to dst as exactly one zstd frame with no checksum,
// as Atomic's writer does with zstd::encode_all. An empty src still gives
// one frame.
func (e *Encoder) Compress(dst, src []byte) []byte {
	return e.e.EncodeAll(src, dst)
}

// Close releases the Encoder's state. An Encoder must not be used after
// Close.
func (e *Encoder) Close() error {
	return e.e.Close()
}
