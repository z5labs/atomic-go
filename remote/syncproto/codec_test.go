package syncproto_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"math/rand/v2"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/internal/postcard"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/zstdframe"
)

// payload returns n bytes from a seeded source, so they do not compress
// and every run sees the same ones.
func payload(n int) []byte {
	r := rand.New(rand.NewPCG(3, 4))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// frameLen walks the zstd frame at the start of b and returns the number of
// bytes it spans.
func frameLen(t *testing.T, b []byte) int {
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
	return off
}

// decompress undoes a body's zstd layer.
func decompress(t *testing.T, body []byte) []byte {
	t.Helper()
	d, err := zstdframe.NewDecoder(zstdframe.AtomicWindow)
	require.NoError(t, err)
	defer d.Close()
	raw, err := d.Decompress(nil, body)
	require.NoError(t, err)
	return raw
}

// collect streams body and returns the objects and refs it held.
func collect(c *syncproto.Codec, body io.Reader) (syncproto.Pack, error) {
	var p syncproto.Pack
	refs, err := c.StreamPack(body, func(o syncproto.ObjectRecord) error {
		p.Objects = append(p.Objects, o)
		return nil
	})
	p.Refs = refs
	return p, err
}

// decoders runs check against both ways of decoding a pack.
func decoders(t *testing.T, check func(t *testing.T, decode func(io.Reader) (syncproto.Pack, error))) {
	t.Helper()
	for _, name := range []string{"DecodePack", "StreamPack"} {
		t.Run(name, func(t *testing.T) {
			check(t, func(r io.Reader) (syncproto.Pack, error) {
				return decodeWith(name, syncproto.NewCodec(), r)
			})
		})
	}
}

func decodeWith(name string, c *syncproto.Codec, r io.Reader) (syncproto.Pack, error) {
	if name == "DecodePack" {
		return c.DecodePack(r)
	}
	return collect(c, r)
}

func bigPack() syncproto.Pack {
	return syncproto.Pack{
		Objects: []syncproto.ObjectRecord{
			{Family: syncproto.Change, Key: "one", Bytes: payload(4 << 10)},
			{Family: syncproto.View, Key: "two", Bytes: []byte("{}")},
			{Family: syncproto.Conflict, Key: "three", Bytes: []byte{0}},
		},
		Refs: []syncproto.RefRecord{
			{Name: "dev", NewTarget: "two"},
			{Name: "f", ExpectOld: ptr("two"), NewTarget: "three"},
		},
	}
}

func TestEncode(t *testing.T) {
	c := syncproto.NewCodec()

	t.Run("pack is one zstd frame of its postcard", func(t *testing.T) {
		body, err := c.EncodePack(goldenPack())
		require.NoError(t, err)
		require.Equal(t, len(body), frameLen(t, body))
		require.Equal(t, mustHex(t, packGolden), decompress(t, body))
	})

	t.Run("wants is one zstd frame of its postcard", func(t *testing.T) {
		body, err := c.EncodeWants(syncproto.Advertise("dev"))
		require.NoError(t, err)
		require.Equal(t, len(body), frameLen(t, body))
		require.Equal(t, mustHex(t, advertiseGolden), decompress(t, body))
	})

	t.Run("compression is applied", func(t *testing.T) {
		p := syncproto.Pack{Objects: []syncproto.ObjectRecord{
			{Family: syncproto.Change, Key: "k", Bytes: bytes.Repeat([]byte("atomic "), 1000)},
		}}
		raw, err := p.AppendPostcard(nil)
		require.NoError(t, err)
		body, err := c.EncodePack(p)
		require.NoError(t, err)
		require.Less(t, len(body), len(raw))
	})
}

func TestDecode(t *testing.T) {
	decoders(t, func(t *testing.T, decode func(io.Reader) (syncproto.Pack, error)) {
		c := syncproto.NewCodec()

		t.Run("golden", func(t *testing.T) {
			body, err := c.EncodePack(goldenPack())
			require.NoError(t, err)
			got, err := decode(bytes.NewReader(body))
			require.NoError(t, err)
			require.Equal(t, goldenPack(), got)
		})

		t.Run("round trip", func(t *testing.T) {
			body, err := c.EncodePack(bigPack())
			require.NoError(t, err)
			got, err := decode(bytes.NewReader(body))
			require.NoError(t, err)
			require.Equal(t, bigPack(), got)
		})

		t.Run("an unknown family is kept", func(t *testing.T) {
			raw := mustHex(t, packGolden)
			raw[1] = 0x09
			got, err := decode(bytes.NewReader(zstdframe.Stored{}.Compress(nil, raw)))
			require.NoError(t, err)
			want := goldenPack()
			want.Objects[0].Family = syncproto.Family(9)
			require.Equal(t, want, got)
		})

		t.Run("empty pack", func(t *testing.T) {
			body, err := c.EncodePack(syncproto.Pack{})
			require.NoError(t, err)
			got, err := decode(bytes.NewReader(body))
			require.NoError(t, err)
			require.Equal(t, syncproto.Pack{}, got)
		})

		t.Run("trailing bytes, in the frame and in another, are ignored", func(t *testing.T) {
			raw := append(mustHex(t, packGolden), 0xff, 0x00, 0xff)
			body := zstdframe.Stored{}.Compress(nil, raw)
			body = zstdframe.Stored{}.Compress(body, []byte{0xff, 0xff})
			got, err := decode(bytes.NewReader(body))
			require.NoError(t, err)
			require.Equal(t, goldenPack(), got)
		})

		t.Run("one byte at a time", func(t *testing.T) {
			body, err := c.EncodePack(bigPack())
			require.NoError(t, err)
			got, err := decode(iotest.OneByteReader(bytes.NewReader(body)))
			require.NoError(t, err)
			require.Equal(t, bigPack(), got)
		})

		t.Run("not zstd fails at Stage zstd", func(t *testing.T) {
			_, err := decode(strings.NewReader("not zstd"))
			requireDecodeError(t, err, syncproto.StageZstd)
			require.NotErrorIs(t, err, syncproto.ErrTooLarge)
		})

		t.Run("a truncated frame fails at Stage zstd", func(t *testing.T) {
			body, err := c.EncodePack(bigPack())
			require.NoError(t, err)
			_, err = decode(bytes.NewReader(body[:len(body)-1]))
			requireDecodeError(t, err, syncproto.StageZstd)
		})

		t.Run("a frame of 0xFF bytes fails at Stage postcard", func(t *testing.T) {
			body := zstdframe.Stored{}.Compress(nil, bytes.Repeat([]byte{0xff}, 64))
			_, err := decode(bytes.NewReader(body))
			requireDecodeError(t, err, syncproto.StagePostcard)
			require.ErrorIs(t, err, postcard.ErrBadVarint)
		})

		t.Run("an empty body fails at Stage postcard", func(t *testing.T) {
			_, err := decode(bytes.NewReader(nil))
			requireDecodeError(t, err, syncproto.StagePostcard)
		})

		t.Run("a truncated pack fails at Stage postcard", func(t *testing.T) {
			golden := mustHex(t, packGolden)
			for i := range golden {
				_, err := decode(bytes.NewReader(zstdframe.Stored{}.Compress(nil, golden[:i])))
				requireDecodeError(t, err, syncproto.StagePostcard)
			}
		})

		t.Run("a declared length the body does not back fails at Stage postcard", func(t *testing.T) {
			// One object, family Change, key "k", then bytes declared as
			// 1 GiB and three bytes delivered.
			raw := []byte{0x01, 0x00, 0x01, 'k', 0x80, 0x80, 0x80, 0x80, 0x04, 'a', 'b', 'c'}
			_, err := decode(bytes.NewReader(zstdframe.Stored{}.Compress(nil, raw)))
			requireDecodeError(t, err, syncproto.StagePostcard)
		})

		t.Run("a reader failing mid-body", func(t *testing.T) {
			boom := errors.New("boom")
			body, err := c.EncodePack(bigPack())
			require.NoError(t, err)
			r := io.MultiReader(bytes.NewReader(body[:len(body)/2]), iotest.ErrReader(boom))
			_, err = decode(r)
			require.ErrorIs(t, err, boom)
			var de *syncproto.DecodeError
			require.False(t, errors.As(err, &de), "a read failure is not a DecodeError: %v", err)
			require.NotErrorIs(t, err, syncproto.ErrTooLarge)
		})

		t.Run("a reader failing at once", func(t *testing.T) {
			boom := errors.New("boom")
			_, err := decode(iotest.ErrReader(boom))
			require.ErrorIs(t, err, boom)
			var de *syncproto.DecodeError
			require.False(t, errors.As(err, &de))
		})

		t.Run("nil reader", func(t *testing.T) {
			_, err := decode(nil)
			require.Error(t, err)
		})
	})
}

func TestTooLarge(t *testing.T) {
	p := syncproto.Pack{Objects: []syncproto.ObjectRecord{
		{Family: syncproto.Change, Key: "k", Bytes: payload(4 << 10)},
	}}
	body, err := syncproto.NewCodec().EncodePack(p)
	require.NoError(t, err)
	small := syncproto.NewCodec(syncproto.WithMaxDecodedSize(1024))

	for _, name := range []string{"DecodePack", "StreamPack"} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeWith(name, small, bytes.NewReader(body))
			require.ErrorIs(t, err, syncproto.ErrTooLarge)
			var de *syncproto.DecodeError
			require.False(t, errors.As(err, &de), "ErrTooLarge is not a DecodeError: %v", err)
		})
	}

	t.Run("DecodeWants", func(t *testing.T) {
		haves := make([]string, 200)
		for i := range haves {
			haves[i] = strings.Repeat("h", 10)
		}
		body, err := syncproto.NewCodec().EncodeWants(syncproto.All(haves))
		require.NoError(t, err)
		_, err = small.DecodeWants(bytes.NewReader(body))
		require.ErrorIs(t, err, syncproto.ErrTooLarge)
	})

	t.Run("trailing bytes past the limit", func(t *testing.T) {
		raw := append(mustHex(t, packGolden), make([]byte, 2048)...)
		body := zstdframe.Stored{}.Compress(nil, raw)
		for _, name := range []string{"DecodePack", "StreamPack"} {
			_, err := decodeWith(name, small, bytes.NewReader(body))
			require.ErrorIs(t, err, syncproto.ErrTooLarge, name)
		}
	})

	t.Run("a body exactly at the limit decodes", func(t *testing.T) {
		raw := mustHex(t, packGolden)
		at := syncproto.NewCodec(syncproto.WithMaxDecodedSize(int64(len(raw))))
		body := zstdframe.Stored{}.Compress(nil, raw)
		for _, name := range []string{"DecodePack", "StreamPack"} {
			got, err := decodeWith(name, at, bytes.NewReader(body))
			require.NoError(t, err, name)
			require.Equal(t, goldenPack(), got, name)
		}
		below := syncproto.NewCodec(syncproto.WithMaxDecodedSize(int64(len(raw) - 1)))
		for _, name := range []string{"DecodePack", "StreamPack"} {
			_, err := decodeWith(name, below, bytes.NewReader(body))
			require.ErrorIs(t, err, syncproto.ErrTooLarge, name)
		}
	})

	t.Run("a size that is not positive panics", func(t *testing.T) {
		require.Panics(t, func() { syncproto.NewCodec(syncproto.WithMaxDecodedSize(0)) })
		require.Panics(t, func() { syncproto.NewCodec(syncproto.WithMaxDecodedSize(-1)) })
	})
}

