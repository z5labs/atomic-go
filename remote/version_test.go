package remote_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
)

func TestNeedsUpgrade(t *testing.T) {
	// The first nine rows are the Rust client's needs_upgrade tests
	// (docs/remote/transport.md#x-atomic-min-version).
	for _, tc := range []struct {
		current, required string
		want              bool
	}{
		{"0.9.0", "1.0.0", true},
		{"1.2.3", "1.2.3", false},
		{"2.0.0", "1.9.9", false},
		{"1.1.0", "1.2.0", true},
		{"1.2.2", "1.2.3", true},
		{"1.2.4", "1.2.3", false},
		{"1.2", "1.2.0", false},
		{"dev", "1.0.0", false},
		{"1.0.0", "not-a-version", false},

		// An empty part, a "v" prefix and a pre-release suffix do not
		// parse, so they never ask for an upgrade.
		{"1..2", "9.0.0", false},
		{"v1.0.0", "2.0.0", false},
		{"1.2.3-rc1", "2.0.0", false},
		{"1.0.0", "1..2", false},
		{"1.0.0", "v2.0.0", false},
		{"1.0.0", "2.0.3-rc1", false},

		// Missing parts count as 0 and a fourth part is ignored.
		{"1", "1.0.1", true},
		{"1.2.3.9", "1.2.3", false},
		{" 1.2.3\n", "1.2.4", true},
	} {
		t.Run(tc.current+" vs "+tc.required, func(t *testing.T) {
			require.Equal(t, tc.want, remote.NeedsUpgrade(tc.current, tc.required))
		})
	}
}

func TestParseVersion(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, tc := range []struct {
			in   string
			want remote.Version
		}{
			{"0.22.1", remote.Version{Major: 0, Minor: 22, Patch: 1}},
			{"1.2", remote.Version{Major: 1, Minor: 2}},
			{"7", remote.Version{Major: 7}},
			{"1.2.3.4", remote.Version{Major: 1, Minor: 2, Patch: 3}},
			{"1.2.3.x.y", remote.Version{Major: 1, Minor: 2, Patch: 3}},
			{"  1.2.3  ", remote.Version{Major: 1, Minor: 2, Patch: 3}},
			{"01.002.3", remote.Version{Major: 1, Minor: 2, Patch: 3}},
			{"+1.2.3", remote.Version{Major: 1, Minor: 2, Patch: 3}},
			{"4294967295.0.0", remote.Version{Major: 4294967295}},
		} {
			t.Run(tc.in, func(t *testing.T) {
				got, err := remote.ParseVersion(tc.in)
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			})
		}
	})

	t.Run("refuses", func(t *testing.T) {
		for _, in := range []string{
			"", " ", "1..2", "v1.0.0", "3-rc1", "1.2.3-rc1", "1.2.", ".1",
			"dev", "-1.0.0", "++1", "+", "4294967296.0.0", "1.2 .3", "1_0.0",
		} {
			t.Run(in, func(t *testing.T) {
				_, err := remote.ParseVersion(in)
				require.ErrorIs(t, err, remote.ErrVersion)
			})
		}
	})
}

func TestVersionCompare(t *testing.T) {
	v := func(major, minor, patch uint32) remote.Version {
		return remote.Version{Major: major, Minor: minor, Patch: patch}
	}
	for _, tc := range []struct {
		name string
		a, b remote.Version
		want int
	}{
		{"equal", v(1, 2, 3), v(1, 2, 3), 0},
		{"major decides", v(2, 0, 0), v(1, 9, 9), 1},
		{"minor decides", v(1, 1, 9), v(1, 2, 0), -1},
		{"patch decides", v(1, 2, 4), v(1, 2, 3), 1},
		{"zero", v(0, 0, 0), v(0, 0, 1), -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.a.Compare(tc.b))
			require.Equal(t, -tc.want, tc.b.Compare(tc.a))
		})
	}
}

func TestVersionString(t *testing.T) {
	require.Equal(t, "0.22.1", remote.Version{Minor: 22, Patch: 1}.String())
}
