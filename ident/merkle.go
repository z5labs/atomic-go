package ident

// NextState returns the Merkle state after change is applied to a view
// whose state is state: the BLAKE3 hash of the 64 bytes state || change.
func NextState(state, change Hash) Hash {
	var buf [2 * Size]byte
	copy(buf[:Size], state[:])
	copy(buf[Size:], change[:])
	return Sum(buf[:])
}

// FoldState returns the Merkle state of a view whose change log is log, in
// log order, starting from the all-zero hash as Atomic's fold does. The
// order matters: the same changes in another order give another state.
//
// Merkle's doc comment in atomic-core describes a seed of BLAKE3 of the
// empty input instead; that question is open, and [FoldStateFrom] takes any
// seed.
func FoldState(log []Hash) Hash {
	return FoldStateFrom(Hash{}, log)
}

// FoldStateFrom folds log into seed with [NextState], in log order.
func FoldStateFrom(seed Hash, log []Hash) Hash {
	state := seed
	for _, change := range log {
		state = NextState(state, change)
	}
	return state
}

// ParseState parses a view's Merkle state as it appears on the wire. The
// text [EmptyState], "-", is an empty view, which has no state, and returns
// ok false with a nil error. Anything else is parsed with [ParseHash] and
// returns ok true; a failure is a *[ParseError] and returns ok false.
func ParseState(s string) (state Hash, ok bool, err error) {
	if s == EmptyState {
		return Hash{}, false, nil
	}
	h, err := decodeBase32Lenient("state", s)
	if err != nil {
		return Hash{}, false, err
	}
	return Hash(h), true, nil
}

// FormatState returns the wire form of a view's Merkle state: the base32
// text of state when ok is true, and [EmptyState] when it is false. It is
// the inverse of [ParseState].
func FormatState(state Hash, ok bool) string {
	if !ok {
		return EmptyState
	}
	return state.String()
}
