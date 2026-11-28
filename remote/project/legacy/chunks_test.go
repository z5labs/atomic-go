package legacy_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/project/legacy"
	"github.com/z5labs/atomic-go/remote/remotetest"
)

// changeHash is the change the layer and chunk requests ask for, and
// changeHashText its base32 text.
var (
	changeHash     = ident.Sum([]byte("a change"))
	changeHashText = changeHash.String()
)

// manifestQuery is the get_chunk_manifest query for changeHash.
var manifestQuery = "change=" + changeHashText + "&manifest"

// chunkHash returns the 32-byte hash whose every byte is b.
func chunkHash(b byte) ident.Hash {
	var h ident.Hash
	for i := range h {
		h[i] = b
	}
	return h
}

func TestLayerString(t *testing.T) {
	require.Equal(t, "graph", legacy.LayerGraph.String())
	require.Equal(t, "semantic", legacy.LayerSemantic.String())
	require.Equal(t, "content", legacy.LayerContent.String())
	require.Equal(t, "Layer(7)", legacy.Layer(7).String())
}

func TestLayerSelectionQueryValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		sel  legacy.LayerSelection
		want string
	}{
		{name: "AllLayers", sel: legacy.AllLayers, want: "all"},
		{name: "ThinPull", sel: legacy.ThinPull, want: "graph,content"},
		{name: "ThinReview", sel: legacy.ThinReview, want: "semantic,content"},
		{name: "GraphOnly", sel: legacy.GraphOnly, want: "graph"},
		{name: "empty", sel: legacy.LayerSelection(0), want: ""},
		{name: "semantic only", sel: legacy.SelectLayers(legacy.LayerSemantic), want: "semantic"},
		{name: "fixed order", sel: legacy.SelectLayers(legacy.LayerContent, legacy.LayerGraph), want: "graph,content"},
		{name: "every layer added", sel: legacy.SelectLayers(legacy.LayerContent, legacy.LayerSemantic, legacy.LayerGraph), want: "all"},
		{name: "unknown layer ignored", sel: legacy.SelectLayers(legacy.Layer(5), legacy.LayerGraph), want: "graph"},
		{name: "unknown bits ignored", sel: legacy.LayerSelection(0xF0) | legacy.GraphOnly, want: "graph"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.sel.QueryValue())
		})
	}
}

func TestLayerSelectionHas(t *testing.T) {
	require.True(t, legacy.ThinPull.Has(legacy.LayerGraph))
	require.False(t, legacy.ThinPull.Has(legacy.LayerSemantic))
	require.True(t, legacy.ThinPull.Has(legacy.LayerContent))
	require.False(t, legacy.AllLayers.Has(legacy.Layer(3)))
	require.Equal(t, legacy.AllLayers, legacy.ThinPull.With(legacy.LayerSemantic))
	require.Equal(t, legacy.ThinPull, legacy.ThinPull.With(legacy.Layer(9)))
}

func TestDownloadChangeLayers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sel   legacy.LayerSelection
		query string
	}{
		{name: "ThinPull", sel: legacy.ThinPull, query: "change=" + changeHashText + "&layers=graph,content"},
		{name: "AllLayers", sel: legacy.AllLayers, query: "change=" + changeHashText + "&layers=all"},
		{name: "empty", sel: 0, query: "change=" + changeHashText + "&layers="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, s := serve(t, map[string]reply{tc.query: {body: "layer\x00bytes"}})
			data, err := c.DownloadChangeLayers(context.Background(), changeHash, tc.sel)
			require.NoError(t, err)
			require.Equal(t, []byte("layer\x00bytes"), data)

			require.Len(t, s.seen, 1)
			require.Equal(t, http.MethodGet, s.seen[0].method)
			require.Equal(t, codePath+"?"+tc.query, s.seen[0].requestURI)
			require.Empty(t, s.seen[0].body)
		})
	}
}

// manifestExample is the example of docs/remote/transport.md#chunk-manifests,
// its hashes "aabb..." and "ccdd..." written out to 64 digits.
var manifestExample = `{
  "entries": [
    {"index": 0, "hash": "` + strings.Repeat("aabb", 16) + `", "compressed_size": 32000, "uncompressed_size": 65536},
    {"index": 1, "hash": "` + strings.Repeat("ccdd", 16) + `", "compressed_size": 28000, "uncompressed_size": 65536}
  ]
}`

