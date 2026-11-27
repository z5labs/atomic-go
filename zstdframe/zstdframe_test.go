package zstdframe_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math/rand/v2"
	"runtime"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/zstdframe"
)

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	require.NoError(t, err)
	return b
}

// payload returns n bytes from a seeded source, so they do not compress to
// nothing and every run sees the same ones.
func payload(n int) []byte {
	r := rand.New(rand.NewPCG(1, 2))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func stored(src string) []byte {
	return zstdframe.Stored{}.Compress(nil, []byte(src))
}

// frameLen walks one zstd frame at the start of b and returns the number of
// bytes it spans and whether its header declares a checksum.
func frameLen(t *testing.T, b []byte) (int, bool) {
	t.Helper()
	var h zstd.Header
	require.NoError(t, h.Decode(b))
	off := h.HeaderSize
	for {
		require.GreaterOrEqual(t, len(b)-off, 3, "frame ends inside a block header")
		bh := uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16
		off += 3
		size := int(bh >> 3)
		if (bh>>1)&3 == 1 { // RLE: one byte repeated size times
			size = 1
		}
		off += size
		require.LessOrEqual(t, off, len(b), "frame ends inside a block")
		if bh&1 == 1 {
			break
		}
	}
	if h.HasCheckSum {
		off += 4
	}
	return off, h.HasCheckSum
}

func newDecoder(t *testing.T, maxOutput int) *zstdframe.Decoder {
	t.Helper()
	d, err := zstdframe.NewDecoder(maxOutput)
	require.NoError(t, err)
	t.Cleanup(d.Close)
	return d
}

func readAll(t *testing.T, src []byte, limit int64) ([]byte, error) {
	t.Helper()
	r, err := zstdframe.NewReader(bytes.NewReader(src), limit)
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	return io.ReadAll(r)
}

func TestConstants(t *testing.T) {
	require.Equal(t, 3, zstdframe.DefaultLevel)
	require.Equal(t, 2<<20, zstdframe.AtomicWindow)
	require.Equal(t, 128<<10, zstdframe.MaxRawBlock)
	require.Equal(t, 8<<20, zstdframe.MaxWindow)
}

func TestStoredGoldens(t *testing.T) {
	t.Run("old bytes", func(t *testing.T) {
		require.Equal(t,
			mustHex(t, "28 b5 2f fd 00 58 51 00 00 6f 6c 64 20 62 79 74 65 73 0a"),
			stored("old bytes\n"))
	})
	t.Run("one zero byte", func(t *testing.T) {
		require.Equal(t,
			mustHex(t, "28 b5 2f fd 00 58 09 00 00 00"),
			stored("\x00"))
	})
	t.Run("empty", func(t *testing.T) {
		require.Equal(t, mustHex(t, "28 b5 2f fd 00 58 01 00 00"), stored(""))
	})
	t.Run("54 bytes", func(t *testing.T) {
		src := payload(54)
		got := zstdframe.Stored{}.Compress(nil, src)
		require.Equal(t, mustHex(t, "b1 01 00"), got[6:9])
		require.Equal(t, src, got[9:])
	})
	t.Run("one byte past a block", func(t *testing.T) {
		src := payload(131073)
		got := zstdframe.Stored{}.Compress(nil, src)
		require.Len(t, got, 6+3+zstdframe.MaxRawBlock+3+1)
		require.Equal(t, mustHex(t, "00 00 10"), got[6:9])
		second := 9 + zstdframe.MaxRawBlock
		require.Equal(t, mustHex(t, "09 00 00"), got[second:second+3])
		require.Equal(t, src[:zstdframe.MaxRawBlock], got[9:second])
		require.Equal(t, src[zstdframe.MaxRawBlock:], got[second+3:])
	})
	t.Run("appends to dst", func(t *testing.T) {
		got := zstdframe.Stored{}.Compress([]byte("x"), []byte("y"))
		require.Equal(t, mustHex(t, "78 28 b5 2f fd 00 58 09 00 00 79"), got)
	})
}

func TestStoredDecodes(t *testing.T) {
	d := newDecoder(t, zstdframe.AtomicWindow)
	for _, n := range []int{0, 1, 54, zstdframe.MaxRawBlock, zstdframe.MaxRawBlock + 1, 3*zstdframe.MaxRawBlock + 7} {
		src := payload(n)
		frame := zstdframe.Stored{}.Compress(nil, src)

		got, err := d.Decompress(nil, frame)
		require.NoError(t, err)
		require.Equal(t, len(src), len(got))
		require.True(t, bytes.Equal(src, got))

		got, err = readAll(t, frame, int64(n))
		require.NoError(t, err)
		require.Equal(t, len(src), len(got))
		require.True(t, bytes.Equal(src, got))
	}
}

func TestConcatenatedFrames(t *testing.T) {
	src := append(stored("he"), stored("llo")...)

	d := newDecoder(t, zstdframe.AtomicWindow)
	got, err := d.Decompress(nil, src)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))

	got, err = readAll(t, src, 16)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
}

