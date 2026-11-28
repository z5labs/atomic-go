package delegation_test

import (
	"encoding/hex"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/delegation"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
)

// The identity ids of RFC 8032 §7.1 TEST 1 and TEST 2, from identity's
// tests.
const (
	delegatorID = "NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA"
	delegateID  = "CAT6ANNSNNQF3RWUW6GQPXBJMYH4YNEYWWMKFZL4JZVRWZZ2D2KQ"
)

// goldenID is NewID(delegatorID, delegateID, 2026-11-24T09:30:00Z), frozen
// from the first run.
const goldenID = "7XFVC6H4XTSGCGH7NTOIWXLWVFZPTCO5LCK4R5C7JA2VBT47WNBQ"

func mustIdentityID(t *testing.T, s string) identity.ID {
	t.Helper()
	id, err := identity.ParseID(s)
	require.NoError(t, err)
	return id
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func goldenIssued() time.Time {
	return time.Date(2026, time.November, 24, 9, 30, 0, 0, time.UTC)
}

func TestNewIDGolden(t *testing.T) {
	id := delegation.NewID(mustIdentityID(t, delegatorID), mustIdentityID(t, delegateID), goldenIssued())
	require.Equal(t, goldenID, id.Base32())
	require.Equal(t, "urn:atomic:delegation:"+goldenID, id.URN())
}

func TestNewIDHashInput(t *testing.T) {
	// The hash input, written out byte for byte
	// (docs/remote/auth.md#delegation-ids).
	input := strings.Join([]string{
		// "atomic:delegation:v1"
		"61746f6d69633a64656c65676174696f6e3a7631",
		// the delegator's identity id
		"6c31041268f471609c79f5f2dbcc38e4a4ab2f4d416109a4e09fcf50fd0f0062",
		// the delegate's identity id
		"1027e035b26b605dc6d4b78d07dc29660fcc3498b598a2e57c4e6b1b673a1e95",
		// 1795512600, 2026-11-24T09:30:00Z, as a little-endian int64
		"1859056b00000000",
	}, "")
	want := ident.Sum(mustHex(t, input))

	got := delegation.NewID(mustIdentityID(t, delegatorID), mustIdentityID(t, delegateID), goldenIssued())
	require.Equal(t, [32]byte(want), [32]byte(got))
}

func TestNewIDInputs(t *testing.T) {
	a, b := mustIdentityID(t, delegatorID), mustIdentityID(t, delegateID)
	issued := goldenIssued()
	id := delegation.NewID(a, b, issued)

	require.Equal(t, id, delegation.NewID(a, b, issued.Add(999*time.Millisecond)), "sub-second time is ignored")
	require.Equal(t, id, delegation.NewID(a, b, issued.In(time.FixedZone("x", 3600))), "the zone is ignored")
	require.NotEqual(t, id, delegation.NewID(b, a, issued), "the order of the ids matters")
	require.NotEqual(t, id, delegation.NewID(a, b, issued.Add(time.Second)))

	// Seconds before 1970 are a negative int64, in two's complement.
	before := time.Date(1969, time.December, 31, 23, 59, 59, 0, time.UTC)
	input := append([]byte(delegation.IDDomain), a[:]...)
	input = append(input, b[:]...)
	input = append(input, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff)
	require.Equal(t, [32]byte(ident.Sum(input)), [32]byte(delegation.NewID(a, b, before)))
}

func TestParseID(t *testing.T) {
	want := delegation.NewID(mustIdentityID(t, delegatorID), mustIdentityID(t, delegateID), goldenIssued())

	for _, in := range []string{goldenID, delegation.URNPrefix + goldenID} {
		got, err := delegation.ParseID(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}

	for name, in := range map[string]string{
		"empty":               "",
		"prefix alone":        delegation.URNPrefix,
		"lower case":          strings.ToLower(goldenID),
		"lower-case urn":      delegation.URNPrefix + strings.ToLower(goldenID),
		"upper-case prefix":   strings.ToUpper(delegation.URNPrefix) + goldenID,
		"padded":              goldenID + "====",
		"short":               goldenID[:51],
		"long":                goldenID + "A",
		"space":               " " + goldenID,
		"not base32":          "not base32!",
		"other urn":           "urn:atomic:identity:" + goldenID,
		"prefix twice":        delegation.URNPrefix + delegation.URNPrefix + goldenID,
		"nonzero unused bits": goldenID[:51] + "B",
	} {
		_, err := delegation.ParseID(in)
		require.ErrorIs(t, err, delegation.ErrInvalidID, name)
	}

	_, err := delegation.ParseID(strings.ToLower(goldenID))
	var pe *ident.ParseError
	require.ErrorAs(t, err, &pe)
}

func TestIDText(t *testing.T) {
	id := delegation.NewID(mustIdentityID(t, delegatorID), mustIdentityID(t, delegateID), goldenIssued())
	urn := delegation.URNPrefix + goldenID

	require.Equal(t, urn, id.String())
	require.Len(t, id.Base32(), ident.Base32Len)

	b, err := id.MarshalText()
	require.NoError(t, err)
	require.Equal(t, urn, string(b))

	b, err = id.AppendText([]byte("x="))
	require.NoError(t, err)
	require.Equal(t, "x="+urn, string(b))

	for _, in := range []string{urn, goldenID} {
		var got delegation.ID
		require.NoError(t, got.UnmarshalText([]byte(in)))
		require.Equal(t, id, got)
	}

	got := id
	require.ErrorIs(t, got.UnmarshalText([]byte(strings.ToLower(goldenID))), delegation.ErrInvalidID)
	require.Equal(t, id, got, "unchanged on failure")

	j, err := json.Marshal(map[string]delegation.ID{"@id": id})
	require.NoError(t, err)
	require.Equal(t, `{"@id":"`+urn+`"}`, string(j))

	var m map[string]delegation.ID
	require.NoError(t, json.Unmarshal(j, &m))
	require.Equal(t, id, m["@id"])
}