// exampleEntries is manifestExample's entries.
var exampleEntries = []legacy.ChunkManifestEntry{
	{Index: 0, Hash: ident.Hash([]byte(strings.Repeat("\xaa\xbb", 16))), CompressedSize: 32000, UncompressedSize: 65536},
	{Index: 1, Hash: ident.Hash([]byte(strings.Repeat("\xcc\xdd", 16))), CompressedSize: 28000, UncompressedSize: 65536},
}

func TestChunkManifest(t *testing.T) {
	c, s := serve(t, map[string]reply{manifestQuery: {body: manifestExample, header: []string{"Content-Type", "application/json"}}})

	m, err := c.ChunkManifest(context.Background(), changeHash)
	require.NoError(t, err)
	require.Equal(t, &legacy.ChunkManifest{Entries: exampleEntries}, m)

	require.Len(t, s.seen, 1)
	require.Equal(t, http.MethodGet, s.seen[0].method)
	require.Equal(t, codePath+"?"+manifestQuery, s.seen[0].requestURI)
}

func TestChunkManifestBodies(t *testing.T) {
	lower := strings.Repeat("0123456789abcdef", 4)
	upper := strings.ToUpper(lower)
	want := ident.Hash([]byte(strings.Repeat("\x01\x23\x45\x67\x89\xab\xcd\xef", 4)))

	for _, tc := range []struct {
		name string
		body string
		want *legacy.ChunkManifest
	}{
		{
			name: "lower-case hash",
			body: `{"entries":[{"index":0,"hash":"` + lower + `","compressed_size":100,"uncompressed_size":200}]}`,
			want: &legacy.ChunkManifest{Entries: []legacy.ChunkManifestEntry{{Hash: want, CompressedSize: 100, UncompressedSize: 200}}},
		},
		{
			name: "upper-case hash",
			body: `{"entries":[{"index":0,"hash":"` + upper + `","compressed_size":100,"uncompressed_size":200}]}`,
			want: &legacy.ChunkManifest{Entries: []legacy.ChunkManifestEntry{{Hash: want, CompressedSize: 100, UncompressedSize: 200}}},
		},
		{
			name: "extra members ignored",
			body: `{"version":3,"entries":[{"index":7,"hash":"` + lower + `","compressed_size":1,"uncompressed_size":2,"codec":"zstd","Index":9}]}`,
			want: &legacy.ChunkManifest{Entries: []legacy.ChunkManifestEntry{{Index: 7, Hash: want, CompressedSize: 1, UncompressedSize: 2}}},
		},
		{
			name: "no entries",
			body: `{"entries":[]}`,
			want: &legacy.ChunkManifest{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{manifestQuery: {body: tc.body, header: []string{"Content-Type", "application/json"}}})
			m, err := c.ChunkManifest(context.Background(), changeHash)
			require.NoError(t, err)
			require.Equal(t, tc.want, m)
		})
	}
}

func TestChunkManifestBodiesThatDoNotParse(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	entry := func(members string) string {
		return `{"entries":[` + members + `]}`
	}
	for _, tc := range []struct {
		name string
		body string
		msg  string // "" means only the prefix is checked
	}{
		{name: "63-character hash", body: entry(`{"index":0,"hash":"` + hash[:63] + `","compressed_size":1,"uncompressed_size":2}`), msg: "entries[0]: hash: expected 64 hex chars, got 63"},
		{name: "non-hex hash", body: entry(`{"index":0,"hash":"zz` + hash[2:] + `","compressed_size":1,"uncompressed_size":2}`), msg: "entries[0]: hash: invalid hex: zz"},
		{name: "plus in hash", body: entry(`{"index":0,"hash":"+f` + hash[2:] + `","compressed_size":1,"uncompressed_size":2}`), msg: "entries[0]: hash: invalid hex: +f"},
		{name: "pair splitting a character", body: entry(`{"index":0,"hash":"aé` + hash[3:] + `","compressed_size":1,"uncompressed_size":2}`), msg: "entries[0]: hash: invalid utf8"},
		{name: "missing compressed_size", body: entry(`{"index":0,"hash":"` + hash + `","uncompressed_size":2}`), msg: "entries[0]: missing field `compressed_size`"},
		{name: "missing index", body: entry(`{"hash":"` + hash + `"}`), msg: "entries[0]: missing field `index`"},
		{name: "wrong-case member is not the member", body: entry(`{"Index":0,"hash":"` + hash + `","compressed_size":1,"uncompressed_size":2}`), msg: "entries[0]: missing field `index`"},
		{name: "null member", body: entry(`{"index":null,"hash":"` + hash + `","compressed_size":1,"uncompressed_size":2}`), msg: "entries[0]: invalid type: null, expected u32"},
		{name: "duplicate member", body: entry(`{"index":0,"index":1,"hash":"` + hash + `","compressed_size":1,"uncompressed_size":2}`), msg: "entries[0]: duplicate field `index`"},
		{name: "negative size", body: entry(`{"index":0,"hash":"` + hash + `","compressed_size":-1,"uncompressed_size":2}`)},
		{name: "size past u32", body: entry(`{"index":0,"hash":"` + hash + `","compressed_size":4294967296,"uncompressed_size":2}`)},
		{name: "entry not an object", body: entry(`7`), msg: "entries[0]: invalid type: number, expected struct ChunkManifestEntry"},
		{name: "missing entries", body: `{}`, msg: "missing field `entries`"},
		{name: "null entries", body: `{"entries":null}`, msg: "invalid type: null, expected a sequence"},
		{name: "not an object", body: `[]`, msg: "invalid type: sequence, expected struct ChunkManifest"},
		{name: "trailing data", body: `{"entries":[]} x`},
		{name: "not JSON", body: `<html>`},
		{name: "empty", body: ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{manifestQuery: {body: tc.body, header: []string{"Content-Type", "application/json"}}})
			m, err := c.ChunkManifest(context.Background(), changeHash)
			require.Nil(t, m)
			re := requireRemoteError(t, err, remote.KindProtocol)
			require.True(t, strings.HasPrefix(re.Message, "Failed to parse chunk manifest: "), re.Message)
			if tc.msg != "" {
				require.Equal(t, "Failed to parse chunk manifest: "+tc.msg, re.Message)
			}
			require.Error(t, re.Err)
			require.Equal(t, "get_chunk_manifest", re.Op)
			require.Equal(t, codeURL+"?"+manifestQuery, re.URL)
			require.Equal(t, http.StatusOK, re.StatusCode)
		})
	}
}