func TestDecodeWants(t *testing.T) {
	c := syncproto.NewCodec()
	for name, want := range map[string]syncproto.Wants{
		"advertise": syncproto.Advertise("dev"),
		"all":       syncproto.All([]string{"aa", "bb"}),
		"zero":      {},
	} {
		t.Run(name, func(t *testing.T) {
			body, err := c.EncodeWants(want)
			require.NoError(t, err)
			got, err := c.DecodeWants(bytes.NewReader(body))
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}

	t.Run("not zstd", func(t *testing.T) {
		_, err := c.DecodeWants(strings.NewReader("not zstd"))
		requireDecodeError(t, err, syncproto.StageZstd)
	})

	t.Run("a frame of 0xFF bytes", func(t *testing.T) {
		_, err := c.DecodeWants(bytes.NewReader(zstdframe.Stored{}.Compress(nil, bytes.Repeat([]byte{0xff}, 64))))
		requireDecodeError(t, err, syncproto.StagePostcard)
	})

	t.Run("a reader failing mid-body", func(t *testing.T) {
		boom := errors.New("boom")
		body, err := c.EncodeWants(syncproto.All([]string{hex.EncodeToString(payload(2 << 10))}))
		require.NoError(t, err)
		_, err = c.DecodeWants(io.MultiReader(bytes.NewReader(body[:len(body)/2]), iotest.ErrReader(boom)))
		require.ErrorIs(t, err, boom)
		var de *syncproto.DecodeError
		require.False(t, errors.As(err, &de))
	})
}

func TestDecodePackBytesAreCapped(t *testing.T) {
	c := syncproto.NewCodec()
	p := syncproto.Pack{Objects: []syncproto.ObjectRecord{
		{Family: syncproto.Change, Key: "a", Bytes: []byte("first")},
		{Family: syncproto.Change, Key: "b", Bytes: []byte("second")},
	}}
	body, err := c.EncodePack(p)
	require.NoError(t, err)
	got, err := c.DecodePack(bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, len(got.Objects[0].Bytes), cap(got.Objects[0].Bytes))
	_ = append(got.Objects[0].Bytes, "XXXXXXXXXXXX"...)
	require.Equal(t, p, got)
}

func TestStreamPack(t *testing.T) {
	c := syncproto.NewCodec()
	body, err := c.EncodePack(goldenPack())
	require.NoError(t, err)

	t.Run("objects in order, then the refs", func(t *testing.T) {
		p := syncproto.Pack{
			Objects: []syncproto.ObjectRecord{
				{Family: syncproto.Change, Key: "a", Bytes: []byte{1}},
				{Family: syncproto.Tag, Key: "b", Bytes: []byte{2}},
				{Family: syncproto.View, Key: "c", Bytes: []byte{3}},
			},
			Refs: goldenPack().Refs,
		}
		body, err := c.EncodePack(p)
		require.NoError(t, err)
		var keys []string
		refs, err := c.StreamPack(bytes.NewReader(body), func(o syncproto.ObjectRecord) error {
			keys = append(keys, o.Key)
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b", "c"}, keys)
		require.Equal(t, p.Refs, refs)
	})

	t.Run("stops at the first error from onObject", func(t *testing.T) {
		p := syncproto.Pack{Objects: []syncproto.ObjectRecord{
			{Family: syncproto.Change, Key: "a"},
			{Family: syncproto.Change, Key: "b"},
			{Family: syncproto.Change, Key: "c"},
		}}
		body, err := c.EncodePack(p)
		require.NoError(t, err)
		stop := errors.New("stop")
		var keys []string
		refs, err := c.StreamPack(bytes.NewReader(body), func(o syncproto.ObjectRecord) error {
			keys = append(keys, o.Key)
			if o.Key == "b" {
				return stop
			}
			return nil
		})
		require.Same(t, stop, err)
		require.Nil(t, refs)
		require.Equal(t, []string{"a", "b"}, keys)
	})

	t.Run("no refs is nil", func(t *testing.T) {
		body, err := c.EncodePack(syncproto.Pack{Objects: goldenPack().Objects})
		require.NoError(t, err)
		refs, err := c.StreamPack(bytes.NewReader(body), func(syncproto.ObjectRecord) error { return nil })
		require.NoError(t, err)
		require.Nil(t, refs)
	})

	t.Run("nil onObject", func(t *testing.T) {
		_, err := c.StreamPack(bytes.NewReader(body), nil)
		require.Error(t, err)
	})
}

func TestConcurrentEncode(t *testing.T) {
	c := syncproto.NewCodec()
	want, err := goldenPack().AppendPostcard(nil)
	require.NoError(t, err)

	type result struct {
		body []byte
		err  error
	}
	const n = 16
	results := make(chan result, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			body, err := c.EncodePack(goldenPack())
			results <- result{body, err}
		})
	}
	// Wait for every goroutine to exit, not just to send, so none is
	// still counted by TestNoGoroutines.
	defer wg.Wait()
	for range n {
		r := <-results
		require.NoError(t, r.err)
		require.Equal(t, want, decompress(t, r.body))
		got, err := c.DecodePack(bytes.NewReader(r.body))
		require.NoError(t, err)
		require.Equal(t, goldenPack(), got)
	}
}

func TestConcurrentDecode(t *testing.T) {
	c := syncproto.NewCodec()
	body, err := c.EncodePack(bigPack())
	require.NoError(t, err)

	type result struct {
		p   syncproto.Pack
		err error
	}
	const n = 16
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			var p syncproto.Pack
			var err error
			if i%2 == 0 {
				p, err = c.DecodePack(bytes.NewReader(body))
			} else {
				p, err = collect(c, bytes.NewReader(body))
			}
			results <- result{p, err}
		})
	}
	defer wg.Wait()
	for range n {
		r := <-results
		require.NoError(t, r.err)
		require.Equal(t, bigPack(), r.p)
	}
}

func TestNoGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	c := syncproto.NewCodec()
	body, err := c.EncodePack(bigPack())
	require.NoError(t, err)
	_, err = c.DecodePack(bytes.NewReader(body))
	require.NoError(t, err)
	_, err = collect(c, bytes.NewReader(body))
	require.NoError(t, err)
	wbody, err := c.EncodeWants(syncproto.Advertise("dev"))
	require.NoError(t, err)
	_, err = c.DecodeWants(bytes.NewReader(wbody))
	require.NoError(t, err)

	require.Equal(t, before, runtime.NumGoroutine())
}
