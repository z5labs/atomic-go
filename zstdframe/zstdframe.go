// Package zstdframe holds the zstd codecs Atomic's change sections, sync
// bodies and zstd-encoded HTTP responses need: a bounded in-memory
// [Decoder], an [Encoder], [Stored] raw frames that are byte-identical to
// what libzstd writes for small payloads, and a bounded stream [Reader].
//
// Every decoder refuses a decompression bomb: output past the limit it was
// given, or a frame whose window is larger than [MaxWindow], is reported as
// an error wrapping [ErrLimit].
//
// It is the only package in this module that imports
// github.com/klauspost/compress. Every codec it builds runs with a
// concurrency of 1, so nothing here starts a goroutine, and nothing is cached
// at package level: each value owns the state it was constructed with.
//
// The format is documented in docs/remote/change-format/container.md, under
// Compression, and docs/remote/transport.md, under Compression.
package zstdframe

import (
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

const (
	// DefaultLevel is the compression level Atomic's change writer uses.
	DefaultLevel = 3

	// AtomicWindow is the window size of the frames Atomic writes: window
	// descriptor 0x58, 2 MiB.
	AtomicWindow = 2 << 20

	// MaxRawBlock is the largest block a zstd frame may hold, 128 KiB, and
	// so the largest raw block [Stored] writes.
	MaxRawBlock = 128 << 10

	// MaxWindow is the largest frame window any decoder here accepts,
	// 8 MiB. It is what a decoder must hold in memory to decode a frame,
	// whatever the output limit.
	MaxWindow = 8 << 20
)

// ErrLimit reports output that would pass a decoder's limit, or a frame
// whose window or declared size the decoder will not hold.
var ErrLimit = errors.New("zstdframe: decoded output exceeds limit")

// mapErr reports klauspost's window and size errors as wrapping
// [ErrLimit], keeping the original in the chain, and returns every other
// error unchanged.
func mapErr(err error) error {
	switch {
	case errors.Is(err, zstd.ErrWindowSizeExceeded),
		errors.Is(err, zstd.ErrDecoderSizeExceeded),
		errors.Is(err, zstd.ErrFrameSizeExceeded):
		return fmt.Errorf("%w: %w", ErrLimit, err)
	default:
		return err
	}
}
