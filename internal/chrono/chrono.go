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

// ErrFraction is returned by [Parse], wrapped, when the fraction of a
// second does not have between 1 and 9 digits.
var ErrFraction = errors.New("fraction of a second must have 1 to 9 digits")

// ErrZone is returned by [Parse], wrapped, when the text after the seconds
// and the fraction is not Z or an offset from -23:59 to +23:59 written
// ±hh:mm.
var ErrZone = errors.New("time zone must be Z or an offset from -23:59 to +23:59")

const (
	layoutSeconds = "2006-01-02T15:04:05Z"
	layoutMillis  = "2006-01-02T15:04:05.000Z"
	layoutMicros  = "2006-01-02T15:04:05.000000Z"
	layoutNanos   = "2006-01-02T15:04:05.000000000Z"
)

// Format returns t in UTC as chrono writes it: whole seconds, then no
// fraction when the nanoseconds are 0, 3 digits for a whole number of
// milliseconds, 6 for a whole number of microseconds and 9 otherwise, then Z.
//
// The text matches chrono's only for years 0000 through 9999. Outside that
// range chrono writes a sign before the year and Format does not.
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
// any UTC offset, and returns the instant it names in UTC.
//
// It is stricter than time.Parse, which also accepts a comma before the
// fraction, truncates a fraction past 9 digits and accepts offsets such as
// +24:00. Parse rejects those: a fraction of 0 or more than 9 digits is an
// error wrapping [ErrFraction], and anything after the seconds other than a
// '.' fraction followed by Z or ±hh:mm, with hh at most 23 and mm at most
// 59, is an error wrapping [ErrZone].
func Parse(s string) (time.Time, error) {
	if err := checkTail(s); err != nil {
		return time.Time{}, fmt.Errorf("chrono: parse %q: %w", s, err)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("chrono: %w", err)
	}
	return t.UTC(), nil
}

// checkTail checks what follows the 19 bytes of YYYY-MM-DDTHH:MM:SS: an
// optional '.' and 1 to 9 digits, then Z or ±hh:mm within a day. Text too
// short to have a tail is left to time.Parse, as is everything before it.
func checkTail(s string) error {
	const secondsEnd = len("2006-01-02T15:04:05")
	if len(s) < secondsEnd {
		return nil
	}
	tail := s[secondsEnd:]
	if len(tail) > 0 && tail[0] == '.' {
		n := 0
		for n+1 < len(tail) && isDigit(tail[n+1]) {
			n++
		}
		if n < 1 || n > 9 {
			return ErrFraction
		}
		tail = tail[1+n:]
	}
	if tail == "Z" {
		return nil
	}
	if len(tail) != len("+hh:mm") || (tail[0] != '+' && tail[0] != '-') || tail[3] != ':' ||
		!isDigit(tail[1]) || !isDigit(tail[2]) || !isDigit(tail[4]) || !isDigit(tail[5]) {
		return ErrZone
	}
	hh := int(tail[1]-'0')*10 + int(tail[2]-'0')
	mm := int(tail[4]-'0')*10 + int(tail[5]-'0')
	if hh > 23 || mm > 59 {
		return ErrZone
	}
	return nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
