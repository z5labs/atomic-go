// Package chrono writes and reads timestamp text the way Atomic's Rust code
// gets it from the chrono crate.
//
// Change headers, tag records, registrations and delegation documents hash
// or sign that text, so a writer must produce it byte for byte. chrono
// writes a DateTime<Utc> as YYYY-MM-DDTHH:MM:SS, then a fraction of a
// second, then Z. The fraction is left out when the nanoseconds are 0, and
// otherwise has 3 digits for a whole number of milliseconds, 6 for a whole
// number of microseconds and 9 in every other case. Go's time.RFC3339Nano
// layout drops trailing zeros instead, so it would write .45135 where chrono
// writes .451350, and the hash would change.
//
// The text is defined for years 0000 through 9999, the range RFC 3339 covers.
package chrono

import (
	"errors"
	"fmt"
	"time"
)

// ErrFraction is returned by [Parse] when the fraction of a second does not
// have between 1 and 9 digits.
var ErrFraction = errors.New("chrono: fraction of a second must have 1 to 9 digits")

const (
	layoutSeconds = "2006-01-02T15:04:05Z"
	layoutMillis  = "2006-01-02T15:04:05.000Z"
	layoutMicros  = "2006-01-02T15:04:05.000000Z"
	layoutNanos   = "2006-01-02T15:04:05.000000000Z"
)

// Format returns t in UTC as chrono writes it: whole seconds, then no
// fraction when the nanoseconds are 0, 3 digits for a whole number of
// milliseconds, 6 for a whole number of microseconds and 9 otherwise, then Z.
func Format(t time.Time) string {
	t = t.UTC()
	ns := t.Nanosecond()
	switch {
	case ns == 0:
		return t.Format(layoutSeconds)
	case ns%int(time.Millisecond) == 0:
		return t.Format(layoutMillis)
	case ns%int(time.Microsecond) == 0:
		return t.Format(layoutMicros)
	default:
		return t.Format(layoutNanos)
	}
}

// FormatSeconds returns t in UTC truncated to whole seconds, with no
// fraction, as in 2026-11-24T09:30:00Z. Registrations and delegation
// documents sign timestamps in this form.
func FormatSeconds(t time.Time) string {
	return t.UTC().Format(layoutSeconds)
}

// Parse reads RFC 3339 text with an optional fraction of 1 to 9 digits and
// any UTC offset, and returns the instant it names in UTC. A fraction with
// more than 9 digits is an error wrapping [ErrFraction] rather than being
// truncated, because no writer of these timestamps produces one.
func Parse(s string) (time.Time, error) {
	if err := checkFraction(s); err != nil {
		return time.Time{}, fmt.Errorf("chrono: parse %q: %w", s, err)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("chrono: parse %q: %w", s, err)
	}
	return t.UTC(), nil
}

// checkFraction reports ErrFraction when s has a fraction of a second, after
// the 19 bytes of YYYY-MM-DDTHH:MM:SS, of 0 or more than 9 digits. Anything
// else wrong with s is left to time.Parse.
func checkFraction(s string) error {
	const secondsEnd = len("2006-01-02T15:04:05")
	if len(s) <= secondsEnd || s[secondsEnd] != '.' {
		return nil
	}
	n := 0
	for i := secondsEnd + 1; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n++
	}
	if n < 1 || n > 9 {
		return ErrFraction
	}
	return nil
}
