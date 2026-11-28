package change_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/change"
)

func sha1ID(b byte) change.GitObjectID {
	return change.GitObjectID{Bytes: bytes.Repeat([]byte{b}, change.SHA1Size)}
}

func sha256ID(b byte) change.GitObjectID {
	return change.GitObjectID{SHA256: true, Bytes: bytes.Repeat([]byte{b}, change.SHA256Size)}
}

func synthesized(d change.GitDerivation, commit change.GitObjectID, parents ...change.GitObjectID) change.Origin {
	return change.Origin{Kind: change.GitSynthesized, Commit: commit, Parents: parents, Derivation: d}
}

func TestOriginValidate(t *testing.T) {
	// docs/remote/change-format/sections.md#wiregitderivationv2 and
	// #wiregitobjectidv2.
	testCases := []struct {
		name   string
		origin change.Origin
		valid  bool
	}{
		{"Root with 0 parents", synthesized(change.DerivationRoot, sha1ID(1)), true},
		{"Root with 1 parent", synthesized(change.DerivationRoot, sha1ID(1), sha1ID(2)), false},
		{"FirstParent with 0 parents", synthesized(change.DerivationFirstParent, sha1ID(1)), false},
		{"FirstParent with 1 parent", synthesized(change.DerivationFirstParent, sha1ID(1), sha1ID(2)), true},
		{"MultiParent with 1 parent", synthesized(change.DerivationMultiParent, sha1ID(1), sha1ID(2)), false},
		{"MultiParent with 2 parents", synthesized(change.DerivationMultiParent, sha1ID(1), sha1ID(2), sha1ID(3)), true},
		{"Squash with 3 parents", synthesized(change.DerivationSquash, sha1ID(1), sha1ID(2), sha1ID(3), sha1ID(4)), true},
		{"EmptyCommit with 0 parents", synthesized(change.DerivationEmptyCommit, sha1ID(1)), true},
		{"RewriteCandidate with 2 parents", synthesized(change.DerivationRewriteCandidate, sha1ID(1), sha1ID(2), sha1ID(3)), true},
		{"SHA-1 commit with a SHA-256 parent", synthesized(change.DerivationFirstParent, sha1ID(1), sha256ID(2)), false},
		{"SHA-256 commit with a SHA-1 parent", synthesized(change.DerivationFirstParent, sha256ID(1), sha1ID(2)), false},
		{"SHA-256 commit and parent", synthesized(change.DerivationFirstParent, sha256ID(1), sha256ID(2)), true},
		{
			"31-byte SHA-256 id",
			synthesized(change.DerivationRoot, change.GitObjectID{SHA256: true, Bytes: bytes.Repeat([]byte{1}, 31)}),
			false,
		},
		{
			"31-byte SHA-256 parent",
			synthesized(change.DerivationFirstParent, sha256ID(1), change.GitObjectID{SHA256: true, Bytes: bytes.Repeat([]byte{2}, 31)}),
			false,
		},
		{"20-byte SHA-1 id", synthesized(change.DerivationRoot, sha1ID(1)), true},
		{
			"20-byte id marked SHA-256",
			synthesized(change.DerivationRoot, change.GitObjectID{SHA256: true, Bytes: bytes.Repeat([]byte{1}, 20)}),
			false,
		},
		{
			"32-byte id marked SHA-1",
			synthesized(change.DerivationRoot, change.GitObjectID{Bytes: bytes.Repeat([]byte{1}, 32)}),
			false,
		},
		{"GitSynthesized with no commit", synthesized(change.DerivationRoot, change.GitObjectID{}), false},
		{"unknown derivation", synthesized(change.GitDerivation(6), sha1ID(1)), false},
		{"Native", change.Origin{}, true},
		{"Native with a commit", change.Origin{Kind: change.Native, Commit: sha1ID(1)}, false},
		{"Native with a parent", change.Origin{Kind: change.Native, Parents: []change.GitObjectID{sha1ID(1)}}, false},
		{"Native with a derivation", change.Origin{Kind: change.Native, Derivation: change.DerivationSquash}, false},
		{
			"GitResolution with 2 parents",
			change.Origin{Kind: change.GitResolution, Commit: sha1ID(1), Parents: []change.GitObjectID{sha1ID(2), sha1ID(3)}},
			true,
		},
		{
			// The two-parent rule belongs to Change.Validate.
			"GitResolution with 1 parent",
			change.Origin{Kind: change.GitResolution, Commit: sha1ID(1), Parents: []change.GitObjectID{sha1ID(2)}},
			true,
		},
		{
			"GitResolution with mixed algorithms",
			change.Origin{Kind: change.GitResolution, Commit: sha256ID(1), Parents: []change.GitObjectID{sha256ID(2), sha1ID(3)}},
			false,
		},
		{
			"GitResolution with a derivation",
			change.Origin{Kind: change.GitResolution, Commit: sha1ID(1), Parents: []change.GitObjectID{sha1ID(2), sha1ID(3)}, Derivation: change.DerivationMultiParent},
			false,
		},
		{"unknown kind", change.Origin{Kind: change.OriginKind(3), Commit: sha1ID(1)}, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.origin.Validate()
			if tc.valid {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, change.ErrInvalidOrigin)
		})
	}
}

func TestOriginKindValues(t *testing.T) {
	// The variant indices of docs/remote/change-format/sections.md#wirechangeoriginv2.
	testCases := []struct {
		k    change.OriginKind
		v    uint8
		name string
	}{
		{change.Native, 0, "Native"},
		{change.GitSynthesized, 1, "GitSynthesized"},
		{change.GitResolution, 2, "GitResolution"},
		{change.OriginKind(3), 3, "OriginKind(3)"},
	}
	for _, tc := range testCases {
		require.Equal(t, tc.v, uint8(tc.k))
		require.Equal(t, tc.name, tc.k.String())
	}
}

func TestGitDerivationValues(t *testing.T) {
	// The variant indices of docs/remote/change-format/sections.md#wiregitderivationv2.
	testCases := []struct {
		d    change.GitDerivation
		v    uint8
		name string
	}{
		{change.DerivationRoot, 0, "Root"},
		{change.DerivationFirstParent, 1, "FirstParent"},
		{change.DerivationMultiParent, 2, "MultiParent"},
		{change.DerivationSquash, 3, "Squash"},
		{change.DerivationEmptyCommit, 4, "EmptyCommit"},
		{change.DerivationRewriteCandidate, 5, "RewriteCandidate"},
		{change.GitDerivation(6), 6, "GitDerivation(6)"},
	}
	for _, tc := range testCases {
		require.Equal(t, tc.v, uint8(tc.d))
		require.Equal(t, tc.name, tc.d.String())
	}
}

func TestZeroValues(t *testing.T) {
	// The zero Kind is durable history and the zero Origin is Native,
	// which is what a v1 HEADER payload implies.
	require.False(t, change.Kind{}.Snapshot)
	require.Equal(t, change.Native, change.Origin{}.Kind)
	require.NoError(t, change.Origin{}.Validate())
}
