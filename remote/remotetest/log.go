package remotetest

import (
	"context"
	"log/slog"
	"slices"
	"sync"
)

// LogRecorder is a [slog.Handler] that keeps every record it handles, at
// every level, so a test can assert on what was logged.
//
// A LogRecorder is safe for concurrent use. Handlers derived from it with
// WithAttrs and WithGroup, including those behind a logger's With and
// WithGroup, record into the same list, and each record carries the
// attributes and groups of the handler that recorded it.
type LogRecorder struct {
	store *recordStore
	// chain is the handler's WithAttrs and WithGroup calls, outermost
	// first. It is never appended to in place, so derived handlers can
	// share a prefix of it.
	chain []groupOrAttrs
}

// NewLogRecorder returns an empty LogRecorder.
func NewLogRecorder() *LogRecorder {
	return &LogRecorder{store: &recordStore{}}
}

// Logger returns a logger that writes to r.
func (r *LogRecorder) Logger() *slog.Logger {
	return slog.New(r)
}

// Records returns a copy of every record handled so far, by r or by any
// handler derived from it, in the order they were handled.
func (r *LogRecorder) Records() []slog.Record {
	return r.store.snapshot()
}

// Enabled reports true for every level.
func (r *LogRecorder) Enabled(context.Context, slog.Level) bool {
	return true
}

// Handle records rec, with the attributes and groups of r added to it.
func (r *LogRecorder) Handle(_ context.Context, rec slog.Record) error {
	attrs := make([]slog.Attr, 0, rec.NumAttrs())
	rec.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	// Wrap the record's own attributes in the handler's chain from the
	// innermost call outwards, so each group holds everything logged after
	// it was opened.
	for i := len(r.chain) - 1; i >= 0; i-- {
		goa := r.chain[i]
		if goa.group == "" {
			attrs = append(slices.Clip(goa.attrs), attrs...)
			continue
		}
		if len(attrs) == 0 {
			// slog drops a group that holds nothing.
			continue
		}
		attrs = []slog.Attr{{Key: goa.group, Value: slog.GroupValue(attrs...)}}
	}

	out := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	out.AddAttrs(attrs...)
	r.store.add(out)
	return nil
}

// WithAttrs returns a handler that records into the same list as r, adding
// attrs to every record.
func (r *LogRecorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return r
	}
	return r.with(groupOrAttrs{attrs: slices.Clone(attrs)})
}

// WithGroup returns a handler that records into the same list as r, with
// every attribute added after it nested under name. An empty name returns r.
func (r *LogRecorder) WithGroup(name string) slog.Handler {
	if name == "" {
		return r
	}
	return r.with(groupOrAttrs{group: name})
}

func (r *LogRecorder) with(goa groupOrAttrs) *LogRecorder {
	return &LogRecorder{
		store: r.store,
		chain: append(slices.Clip(r.chain), goa),
	}
}

// groupOrAttrs is one WithGroup call, when group is set, or one WithAttrs
// call otherwise.
type groupOrAttrs struct {
	group string
	attrs []slog.Attr
}

// recordStore is the list every handler derived from one LogRecorder shares.
type recordStore struct {
	mu      sync.Mutex
	records []slog.Record
}

func (s *recordStore) add(rec slog.Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, rec)
}

func (s *recordStore) snapshot() []slog.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]slog.Record, len(s.records))
	for i, rec := range s.records {
		out[i] = rec.Clone()
	}
	return out
}
