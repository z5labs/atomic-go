package change

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/changefixture"
	"github.com/z5labs/atomic-go/internal/postcard"
)

// hashOf returns a non-zero hash whose bytes sort by b.
func hashOf(b byte) ident.Hash {
	var h ident.Hash
	h[0] = b
	h[31] = 0xAA
	return h
}

// testTable returns the table zero, hashOf(1), hashOf(2), hashOf(3): slot
// 0 holds the zero hash, as in every file Atomic writes.
func testTable(t *testing.T) changefile.HashTable {
	t.Helper()
	tab, err := changefile.NewHashTable([]ident.Hash{{}, hashOf(1), hashOf(2), hashOf(3)})
	require.NoError(t, err)
	return tab
}

func emptyTable(t *testing.T) changefile.HashTable {
	t.Helper()
	tab, err := changefile.NewHashTable([]ident.Hash{{}})
	require.NoError(t, err)
	return tab
}

// requireFormatError checks that err is a *changefile.FormatError of kind
// in section.
func requireFormatError(t *testing.T, err error, kind error, section changefile.SectionType) {
	t.Helper()
	require.Error(t, err)
	var fe *changefile.FormatError
	require.True(t, errors.As(err, &fe), "%v is not a *changefile.FormatError", err)
	require.ErrorIs(t, err, kind)
	require.Equal(t, section, fe.Section)
}

// headerPrefix returns a ChangeHeader with the message "m", no
// description, the worked example's timestamp and no authors.
func headerPrefix() []byte {
	b := []byte{1, 'm', 0, 20}
	b = append(b, "2023-11-14T22:13:20Z"...)
	return append(b, 0)
}

func headerM() Header {
	return Header{
		Message:       "m",
		Timestamp:     time.Unix(1700000000, 0).UTC(),
		TimestampText: "2023-11-14T22:13:20Z",
	}
}

func ptr[T any](v T) *T { return &v }

func TestHeaderV2WorkedExample(t *testing.T) {
	// docs/remote/change-format/sections.md#header-example.
	payload := changefixture.WorkedExampleHeader()
	require.Len(t, payload, 54)
	tab := emptyTable(t)

	want := envelope{
		header: Header{
			Message:       "opaque v2 fixture",
			Timestamp:     time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
			TimestampText: "2023-11-14T22:13:20Z",
		},
		metadata: []byte("ATSP\x01\x00\x00"),
	}
	for _, v := range []changefile.Version{changefile.Version2, changefile.Version3} {
		env, err := decodeHeader(v, payload, tab)
		require.NoError(t, err)
		require.Equal(t, want, env)
		require.Equal(t, Kind{}, env.kind, "Durable")
		require.Equal(t, Native, env.origin.Kind)

		got, err := appendHeader(nil, v, env, tab)
		require.NoError(t, err)
		require.Equal(t, payload, got)
	}

	t.Run("bytes after the value are ignored", func(t *testing.T) {
		env, err := decodeHeaderV2(append(changefixture.WorkedExampleHeader(), 0xEE), tab)
		require.NoError(t, err)
		require.Equal(t, want, env)
	})

	t.Run("without timestamp text the timestamp is formatted as chrono does", func(t *testing.T) {
		env := want
		env.header.TimestampText = ""
		got, err := appendHeaderV2(nil, env, tab)
		require.NoError(t, err)
		require.Equal(t, payload, got)
	})

	t.Run("the decoded metadata does not alias the payload", func(t *testing.T) {
		b := changefixture.WorkedExampleHeader()
		env, err := decodeHeaderV2(b, tab)
		require.NoError(t, err)
		b[len(b)-1] = 0xEE
		require.Equal(t, []byte("ATSP\x01\x00\x00"), env.metadata)
	})
}

// changeHeaderExample returns the 153-byte schema version 1 HEADER payload
// of docs/remote/change-format/sections.md#changeheader-example.
func changeHeaderExample() []byte {
	str := func(b []byte, s string) []byte {
		return append(append(b, byte(len(s))), s...)
	}
	b := str(nil, "legacy v1 file recorded by atomic 0.19.1")
	b = append(b, 0) // description: None
	b = str(b, "2026-11-12T16:23:20.451350Z")
	b = append(b, 1) // authors: 1 entry
	b = str(b, "fixture")
	b = str(append(b, 1), "fixture@atomic.dev")
	b = str(append(b, 1), "TKX4DF2DBALZPZRTUORRAZ74CX5BTFCLMBIRPF27PWYD3TDFUNYQ")
	return b
}