func TestNewDecoderRefusesSmallLimit(t *testing.T) {
	d, err := zstdframe.NewDecoder(1 << 20)
	require.Error(t, err)
	require.Nil(t, d)

	d, err = zstdframe.NewDecoder(zstdframe.AtomicWindow - 1)
	require.Error(t, err)
	require.Nil(t, d)
}

func TestDecompress(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		d := newDecoder(t, zstdframe.AtomicWindow)
		got, err := d.Decompress(nil, nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})
	t.Run("appends to dst", func(t *testing.T) {
		d := newDecoder(t, zstdframe.AtomicWindow)
		got, err := d.Decompress([]byte("say "), stored("hello"))
		require.NoError(t, err)
		require.Equal(t, "say hello", string(got))
	})
	t.Run("corrupt input", func(t *testing.T) {
		d := newDecoder(t, zstdframe.AtomicWindow)
		_, err := d.Decompress(nil, []byte("not zstd at all"))
		require.Error(t, err)
		require.False(t, errors.Is(err, zstdframe.ErrLimit))
	})
}

func TestDecompressLimit(t *testing.T) {
	limit := zstdframe.AtomicWindow
	t.Run("exactly at the limit", func(t *testing.T) {
		d := newDecoder(t, limit)
		got, err := d.Decompress(nil, zstdframe.Stored{}.Compress(nil, payload(limit)))
		require.NoError(t, err)
		require.Len(t, got, limit)
	})
	t.Run("stored frame past the limit", func(t *testing.T) {
		d := newDecoder(t, limit)
		dst := []byte("kept")
		got, err := d.Decompress(dst, zstdframe.Stored{}.Compress(nil, payload(limit+1)))
		require.ErrorIs(t, err, zstdframe.ErrLimit)
		require.Equal(t, "kept", string(got))
	})
	t.Run("declared content size past the limit", func(t *testing.T) {
		e, err := zstdframe.NewEncoder(zstdframe.DefaultLevel)
		require.NoError(t, err)
		defer func() { require.NoError(t, e.Close()) }()
		d := newDecoder(t, limit)
		_, err = d.Decompress(nil, e.Compress(nil, make([]byte, limit+1)))
		require.ErrorIs(t, err, zstdframe.ErrLimit)
	})
	t.Run("concatenated frames past the limit together", func(t *testing.T) {
		d := newDecoder(t, limit)
		half := zstdframe.Stored{}.Compress(nil, payload(limit/2+1))
		_, err := d.Decompress(nil, append(half, half...))
		require.ErrorIs(t, err, zstdframe.ErrLimit)
	})
	t.Run("limit does not count dst", func(t *testing.T) {
		d := newDecoder(t, limit)
		got, err := d.Decompress(make([]byte, limit), zstdframe.Stored{}.Compress(nil, payload(limit)))
		require.NoError(t, err)
		require.Len(t, got, 2*limit)
	})
	t.Run("window past MaxWindow", func(t *testing.T) {
		d := newDecoder(t, 64<<20)
		_, err := d.Decompress(nil, wideWindowFrame(t))
		require.ErrorIs(t, err, zstdframe.ErrLimit)
	})
}

// wideWindowFrame is an empty frame whose window descriptor 0x70 asks for a
// 16 MiB window, twice MaxWindow.
func wideWindowFrame(t *testing.T) []byte {
	return mustHex(t, "28 b5 2f fd 00 70 01 00 00")
}

func TestDecompressConcurrent(t *testing.T) {
	d := newDecoder(t, zstdframe.AtomicWindow)
	const workers = 8
	type result struct {
		want, got []byte
		err       error
	}
	results := make(chan result, workers)
	for i := range workers {
		go func() {
			want := payload(1000 + i*100)
			got, err := d.Decompress(nil, zstdframe.Stored{}.Compress(nil, want))
			results <- result{want: want, got: got, err: err}
		}()
	}
	for range workers {
		r := <-results
		require.NoError(t, r.err)
		require.Equal(t, r.want, r.got)
	}
}

func TestEncoder(t *testing.T) {
	d := newDecoder(t, zstdframe.AtomicWindow)
	inputs := [][]byte{
		nil,
		[]byte("hello"),
		bytes.Repeat([]byte("atomic "), 10000),
		payload(3*zstdframe.MaxRawBlock + 5),
	}
	for _, level := range []int{1, 2, zstdframe.DefaultLevel, 9, 19, 22} {
		e, err := zstdframe.NewEncoder(level)
		require.NoError(t, err)
		for _, src := range inputs {
			frame := e.Compress(nil, src)

			n, checksum := frameLen(t, frame)
			require.Equal(t, len(frame), n, "level %d: more than one frame", level)
			require.False(t, checksum, "level %d: frame has a checksum", level)

			got, err := d.Decompress(nil, frame)
			require.NoError(t, err)
			require.Equal(t, len(src), len(got))
			require.True(t, bytes.Equal(src, got))

			got, err = readAll(t, frame, int64(len(src)))
			require.NoError(t, err)
			require.True(t, bytes.Equal(src, got))
		}
		require.Equal(t, []byte("x"), e.Compress([]byte("x"), []byte("y"))[:1])
		require.NoError(t, e.Close())
	}
}

