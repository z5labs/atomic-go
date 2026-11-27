package syncproto

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/z5labs/atomic-go/internal/postcard"
	"github.com/z5labs/atomic-go/zstdframe"
)

// level is the zstd level Atomic compresses sync bodies at.
const level = 3

// Decode stages, the values of [DecodeError.Stage].
const (
	// StageZstd is a body that is not a valid zstd stream.
	StageZstd = "zstd"

	// StagePostcard is a body that decompresses but is not a valid
	// postcard value of the type asked for.
	StagePostcard = "postcard"
)

// ErrTooLarge reports a body that inflates past a [Codec]'s limit. It is
// not a [DecodeError]: Atomic reports it as a protocol error, where a
// DecodeError suggests a client and server on incompatible versions.
var ErrTooLarge = errors.New("syncproto: decoded body exceeds the size limit")

// DecodeError reports a body that could not be decoded: one that is not
// valid zstd, or that decompresses to something that is not the postcard
// value asked for. Atomic reports either as a version mismatch, since an
// undecodable body almost always means the server runs a different
// release.
//
// A failure to read the body, and a body past the size limit, are not
// DecodeErrors.
type DecodeError struct {
	// Stage is the layer that failed, [StageZstd] or [StagePostcard].
	Stage string

	// Err is that layer's error.
	Err error
}

func (e *DecodeError) Error() string {
	return "syncproto: " + e.Stage + " decode: " + e.Err.Error()
}

func (e *DecodeError) Unwrap() error {
	return e.Err
}

// Codec encodes and decodes sync bodies. It is safe for concurrent use, and
// starts no goroutines.
type Codec struct {
	maxDecoded int64
	enc        *zstdframe.Encoder
}

// Option configures a [Codec].
type Option func(*Codec)

// WithMaxDecodedSize sets the most bytes a body may inflate to before a
// decode fails with [ErrTooLarge]. The default is [MaxDecodedSize]. n must
// be positive.
func WithMaxDecodedSize(n int64) Option {
	return func(c *Codec) {
		c.maxDecoded = n
	}
}

// NewCodec returns a Codec configured by opts. It panics if
// [WithMaxDecodedSize] was given a size that is not positive.
func NewCodec(opts ...Option) *Codec {
	c := &Codec{maxDecoded: MaxDecodedSize}
	for _, opt := range opts {
		opt(c)
	}
	if c.maxDecoded < 1 {
		panic(fmt.Sprintf("syncproto: max decoded size %d is not positive", c.maxDecoded))
	}
	enc, err := zstdframe.NewEncoder(level)
	if err != nil {
		// Only a level outside 1 to 22 fails, and level is 3.
		panic(fmt.Sprintf("syncproto: %v", err))
	}
	c.enc = enc
	return c
}

// EncodePack returns p as a sync body: its postcard encoding, compressed as
// one zstd frame at level 3. It fails as [Pack.AppendPostcard] does.
func (c *Codec) EncodePack(p Pack) ([]byte, error) {
	raw, err := p.AppendPostcard(nil)
	if err != nil {
		return nil, err
	}
	return c.enc.Compress(nil, raw), nil
}

// EncodeWants returns w as a sync body: its postcard encoding, compressed
// as one zstd frame at level 3. It fails as [Wants.AppendPostcard] does.
func (c *Codec) EncodeWants(w Wants) ([]byte, error) {
	raw, err := w.AppendPostcard(nil)
	if err != nil {
		return nil, err
	}
	return c.enc.Compress(nil, raw), nil
}

// DecodePack reads a sync body from r to its end and decodes the Pack in
// it. Bytes after the pack are ignored. The whole decoded body is held in
// memory; [Codec.StreamPack] holds one object at a time.
//
// A body that inflates past the Codec's limit is [ErrTooLarge]; one that is
// not valid zstd, or not a postcard Pack, is a *[DecodeError]. An error
// from r is returned wrapped, so [errors.Is] reaches it, and is neither.
func (c *Codec) DecodePack(r io.Reader) (Pack, error) {
	buf, err := c.readAll(r)
	if err != nil {
		return Pack{}, err
	}
	return unmarshalPack(buf, false)
}

// DecodeWants reads a sync body from r to its end and decodes the Wants in
// it. It fails as [Codec.DecodePack] does.
func (c *Codec) DecodeWants(r io.Reader) (Wants, error) {
	buf, err := c.readAll(r)
	if err != nil {
		return Wants{}, err
	}
	var w Wants
	if err := w.UnmarshalPostcard(buf); err != nil {
		return Wants{}, err
	}
	return w, nil
}

func (c *Codec) readAll(r io.Reader) ([]byte, error) {
	b, err := c.open(r)
	if err != nil {
		return nil, err
	}
	defer b.close()
	buf, err := io.ReadAll(b.out)
	if err != nil {
		return nil, b.fail(err)
	}
	return buf, nil
}