func TestHeaderV1Example(t *testing.T) {
	payload := changeHeaderExample()
	require.Len(t, payload, 153)

	want := envelope{header: Header{
		Message:       "legacy v1 file recorded by atomic 0.19.1",
		Timestamp:     time.Date(2026, 11, 12, 16, 23, 20, 451350000, time.UTC),
		TimestampText: "2026-11-12T16:23:20.451350Z",
		Authors: []Author{{
			Name:     "fixture",
			Email:    ptr("fixture@atomic.dev"),
			Identity: ptr("TKX4DF2DBALZPZRTUORRAZ74CX5BTFCLMBIRPF27PWYD3TDFUNYQ"),
		}},
	}}
	env, err := decodeHeader(changefile.Version1, payload, changefile.HashTable{})
	require.NoError(t, err)
	require.Equal(t, want, env)
	require.Equal(t, Kind{}, env.kind, "Durable")
	require.Equal(t, Native, env.origin.Kind)

	got, err := appendHeader(nil, changefile.Version1, env, changefile.HashTable{})
	require.NoError(t, err)
	require.Equal(t, payload, got)

	t.Run("without timestamp text the six-digit fraction is kept", func(t *testing.T) {
		h := want.header
		h.TimestampText = ""
		got, err := appendHeaderV1(nil, h)
		require.NoError(t, err)
		require.Equal(t, payload, got)
	})

	t.Run("a v2 payload decoded as v1 gives its human-readable header", func(t *testing.T) {
		env, err := decodeHeaderV1(changefixture.WorkedExampleHeader())
		require.NoError(t, err)
		require.Equal(t, "opaque v2 fixture", env.header.Message)
		require.Nil(t, env.metadata)
	})

	t.Run("a v1 header cannot carry the envelope", func(t *testing.T) {
		env := want
		env.metadata = []byte{1}
		_, err := appendHeader(nil, changefile.Version1, env, changefile.HashTable{})
		requireFormatError(t, err, changefile.ErrInvalidChangeMetadata, changefile.SectionHeader)
	})
}

func TestHeaderTimestampTextIsKept(t *testing.T) {
	// One instant, a spelling chrono would not write.
	b := []byte{1, 'm', 0, 25}
	b = append(b, "2023-11-14T23:13:20+01:00"...)
	b = append(b, 0)

	env, err := decodeHeaderV1(b)
	require.NoError(t, err)
	require.True(t, env.header.Timestamp.Equal(time.Unix(1700000000, 0)))
	require.Equal(t, time.UTC, env.header.Timestamp.Location())
	require.Equal(t, "2023-11-14T23:13:20+01:00", env.header.TimestampText)

	got, err := appendHeaderV1(nil, env.header)
	require.NoError(t, err)
	require.Equal(t, b, got)
}

