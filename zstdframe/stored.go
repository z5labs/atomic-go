package zstdframe

// frameHeader is a stored frame's header: the zstd magic number
// 0xFD2FB528 little endian, frame header descriptor 0x00 (no content size,
// no checksum, no dictionary, not single segment) and window descriptor
// 0x58 (a 2 MiB window, [AtomicWindow]).
const frameHeader = "\x28\xb5\x2f\xfd\x00\x58"

// Stored writes payloads as zstd frames of raw blocks, uncompressed. For
// a small payload this is byte-identical to what libzstd writes, which is
// what lets Go reproduce the bytes of Atomic's change files; see
// docs/remote/change-format/container.md, under Compression.
type Stored struct{}

// Compress appends src to dst as one zstd frame: the header described by
// the container format, then src in raw blocks of at most [MaxRawBlock]
// bytes. Each block header is three bytes little endian holding the block's
// length shifted left by 3, with bit 0 set on the last block. An empty src
// gives one empty last block.
func (Stored) Compress(dst, src []byte) []byte {
	dst = append(dst, frameHeader...)
	for {
		n := min(len(src), MaxRawBlock)
		h := uint32(n) << 3
		if n == len(src) {
			h |= 1
		}
		dst = append(dst, byte(h), byte(h>>8), byte(h>>16))
		dst = append(dst, src[:n]...)
		src = src[n:]
		if len(src) == 0 {
			return dst
		}
	}
}
