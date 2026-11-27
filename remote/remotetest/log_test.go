package remotetest_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/remotetest"
)

func attrsOf(rec slog.Record) map[string]slog.Value {
	out := map[string]slog.Value{}
	rec.Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value
		return true
	})
	return out
}

func TestLogRecorder(t *testing.T) {
	t.Run("captures a Warn from a logger derived with With", func(t *testing.T) {
		rec := remotetest.NewLogRecorder()

		rec.Logger().With("k", "v").Warn("upgrade available", "version", "1.2.3")

		records := rec.Records()
		require.Len(t, records, 1)
		require.Equal(t, slog.LevelWarn, records[0].Level)
		require.Equal(t, "upgrade available", records[0].Message)

		attrs := attrsOf(records[0])
		require.Len(t, attrs, 2)
		require.Equal(t, "v", attrs["k"].String())
		require.Equal(t, "1.2.3", attrs["version"].String())
	})

	t.Run("records at every level", func(t *testing.T) {
		rec := remotetest.NewLogRecorder()
		logger := rec.Logger()

		logger.Debug("debug")
		logger.Info("info")
		logger.Error("error")

		records := rec.Records()
		require.Len(t, records, 3)
		require.Equal(t, slog.LevelDebug, records[0].Level)
		require.Equal(t, slog.LevelInfo, records[1].Level)
		require.Equal(t, slog.LevelError, records[2].Level)
	})

	t.Run("derived loggers record into the same list with their attributes", func(t *testing.T) {
		rec := remotetest.NewLogRecorder()
		root := rec.Logger()
		grouped := root.With("a", 1).WithGroup("g").With("b", 2)

		root.Info("root")
		grouped.Info("grouped", "c", 3)
		grouped.WithGroup("empty").Info("no attrs in the inner group")

		records := rec.Records()
		require.Len(t, records, 3)

		require.Equal(t, "root", records[0].Message)
		require.Empty(t, attrsOf(records[0]))

		require.Equal(t, "grouped", records[1].Message)
		attrs := attrsOf(records[1])
		require.Len(t, attrs, 2)
		require.Equal(t, int64(1), attrs["a"].Int64())
		require.Equal(t, slog.KindGroup, attrs["g"].Kind())
		require.Equal(t, []slog.Attr{slog.Int("b", 2), slog.Int("c", 3)}, attrs["g"].Group())

		attrs = attrsOf(records[2])
		require.Len(t, attrs, 2)
		require.Equal(t, int64(1), attrs["a"].Int64())
		require.Equal(t, []slog.Attr{slog.Int("b", 2)}, attrs["g"].Group())
	})

	t.Run("an empty group name and no attrs return the same handler", func(t *testing.T) {
		rec := remotetest.NewLogRecorder()
		require.Same(t, rec, rec.WithGroup(""))
		require.Same(t, rec, rec.WithAttrs(nil))
	})

	t.Run("Records returns a copy", func(t *testing.T) {
		rec := remotetest.NewLogRecorder()
		rec.Logger().Info("first")

		records := rec.Records()
		records[0].AddAttrs(slog.String("added", "later"))

		require.Empty(t, attrsOf(rec.Records()[0]))
	})

	t.Run("is safe for concurrent use", func(t *testing.T) {
		const n = 32
		rec := remotetest.NewLogRecorder()
		logger := rec.Logger()

		done := make(chan struct{}, n)
		for i := range n {
			go func() {
				logger.With("i", i).WithGroup("g").Info("concurrent", "j", i)
				rec.Records()
				done <- struct{}{}
			}()
		}
		for range n {
			<-done
		}

		records := rec.Records()
		require.Len(t, records, n)
		seen := map[int64]bool{}
		for _, r := range records {
			seen[attrsOf(r)["i"].Int64()] = true
		}
		require.Len(t, seen, n)
	})
}