func TestHeaderV2RoundTrip(t *testing.T) {
	tab := testTable(t)
	sha1 := func(b byte) GitObjectID { return GitObjectID{Bytes: bytes.Repeat([]byte{b}, SHA1Size)} }

	// A GitSynthesized FirstParent change with one frontier root and two
	// extra_known changes, assembled field by field.
	payload := headerPrefix()
	payload = append(payload,
		0,    // kind: Durable
		0,    // supersedes: None
		1, 0, // origin: GitSynthesized, commit: SHA-1
	)
	payload = append(payload, sha1(0xC1).Bytes...)
	payload = append(payload, 1, 0) // parents: 1 entry, SHA-1
	payload = append(payload, sha1(0xC2).Bytes...)
	payload = append(payload,
		1,    // derivation: FirstParent
		1, 3, // causal_frontier: [3]
		2, 1, 2, // extra_known: [1, 2]
		2, 0xAB, 0xCD, // metadata
	)

	want := envelope{
		header: headerM(),
		origin: Origin{
			Kind:       GitSynthesized,
			Commit:     sha1(0xC1),
			Parents:    []GitObjectID{sha1(0xC2)},
			Derivation: DerivationFirstParent,
		},
		causalFrontier: []ident.Hash{hashOf(3)},
		extraKnown:     []ident.Hash{hashOf(1), hashOf(2)},
		metadata:       []byte{0xAB, 0xCD},
	}
	env, err := decodeHeaderV2(payload, tab)
	require.NoError(t, err)
	require.Equal(t, want, env)
	got, err := appendHeaderV2(nil, env, tab)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	t.Run("a snapshot that supersedes another", func(t *testing.T) {
		var wc [16]byte
		for i := range wc {
			wc[i] = byte(i + 1)
		}
		payload := headerPrefix()
		payload = append(payload, 1) // kind: Snapshot
		payload = append(payload, wc[:]...)
		payload = append(payload,
			1, 2, // supersedes: Some(2)
			0, // origin: Native
			0, 0, 0,
		)
		want := envelope{header: headerM(), kind: Kind{Snapshot: true, WorkingCopy: wc}, supersedes: ptr(hashOf(2))}
		env, err := decodeHeaderV2(payload, tab)
		require.NoError(t, err)
		require.Equal(t, want, env)
		got, err := appendHeaderV2(nil, env, tab)
		require.NoError(t, err)
		require.Equal(t, payload, got)
	})

	t.Run("a GitResolution with SHA-256 ids", func(t *testing.T) {
		sha256 := func(b byte) GitObjectID {
			return GitObjectID{SHA256: true, Bytes: bytes.Repeat([]byte{b}, SHA256Size)}
		}
		env := envelope{
			header: headerM(),
			origin: Origin{
				Kind:    GitResolution,
				Commit:  sha256(1),
				Parents: []GitObjectID{sha256(2), sha256(3)},
			},
			causalFrontier: []ident.Hash{hashOf(1), hashOf(3)},
		}
		b, err := appendHeaderV2(nil, env, tab)
		require.NoError(t, err)
		got, err := decodeHeaderV2(b, tab)
		require.NoError(t, err)
		require.Equal(t, env, got)
	})
}

func TestDeps(t *testing.T) {
	// docs/remote/change-format/sections.md#deps-section.
	tab := testTable(t)
	testCases := []struct {
		name    string
		payload []byte
		indices []changefile.HashIndex
		want    []ident.Hash
	}{
		{"no dependencies", []byte{0}, nil, nil},
		{"one dependency", []byte{1, 1}, []changefile.HashIndex{1}, []ident.Hash{hashOf(1)}},
		{"three dependencies", []byte{3, 1, 2, 3}, []changefile.HashIndex{1, 2, 3}, []ident.Hash{hashOf(1), hashOf(2), hashOf(3)}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			deps, err := decodeDeps(tc.payload, tab)
			require.NoError(t, err)
			require.Equal(t, tc.want, deps)

			deps, err = decodeDeps(append(bytes.Clone(tc.payload), 0xEE), tab)
			require.NoError(t, err)
			require.Equal(t, tc.want, deps)

			got, err := appendDeps(nil, tc.indices)
			require.NoError(t, err)
			require.Equal(t, tc.payload, got)
		})
	}

	t.Run("the reader does not check index order", func(t *testing.T) {
		deps, err := decodeDeps([]byte{2, 2, 1}, tab)
		require.NoError(t, err)
		require.Equal(t, []ident.Hash{hashOf(2), hashOf(1)}, deps)
	})
}

func TestDepsDecodeErrors(t *testing.T) {
	tab := testTable(t)
	testCases := []struct {
		name    string
		payload []byte
		kind    error
	}{
		{"index 0 names the zero slot", []byte{1, 0}, changefile.ErrInvalidChangeMetadata},
		{"index 0xFFFF", []byte{1, 0xFF, 0xFF, 0x03}, changefile.ErrHashIndexOutOfBounds},
		{"index past the end", []byte{2, 1, 4}, changefile.ErrHashIndexOutOfBounds},
		{"truncated", []byte{2, 1}, changefile.ErrPayload},
		{"empty", nil, changefile.ErrPayload},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeDeps(tc.payload, tab)
			requireFormatError(t, err, tc.kind, changefile.SectionDeps)
		})
	}

	t.Run("a postcard failure is kept as the cause", func(t *testing.T) {
		_, err := decodeDeps([]byte{2, 1}, tab)
		require.ErrorIs(t, err, postcard.ErrUnexpectedEnd)
	})
}