func TestNewEncoderRefusesLevel(t *testing.T) {
	for _, level := range []int{-1, 0, 23} {
		e, err := zstdframe.NewEncoder(level)
		require.Error(t, err, "level %d", level)
		require.Nil(t, e)
	}
}

func TestReader(t *testing.T) {
	t.Run("small limit reads a 2 MiB window", func(t *testing.T) {
		got, err := readAll(t, stored("hello"), 16)
		require.NoError(t, err)
		require.Equal(t, "hello", string(got))
	})
	t.Run("exactly at the limit", func(t *testing.T) {
		got, err := readAll(t, stored("hello"), 5)
		require.NoError(t, err)
		require.Equal(t, "hello", string(got))
	})
	t.Run("zero limit, empty stream", func(t *testing.T) {
		got, err := readAll(t, nil, 0)
		require.NoError(t, err)
		require.Empty(t, got)
	})
	t.Run("zero limit, empty frame", func(t *testing.T) {
		got, err := readAll(t, stored(""), 0)
		require.NoError(t, err)
		require.Empty(t, got)
	})
	t.Run("4 KiB frame past a 1 KiB limit", func(t *testing.T) {
		src := payload(4 << 10)
		got, err := readAll(t, zstdframe.Stored{}.Compress(nil, src), 1024)
		require.ErrorIs(t, err, zstdframe.ErrLimit)
		require.Equal(t, src[:1024], got)
	})
	t.Run("limit error is sticky", func(t *testing.T) {
		r, err := zstdframe.NewReader(bytes.NewReader(stored("hello")), 2)
		require.NoError(t, err)
		defer func() { require.NoError(t, r.Close()) }()
		buf := make([]byte, 16)
		n, err := r.Read(buf)
		require.ErrorIs(t, err, zstdframe.ErrLimit)
		require.Equal(t, "he", string(buf[:n]))
		n, err = r.Read(buf)
		require.ErrorIs(t, err, zstdframe.ErrLimit)
		require.Zero(t, n)
	})
	t.Run("one byte at a time", func(t *testing.T) {
		r, err := zstdframe.NewReader(bytes.NewReader(append(stored("he"), stored("llo")...)), 4)
		require.NoError(t, err)
		defer func() { require.NoError(t, r.Close()) }()
		var got []byte
		b := make([]byte, 1)
		for {
			n, err := r.Read(b)
			got = append(got, b[:n]...)
			if err != nil {
				require.ErrorIs(t, err, zstdframe.ErrLimit)
				break
			}
		}
		require.Equal(t, "hell", string(got))
	})
	t.Run("window past MaxWindow", func(t *testing.T) {
		_, err := readAll(t, wideWindowFrame(t), 1<<30)
		require.ErrorIs(t, err, zstdframe.ErrLimit)
	})
	t.Run("corrupt input", func(t *testing.T) {
		_, err := readAll(t, []byte("not zstd at all"), 1<<20)
		require.Error(t, err)
		require.False(t, errors.Is(err, zstdframe.ErrLimit))
	})
	t.Run("refuses a negative limit", func(t *testing.T) {
		r, err := zstdframe.NewReader(bytes.NewReader(nil), -1)
		require.Error(t, err)
		require.Nil(t, r)
	})
	t.Run("refuses a nil reader", func(t *testing.T) {
		r, err := zstdframe.NewReader(nil, 0)
		require.Error(t, err)
		require.Nil(t, r)
	})
}

type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error {
	c.closed = true
	return nil
}

func TestReaderCloseLeavesSourceOpen(t *testing.T) {
	src := &closeRecorder{Reader: bytes.NewReader(stored("hello"))}
	r, err := zstdframe.NewReader(src, 16)
	require.NoError(t, err)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
	require.NoError(t, r.Close())
	require.False(t, src.closed)
}

func TestNoGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	d, err := zstdframe.NewDecoder(zstdframe.AtomicWindow)
	require.NoError(t, err)
	e, err := zstdframe.NewEncoder(zstdframe.DefaultLevel)
	require.NoError(t, err)
	src := bytes.Repeat([]byte("atomic "), 50000)
	frame := e.Compress(nil, src)
	_, err = d.Decompress(nil, frame)
	require.NoError(t, err)
	r, err := zstdframe.NewReader(bytes.NewReader(frame), int64(len(src)))
	require.NoError(t, err)
	_, err = io.ReadAll(r)
	require.NoError(t, err)

	require.Equal(t, before, runtime.NumGoroutine())

	require.NoError(t, r.Close())
	require.NoError(t, e.Close())
	d.Close()
}
