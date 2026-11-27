package zstdframe

import (
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// Decoder decompresses whole zstd payloads held in memory, refusing to
// produce more than the limit it was constructed with. It is safe for
// concurrent use; concurrent calls take turns, since its single decoder
// state is never duplicated.
type Decoder struct {
	d *zstd.Decoder
}

// NewDecoder returns a Decoder whose [Decoder.Decompress] produces at most
// maxOutput bytes per call.
//
// maxOutput must be at least [AtomicWindow]: klauspost caps a frame's
// window at the output limit, so a smaller limit would refuse every frame
// Atomic writes, however little it held.
func NewDecoder(maxOutput int) (*Decoder, error) {
	if maxOutput < AtomicWindow {
		return nil, fmt.Errorf("zstdframe: decoder limit %d is below AtomicWindow (%d)", maxOutput, AtomicWindow)
	}
	d, err := zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(uint64(maxOutput)),
		zstd.WithDecoderMaxWindow(MaxWindow),
	)
	if err != nil {
		return nil, fmt.Errorf("zstdframe: new decoder: %w", err)
	}
	return &Decoder{d: d}, nil
}

// Decompress decodes every frame in src, concatenated, and appends the
// output to dst, as Atomic's reader does with zstd::decode_all. An empty
// src decodes to nothing.
//
// Output past the limit given to [NewDecoder] is an error wrapping
// [ErrLimit]. The limit counts only what this call appends, not what dst
// already holds. On any error dst is returned with nothing appended.
func (d *Decoder) Decompress(dst, src []byte) ([]byte, error) {
	if len(dst) == 0 {
		out, err := d.d.DecodeAll(src, dst)
		if err != nil {
			return dst, fmt.Errorf("zstdframe: decompress: %w", mapErr(err))
		}
		return out, nil
	}
	// klauspost counts what dst already holds against the limit of a frame
	// that declares its content size, so decode on its own and append.
	out, err := d.d.DecodeAll(src, nil)
	if err != nil {
		return dst, fmt.Errorf("zstdframe: decompress: %w", mapErr(err))
	}
	return append(dst, out...), nil
}

// Close releases the Decoder's state. A Decoder must not be used after
// Close.
func (d *Decoder) Close() {
	d.d.Close()
}