func TestDepsEncodeErrors(t *testing.T) {
	// ChangeWriter::write_dependencies's rules.
	testCases := []struct {
		name    string
		indices []changefile.HashIndex
	}{
		{"descending", []changefile.HashIndex{2, 1}},
		{"repeated", []changefile.HashIndex{1, 1}},
		{"index 0", []changefile.HashIndex{0}},
		{"index 0xFFFF", []changefile.HashIndex{changefile.ThisChange}},
		{"index 0xFFFF after a valid one", []changefile.HashIndex{1, changefile.ThisChange}},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dst := []byte{0x77}
			got, err := appendDeps(dst, tc.indices)
			requireFormatError(t, err, changefile.ErrInvalidChangeMetadata, changefile.SectionDeps)
			require.Equal(t, dst, got, "nothing is appended")
		})
	}
}

func TestHeaderV2DecodeErrors(t *testing.T) {
	tab := testTable(t)
	sha1 := bytes.Repeat([]byte{0x11}, SHA1Size)
	sha256 := bytes.Repeat([]byte{0x22}, SHA256Size)

	// envelopeWith returns a payload of headerPrefix, a Durable kind, then
	// tail.
	envelopeWith := func(tail ...byte) []byte {
		return append(append(headerPrefix(), 0), tail...)
	}
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

	testCases := []struct {
		name    string
		payload []byte
		kind    error
	}{
		{"supersedes index 0 names the zero slot", envelopeWith(1, 0, 0, 0, 0, 0), changefile.ErrInvalidChangeMetadata},
		{"supersedes index 0xFFFF", envelopeWith(1, 0xFF, 0xFF, 0x03, 0, 0, 0, 0), changefile.ErrHashIndexOutOfBounds},
		{"supersedes index past the end", envelopeWith(1, 9, 0, 0, 0, 0), changefile.ErrHashIndexOutOfBounds},
		{"frontier index 0", envelopeWith(0, 0, 1, 0, 0, 0), changefile.ErrInvalidChangeMetadata},
		{"unsorted frontier", envelopeWith(0, 0, 2, 2, 1, 0, 0), changefile.ErrInvalidChangeMetadata},
		{"repeated frontier root", envelopeWith(0, 0, 2, 1, 1, 0, 0), changefile.ErrInvalidChangeMetadata},
		{"unsorted extra_known", envelopeWith(0, 0, 0, 2, 3, 1, 0), changefile.ErrInvalidChangeMetadata},
		{"extra_known index past the end", envelopeWith(0, 0, 0, 1, 4, 0), changefile.ErrHashIndexOutOfBounds},
		{
			"GitSynthesized with a SHA-1 commit and a SHA-256 parent",
			cat(envelopeWith(0, 1, 0), sha1, []byte{1, 1}, sha256, []byte{1, 0, 0, 0}),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"Root origin with one parent",
			cat(envelopeWith(0, 1, 0), sha1, []byte{1, 0}, sha1, []byte{0, 0, 0, 0}),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"SHA-256 commit cut short at 20 bytes",
			cat(envelopeWith(0, 1, 1), sha1),
			changefile.ErrPayload,
		},
		{"unknown kind", append(headerPrefix(), 2), changefile.ErrPayload},
		{"unknown origin", envelopeWith(0, 3, 0, 0, 0), changefile.ErrPayload},
		{"unknown derivation", cat(envelopeWith(0, 1, 0), sha1, []byte{0, 6, 0, 0, 0}), changefile.ErrPayload},
		{"bad timestamp", append([]byte{1, 'm', 0, 3}, "now\x00\x00\x00\x00\x00\x00\x00"...), changefile.ErrPayload},
		{"no metadata", envelopeWith(0, 0, 0, 0), changefile.ErrPayload},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeHeaderV2(tc.payload, tab)
			requireFormatError(t, err, tc.kind, changefile.SectionHeader)
		})
	}

	t.Run("an invalid origin keeps its cause", func(t *testing.T) {
		_, err := decodeHeaderV2(cat(envelopeWith(0, 1, 0), sha1, []byte{1, 0}, sha1, []byte{0, 0, 0, 0}), tab)
		require.ErrorIs(t, err, ErrInvalidOrigin)
	})

	t.Run("a v1 payload that does not decode", func(t *testing.T) {
		_, err := decodeHeaderV1([]byte{5, 'a'})
		requireFormatError(t, err, changefile.ErrPayload, changefile.SectionHeader)
	})

	t.Run("an unsupported version", func(t *testing.T) {
		_, err := decodeHeader(changefile.Version(4), changefixture.WorkedExampleHeader(), tab)
		requireFormatError(t, err, changefile.ErrUnsupportedVersion, changefile.SectionHeader)
		_, err = appendHeader(nil, changefile.Version(0), envelope{}, tab)
		requireFormatError(t, err, changefile.ErrUnsupportedVersion, changefile.SectionHeader)
	})
}