// StreamPack reads a sync body from r and decodes the Pack in it one object
// at a time, calling onObject with each object in order, and returns the
// pack's refs. Only one object is held in memory at a time, so a large pull
// costs no more than its largest object.
//
// If onObject returns an error, StreamPack stops at once and returns that
// error unchanged. Otherwise StreamPack reads r to its end and fails as
// [Codec.DecodePack] does. Objects are passed to onObject as they are
// decoded, so a body found to be bad after some objects were passed to it
// has still had them passed.
func (c *Codec) StreamPack(r io.Reader, onObject func(ObjectRecord) error) ([]RefRecord, error) {
	if onObject == nil {
		return nil, errors.New("syncproto: stream pack: nil onObject")
	}
	b, err := c.open(r)
	if err != nil {
		return nil, err
	}
	defer b.close()

	br := bufio.NewReader(b.out)
	pr := postcard.NewReader(br, int(min(c.maxDecoded, math.MaxInt)))
	n := pr.SeqLen()
	for i := 0; i < n && pr.Err() == nil; i++ {
		o := ObjectRecord{
			Family: Family(pr.U32()),
			Key:    pr.Str(),
			Bytes:  pr.ByteSeq(),
		}
		if pr.Err() != nil {
			break
		}
		if err := onObject(o); err != nil {
			return nil, err
		}
	}
	var refs []RefRecord
	n = pr.SeqLen()
	for i := 0; i < n && pr.Err() == nil; i++ {
		ref := RefRecord{Name: pr.Str()}
		if pr.OptionTag() {
			old := pr.Str()
			ref.ExpectOld = &old
		}
		ref.NewTarget = pr.Str()
		if pr.Err() == nil {
			refs = append(refs, ref)
		}
	}

	// Read the rest of the body, so a body that is too large or not valid
	// zstd past the pack fails as it does for DecodePack, which decompresses
	// all of it before decoding any.
	_, drainErr := io.Copy(io.Discard, br)
	if err := pr.Err(); err != nil {
		return nil, b.fail(err)
	}
	if drainErr != nil {
		return nil, b.fail(drainErr)
	}
	return refs, nil
}

// body is one sync body being decompressed. out is the decoded stream; the
// recorders under it say which layer a failure came from.
type body struct {
	src *errRecorder
	dec *errRecorder
	zr  *zstdframe.Reader
	lim *limitReader
	out io.Reader
}

func (c *Codec) open(r io.Reader) (*body, error) {
	if r == nil {
		return nil, errors.New("syncproto: nil io.Reader")
	}
	b := &body{src: &errRecorder{r: r}}
	// The limit is applied above the decoder rather than by it, so that
	// zstdframe.ErrLimit can only mean a frame the decoder will not hold,
	// which is a zstd failure, not a body past the limit.
	zr, err := zstdframe.NewReader(b.src, math.MaxInt64)
	if err != nil {
		return nil, &DecodeError{Stage: StageZstd, Err: err}
	}
	b.zr = zr
	b.dec = &errRecorder{r: zr}
	b.lim = &limitReader{r: b.dec, limit: c.maxDecoded}
	b.out = b.lim
	return b, nil
}

func (b *body) close() {
	_ = b.zr.Close()
}

// fail turns err, the failure of a read from out or of the postcard decode
// above it, into the error a decode returns, by the layer it came from.
func (b *body) fail(err error) error {
	switch {
	case b.src.err != nil:
		return fmt.Errorf("syncproto: read body: %w", b.src.err)
	case b.lim.passed:
		return fmt.Errorf("%w: more than %d bytes", ErrTooLarge, b.lim.limit)
	case b.dec.err != nil:
		return &DecodeError{Stage: StageZstd, Err: b.dec.err}
	}
	return &DecodeError{Stage: StagePostcard, Err: err}
}

// errRecorder remembers the first error other than io.EOF that r returned.
type errRecorder struct {
	r   io.Reader
	err error
}

func (e *errRecorder) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// errPassed is what a limitReader returns once its input passes its limit.
var errPassed = errors.New("syncproto: decoded output passes the limit")

// limitReader passes on at most limit bytes of r. A Read that would pass
// the limit returns the bytes up to it and errPassed, as does every Read
// after it.
type limitReader struct {
	r      io.Reader
	limit  int64
	n      int64
	passed bool
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.passed {
		return 0, errPassed
	}
	if len(p) == 0 {
		return 0, nil
	}
	remaining := l.limit - l.n
	if int64(len(p)) > remaining {
		// One byte past the limit is enough to tell that r passes it.
		p = p[:remaining+1]
	}
	n, err := l.r.Read(p)
	if int64(n) > remaining {
		l.n = l.limit
		l.passed = true
		return int(remaining), errPassed
	}
	l.n += int64(n)
	return n, err
}