func TestChunkManifestContentType(t *testing.T) {
	for _, tc := range []struct {
		name   string
		header []string
		found  bool
	}{
		{name: "application/json", header: []string{"Content-Type", "application/json"}, found: true},
		{name: "json with a charset", header: []string{"Content-Type", "application/json; charset=utf-8"}, found: true},
		{name: "vendor json", header: []string{"Content-Type", "application/vnd.atomic+json"}, found: true},
		{name: "text/plain", header: []string{"Content-Type", "text/plain"}},
		{name: "application/JSON", header: []string{"Content-Type", "application/JSON"}},
		{name: "application/octet-stream", header: []string{"Content-Type", "application/octet-stream"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := serve(t, map[string]reply{manifestQuery: {body: manifestExample, header: tc.header}})
			m, err := c.ChunkManifest(context.Background(), changeHash)
			require.NoError(t, err)
			if tc.found {
				require.Equal(t, &legacy.ChunkManifest{Entries: exampleEntries}, m)
			} else {
				require.Nil(t, m)
			}
		})
	}
}

func TestChunkManifestWithoutAContentTypeIsNone(t *testing.T) {
	// A RoundTripper, since net/http's server sniffs a Content-Type for a
	// response that sets none.
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(manifestExample)),
			Request:    req,
		}, nil
	})}
	c := newClientWithDoer(t, doer)
	m, err := c.ChunkManifest(context.Background(), changeHash)
	require.NoError(t, err)
	require.Nil(t, m)
}

func TestChunkManifestBadRequestIsNone(t *testing.T) {
	c, _ := serve(t, map[string]reply{manifestQuery: {status: http.StatusBadRequest, body: "unknown query"}})
	m, err := c.ChunkManifest(context.Background(), changeHash)
	require.NoError(t, err)
	require.Nil(t, m)
}

func TestChunkManifestNotFound(t *testing.T) {
	c, _ := serve(t, map[string]reply{manifestQuery: {status: http.StatusNotFound, body: "nope", header: []string{remote.MinVersionHeader, "0.1.0"}}})
	m, err := c.ChunkManifest(context.Background(), changeHash)
	require.Nil(t, m)
	re := requireRemoteError(t, err, remote.KindChangeNotFound)
	require.EqualError(t, err, "Change not found: "+changeHashText)
	require.Equal(t, changeHashText, re.Subject)
	require.Equal(t, "get_chunk_manifest", re.Op)
	require.Equal(t, http.MethodGet, re.Method)
	require.Equal(t, codeURL+"?"+manifestQuery, re.URL)
	require.Equal(t, http.StatusNotFound, re.StatusCode)
	require.Equal(t, "0.1.0", re.ServerMinVersion)
	require.ErrorIs(t, err, remote.ErrNotFound)
}