func TestHeaderV2EncodeErrors(t *testing.T) {
	tab := testTable(t)
	sha1 := func(n int) GitObjectID { return GitObjectID{Bytes: bytes.Repeat([]byte{1}, n)} }
	sha256 := func(n int) GitObjectID { return GitObjectID{SHA256: true, Bytes: bytes.Repeat([]byte{2}, n)} }
	with := func(f func(*envelope)) envelope {
		env := envelope{header: headerM()}
		f(&env)
		return env
	}

	testCases := []struct {
		name string
		env  envelope
		kind error
	}{
		{
			"a 19-byte SHA-1 commit",
			with(func(e *envelope) { e.origin = Origin{Kind: GitSynthesized, Commit: sha1(19)} }),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"a 20-byte SHA-256 parent",
			with(func(e *envelope) {
				e.origin = Origin{Kind: GitSynthesized, Commit: sha256(32), Parents: []GitObjectID{sha256(20)}, Derivation: DerivationFirstParent}
			}),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"a SHA-1 commit with a SHA-256 parent",
			with(func(e *envelope) {
				e.origin = Origin{Kind: GitSynthesized, Commit: sha1(20), Parents: []GitObjectID{sha256(32)}, Derivation: DerivationFirstParent}
			}),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"unsorted frontier",
			with(func(e *envelope) { e.causalFrontier = []ident.Hash{hashOf(2), hashOf(1)} }),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"unsorted extra_known",
			with(func(e *envelope) { e.extraKnown = []ident.Hash{hashOf(3), hashOf(3)} }),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"supersedes the zero hash",
			with(func(e *envelope) { e.supersedes = &ident.Hash{} }),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"a hash the table does not hold",
			with(func(e *envelope) { e.extraKnown = []ident.Hash{hashOf(9)} }),
			changefile.ErrHashNotFound,
		},
		{
			"a Durable kind with a working copy",
			with(func(e *envelope) { e.kind.WorkingCopy[0] = 1 }),
			changefile.ErrInvalidChangeMetadata,
		},
		{
			"a message that is not UTF-8",
			with(func(e *envelope) { e.header.Message = "\xff" }),
			changefile.ErrPayload,
		},
		{
			"an author email that is not UTF-8",
			with(func(e *envelope) { e.header.Authors = []Author{{Name: "a", Email: ptr("\xff")}} }),
			changefile.ErrPayload,
		},
		{
			"timestamp text that does not parse",
			with(func(e *envelope) { e.header.TimestampText = "yesterday" }),
			changefile.ErrPayload,
		},
		{
			"a year past 9999",
			with(func(e *envelope) {
				e.header.TimestampText = ""
				e.header.Timestamp = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			}),
			changefile.ErrPayload,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dst := []byte{0x77}
			got, err := appendHeaderV2(dst, tc.env, tab)
			requireFormatError(t, err, tc.kind, changefile.SectionHeader)
			require.Equal(t, dst, got, "nothing is appended")
		})
	}

	t.Run("a Git id of the wrong length keeps its cause", func(t *testing.T) {
		_, err := appendHeaderV2(nil, testCases[0].env, tab)
		require.ErrorIs(t, err, ErrInvalidOrigin)
	})
}
