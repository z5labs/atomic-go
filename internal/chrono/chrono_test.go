package chrono_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/internal/chrono"
)

func TestFormat(t *testing.T) {
	plus2 := time.FixedZone("+02:00", 2*60*60)
	base := time.Date(2026, 11, 12, 16, 23, 20, 0, time.UTC)

	for _, tc := range []struct {
		name string
		in   time.Time
		want string
	}{
		{"whole seconds have no fraction", time.Unix(1700000000, 0), "2023-11-14T22:13:20Z"},
		{"whole microseconds keep 6 digits", base.Add(451350 * time.Microsecond), "2026-11-12T16:23:20.451350Z"},
		{"whole milliseconds give 3 digits", base.Add(451 * time.Millisecond), "2026-11-12T16:23:20.451Z"},
		{"one nanosecond gives 9 digits", base.Add(time.Nanosecond), "2026-11-12T16:23:20.000000001Z"},
		{"one millisecond keeps leading zeros", base.Add(time.Millisecond), "2026-11-12T16:23:20.001Z"},
		{"one microsecond keeps leading zeros", base.Add(time.Microsecond), "2026-11-12T16:23:20.000001Z"},
		{"non-UTC input is converted", time.Date(2026, 11, 24, 11, 30, 0, 0, plus2), "2026-11-24T09:30:00Z"},
		{"non-UTC input with a fraction is converted", time.Date(2026, 11, 24, 11, 30, 0, 750_000_000, plus2), "2026-11-24T09:30:00.750Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, chrono.Format(tc.in))
		})
	}
}

func TestFormatSeconds(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   time.Time
		want string
	}{
		{"truncates milliseconds", time.Date(2026, 11, 24, 9, 30, 0, 750_000_000, time.UTC), "2026-11-24T09:30:00Z"},
		{"truncates rather than rounds", time.Date(2026, 11, 24, 9, 30, 0, 999_999_999, time.UTC), "2026-11-24T09:30:00Z"},
		{"converts to UTC", time.Date(2026, 11, 24, 11, 30, 0, 750_000_000, time.FixedZone("+02:00", 2*60*60)), "2026-11-24T09:30:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, chrono.FormatSeconds(tc.in))
		})
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want time.Time
	}{
		{"offset is converted to UTC", "2026-11-24T11:30:00+02:00", time.Date(2026, 11, 24, 9, 30, 0, 0, time.UTC)},
		{"negative offset is converted to UTC", "2026-11-24T04:00:00-05:30", time.Date(2026, 11, 24, 9, 30, 0, 0, time.UTC)},
		{"Z", "2023-11-14T22:13:20Z", time.Unix(1700000000, 0).UTC()},
		{"6 digits", "2026-11-12T16:23:20.451350Z", time.Date(2026, 11, 12, 16, 23, 20, 451_350_000, time.UTC)},
		{"fraction with an offset", "2026-11-24T11:30:00.5+02:00", time.Date(2026, 11, 24, 9, 30, 0, 500_000_000, time.UTC)},
		{"largest offset", "2026-11-24T23:59:00+23:59", time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)},
		{"smallest offset", "2026-11-24T00:00:00-23:59", time.Date(2026, 11, 24, 23, 59, 0, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chrono.Parse(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, time.UTC, got.Location())
		})
	}
}

func TestParseFractionLengths(t *testing.T) {
	// Each length's digits are the leading digits of 123456789, so length n
	// names 123456789 truncated to n digits, scaled to nanoseconds.
	const digits = "123456789"
	scale := 100_000_000
	ns := 0
	for n := 1; n <= 9; n++ {
		ns += int(digits[n-1]-'0') * scale
		scale /= 10
		in := "2026-11-12T16:23:20." + digits[:n] + "Z"
		t.Run(in, func(t *testing.T) {
			got, err := chrono.Parse(in)
			require.NoError(t, err)
			require.Equal(t, time.Date(2026, 11, 12, 16, 23, 20, ns, time.UTC), got)
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		is   error
	}{
		{"10 fraction digits", "2026-11-12T16:23:20.1234567890Z", chrono.ErrFraction},
		{"empty fraction", "2026-11-12T16:23:20.Z", chrono.ErrFraction},
		{"comma decimal mark", "2026-11-12T16:23:20,5Z", chrono.ErrZone},
		{"comma decimal mark with 10 digits", "2026-11-12T16:23:20,1234567890Z", chrono.ErrZone},
		{"offset hour 24", "2026-11-12T16:23:20.5+24:00", chrono.ErrZone},
		{"offset minute 60", "2026-11-12T16:23:20+23:60", chrono.ErrZone},
		{"offset without colon", "2026-11-12T16:23:20+0200", chrono.ErrZone},
		{"lower-case z", "2026-11-12T16:23:20z", chrono.ErrZone},
		{"no zone", "2026-11-12T16:23:20", chrono.ErrZone},
		{"date only", "2026-11-12", nil},
		{"empty", "", nil},
		{"space separator", "2026-11-12 16:23:20Z", nil},
		{"month out of range", "2026-13-12T16:23:20Z", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := chrono.Parse(tc.in)
			require.Error(t, err)
			if tc.is != nil {
				require.ErrorIs(t, err, tc.is)
				return
			}
			require.NotErrorIs(t, err, chrono.ErrFraction)
			require.NotErrorIs(t, err, chrono.ErrZone)
		})
	}
}

func TestRoundTrip(t *testing.T) {
	base := time.Date(2026, 11, 12, 16, 23, 20, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		ns     int
		digits int
	}{
		{"no fraction", 0, 0},
		{"3 digits", 451_000_000, 3},
		{"6 digits", 451_350_000, 6},
		{"9 digits", 451_350_001, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := base.Add(time.Duration(tc.ns))
			s := chrono.Format(in)
			if tc.digits == 0 {
				require.NotContains(t, s, ".")
			} else {
				require.Len(t, s[strings.Index(s, ".")+1:len(s)-1], tc.digits)
			}

			got, err := chrono.Parse(s)
			require.NoError(t, err)
			require.True(t, in.Equal(got))
			require.Equal(t, s, chrono.Format(got))
		})
	}

	// Text of every accepted fraction length parses to the same instant and
	// formats back in the canonical length for its value.
	for _, tc := range []struct {
		in, want string
	}{
		{".7", ".700"},
		{".77", ".770"},
		{".777", ".777"},
		{".7777", ".777700"},
		{".77777", ".777770"},
		{".777777", ".777777"},
		{".7777777", ".777777700"},
		{".77777777", ".777777770"},
		{".777777777", ".777777777"},
	} {
		in := "2026-11-12T16:23:20" + tc.in + "Z"
		t.Run(in, func(t *testing.T) {
			parsed, err := chrono.Parse(in)
			require.NoError(t, err)
			s := chrono.Format(parsed)
			require.Equal(t, "2026-11-12T16:23:20"+tc.want+"Z", s)
			again, err := chrono.Parse(s)
			require.NoError(t, err)
			require.Equal(t, parsed, again)
		})
	}
}