func TestChunkManifestStatuses(t *testing.T) {
	for _, tc := range []struct {
		status   int
		kind     remote.Kind
		sentinel error
	}{
		{status: http.StatusUnauthorized, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrUnauthorized},
		{status: http.StatusForbidden, kind: remote.KindAuthenticationFailed, sentinel: remote.ErrForbidden},
		{status: http.StatusInternalServerError, kind: remote.KindHTTP},
		{status: http.StatusCreated, kind: remote.KindHTTP},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c, _ := serve(t, map[string]reply{manifestQuery: {status: tc.status, body: "nope"}})
			m, err := c.ChunkManifest(context.Background(), changeHash)
			require.Nil(t, m)
			re := requireRemoteError(t, err, tc.kind)
			require.Equal(t, "get_chunk_manifest", re.Op)
			require.Equal(t, tc.status, re.StatusCode)
			require.Equal(t, "nope", re.Body)
			if tc.sentinel != nil {
				require.ErrorIs(t, err, tc.sentinel)
			}
		})
	}
}

func TestChunkManifestConnectionFailure(t *testing.T) {
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errSentinel
	})}
	c := newClientWithDoer(t, doer)
	m, err := c.ChunkManifest(context.Background(), changeHash)
	require.Nil(t, m)
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.Equal(t, "get_chunk_manifest", re.Op)
}

func TestChunkManifestReadFailure(t *testing.T) {
	body := &failingBody{r: strings.NewReader(`{"entries":`)}
	doer := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       body,
			Request:    req,
		}, nil
	})}
	c := newClientWithDoer(t, doer)
	m, err := c.ChunkManifest(context.Background(), changeHash)
	require.Nil(t, m)
	re := requireRemoteError(t, err, remote.KindConnectionFailed)
	require.ErrorIs(t, err, errSentinel)
	require.Equal(t, "get_chunk_manifest", re.Op)
	require.True(t, body.closed)
}

func TestChunkManifestBodyTooLarge(t *testing.T) {
	s := &server{routes: map[string]reply{manifestQuery: {body: `{"entries":[]}`, header: []string{"Content-Type", "application/json"}}}}
	c := newClientWithMaxBody(t, s, 13)
	m, err := c.ChunkManifest(context.Background(), changeHash)
	require.Nil(t, m)
	re := requireRemoteError(t, err, remote.KindProtocol)
	require.EqualError(t, err, "Protocol error: response body is larger than 13 bytes")
	require.Equal(t, "get_chunk_manifest", re.Op)
}

func TestNegotiateChunks(t *testing.T) {
	a := legacy.ChunkManifestEntry{Index: 0, Hash: chunkHash(0xa), CompressedSize: 100, UncompressedSize: 1000}
	b := legacy.ChunkManifestEntry{Index: 1, Hash: chunkHash(0xb), CompressedSize: 200, UncompressedSize: 2000}
	cc := legacy.ChunkManifestEntry{Index: 2, Hash: chunkHash(0xc), CompressedSize: 400, UncompressedSize: 4000}
	m := &legacy.ChunkManifest{Entries: []legacy.ChunkManifestEntry{a, b, cc}}

	n := legacy.NegotiateChunks(m, []ident.Hash{chunkHash(0xc), chunkHash(0xa), chunkHash(0xf)})
	require.Equal(t, legacy.ChunkNegotiation{
		Needed:      []legacy.ChunkManifestEntry{b},
		AlreadyHave: 2,
		BytesSaved:  500,
		BytesNeeded: 200,
		TotalChunks: 3,
	}, n)
}

func TestNegotiateChunksHoldingNothing(t *testing.T) {
	a := legacy.ChunkManifestEntry{Index: 0, Hash: chunkHash(1), CompressedSize: 10}
	b := legacy.ChunkManifestEntry{Index: 1, Hash: chunkHash(1), CompressedSize: 20}
	m := &legacy.ChunkManifest{Entries: []legacy.ChunkManifestEntry{a, b}}

	require.Equal(t, legacy.ChunkNegotiation{
		Needed:      []legacy.ChunkManifestEntry{a, b},
		BytesNeeded: 30,
		TotalChunks: 2,
	}, legacy.NegotiateChunks(m, nil))

	require.Equal(t, legacy.ChunkNegotiation{
		AlreadyHave: 2,
		BytesSaved:  30,
		TotalChunks: 2,
	}, legacy.NegotiateChunks(m, []ident.Hash{chunkHash(1)}))
}

func TestNegotiateChunksNilManifest(t *testing.T) {
	require.Equal(t, legacy.ChunkNegotiation{}, legacy.NegotiateChunks(nil, []ident.Hash{chunkHash(1)}))
}
