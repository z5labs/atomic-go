package legacy

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/internal/wire"
)

// The Rust client's names for the layer and chunk requests, used as
// [remote.Error.Op].
const (
	opDownloadChangeLayers = "download_change_layers"
	opGetChunkManifest     = "get_chunk_manifest"
)

// Layer is one of the three layers of a V3 change file that a download can
// select (docs/remote/types.md#layer). The HEADER, DEPS and PROVENANCE
// sections are not layers; the Rust client describes them as always sent,
// which is the server's decision.
type Layer uint8

// The layers, in the fixed order [LayerSelection.QueryValue] writes them.
const (
	// LayerGraph is the GRAPH sections, needed to apply a change.
	LayerGraph Layer = iota

	// LayerSemantic is the SEMANTIC sections, needed for diffs, blame and
	// review, and which can be regenerated from the graph and the content.
	LayerSemantic

	// LayerContent is the CONTENT chunks: the raw file data.
	LayerContent
)

// String returns the layer's name as the query sends it: "graph",
// "semantic" or "content". Any other value is "Layer(n)".
func (l Layer) String() string {
	switch l {
	case LayerGraph:
		return "graph"
	case LayerSemantic:
		return "semantic"
	case LayerContent:
		return "content"
	}
	return "Layer(" + strconv.Itoa(int(l)) + ")"
}

// LayerSelection is a set of [Layer]s, the layers=
// value of [Client.DownloadChangeLayers]
// (docs/remote/endpoints/project.md#layerselection).
//
// The zero LayerSelection selects nothing. Unlike the Rust client's
// LayerSelection, whose default is every layer, it is not [AllLayers];
// its [LayerSelection.QueryValue] is "".
type LayerSelection uint8

// The Rust client's preset selections.
const (
	// AllLayers selects every layer. Its query value is "all".
	AllLayers = LayerSelection(1<<LayerGraph | 1<<LayerSemantic | 1<<LayerContent)

	// ThinPull selects the graph and the content, enough to apply a
	// change. Its query value is "graph,content".
	ThinPull = LayerSelection(1<<LayerGraph | 1<<LayerContent)

	// ThinReview selects the semantic sections and the content, enough to
	// review a change. Its query value is "semantic,content".
	ThinReview = LayerSelection(1<<LayerSemantic | 1<<LayerContent)

	// GraphOnly selects the graph alone. Its query value is "graph".
	GraphOnly = LayerSelection(1 << LayerGraph)
)

// SelectLayers returns the selection holding each of ls. A value that is
// not one of the three layers is ignored.
func SelectLayers(ls ...Layer) LayerSelection {
	var s LayerSelection
	for _, l := range ls {
		s = s.With(l)
	}
	return s
}

// With returns s with l added. A value that is not one of the three
// layers leaves s unchanged.
func (s LayerSelection) With(l Layer) LayerSelection {
	if l > LayerContent {
		return s
	}
	return s | 1<<l
}

// Has reports whether s selects l.
func (s LayerSelection) Has(l Layer) bool {
	return l <= LayerContent && s&(1<<l) != 0
}

// QueryValue returns the selection as the layers= query sends it, as the
// Rust client's to_query_value writes it: "all" when every layer is
// selected, otherwise the selected layers' names joined by ',' in the fixed
// order graph, semantic, content, whatever order they were added in, and
// "" when none is. Bits that are not a layer are ignored.
func (s LayerSelection) QueryValue() string {
	if s&AllLayers == AllLayers {
		return "all"
	}
	var names []string
	for l := LayerGraph; l <= LayerContent; l++ {
		if s.Has(l) {
			names = append(names, l.String())
		}
	}
	return strings.Join(names, ",")
}

// DownloadChangeLayers returns the layers of the change whose hash is h
// that layers selects
// (docs/remote/endpoints/project.md#download_change_layers).
//
// The request is a GET of the project URL with the query
// change={h}&layers={layers}, where {h} is h's canonical base32 text,
// [ident.Hash.String], and {layers} is [LayerSelection.QueryValue]; an
// empty selection sends "layers=". Only 200 succeeds, and its body is
// returned as it arrived, after any Content-Encoding is decoded.
//
// The bytes are not interpreted. Which sections a layer-selected response
// holds, and how they are laid out, is the server's decision and is not
// documented; a server that does not support layers= sends the whole
// change, and the client cannot tell which it got.
//
// 404 is a [remote.KindChangeNotFound] whose Subject is h's text. The
// other failures are those every read shares; see [Client.Changelist].
func (c *Client) DownloadChangeLayers(ctx context.Context, h ident.Hash, layers LayerSelection) ([]byte, error) {
	body, _, err := c.fetch(ctx, opDownloadChangeLayers, notFoundAs(remote.KindChangeNotFound, h.String()), func(q *wire.Query) error {
		if err := q.Add("change", h.String()); err != nil {
			return err
		}
		return q.Add("layers", layers.QueryValue())
	})
	if err != nil {
		return nil, err
	}
	return body.data, nil
}

// ChunkManifestEntry is one content chunk of a change
// (docs/remote/types.md#chunkmanifestentry).
type ChunkManifestEntry struct {
	// Index is the chunk's 0-based index within the change.
	Index uint32

	// Hash is the BLAKE3 hash of the chunk's uncompressed data, so the
	// same content has the same hash in any change. The wire writes it as
	// 64 hex digits, [ident.Hash.Hex], not as base32.
	Hash ident.Hash

	// CompressedSize is the chunk's compressed size in bytes: what would
	// be transferred.
	CompressedSize uint32

	// UncompressedSize is the size the chunk decompresses to.
	UncompressedSize uint32
}

// ChunkManifest is the list of a change's content chunks, in order, the
// answer to [Client.ChunkManifest]
// (docs/remote/endpoints/project.md#chunkmanifest).
type ChunkManifest struct {
	// Entries are the chunks, in the server's order. None is a change
	// with no content chunks.
	Entries []ChunkManifestEntry
}

// ChunkManifest returns the chunk manifest of the change whose hash is h,
// or nil and no error when the server offers none
// (docs/remote/endpoints/project.md#get_chunk_manifest).
//
// The request is a GET of the project URL with the query
// change={h}&manifest, {h} being h's canonical base32 text.
//
// A 200 whose Content-Type contains "json", matched case-sensitively as
// the Rust client does, is parsed as a [ChunkManifest]. Its "entries"
// member and each entry's "index", "hash", "compressed_size" and
// "uncompressed_size" are required, member names match exactly, and
// unknown members are ignored. A hash must be 64 hex digits of either
// case; the Rust client's parser also takes a '+' at the start of a digit
// pair, which this one refuses, as [ident.ParseHashHex] does. A body that
// does not parse is a [remote.KindProtocol] "Failed to parse chunk
// manifest: {error}".
//
// A 200 with any other Content-Type, or none, is nil: the Rust client
// takes it that the server ignored &manifest and sent the change itself,
// and its body is not read. A 400 is nil too, taken as a server that does
// not support the request.
//
// 404 is a [remote.KindChangeNotFound] whose Subject is h's text. The
// other failures are those every read shares; see [Client.Changelist].
func (c *Client) ChunkManifest(ctx context.Context, h ident.Hash) (*ChunkManifest, error) {
	const op = opGetChunkManifest
	u, err := c.queryURL(op, http.MethodGet, func(q *wire.Query) error {
		if err := q.Add("change", h.String()); err != nil {
			return err
		}
		q.Flag("manifest")
		return nil
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.requester.Do(ctx, wire.Request{Op: op, Method: http.MethodGet, URL: u})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest:
		return nil, nil
	case http.StatusNotFound:
		err := notFoundAs(remote.KindChangeNotFound, h.String())(resp)
		var re *remote.Error
		if errors.As(err, &re) {
			re.Op = op
		}
		return nil, err
	default:
		return nil, wire.StatusError(op, resp, wire.ErrorBody(resp))
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		return nil, nil
	}
	body, err := c.readBody(ctx, response{op: op, method: http.MethodGet, resp: resp})
	if err != nil {
		return nil, err
	}
	m, err := parseChunkManifest(body.data)
	if err != nil {
		return nil, body.protocol("Failed to parse chunk manifest: "+err.Error(), err)
	}
	return m, nil
}

// parseChunkManifest parses a chunk manifest's JSON as the Rust client's
// serde derive does: every field required, names matched exactly, unknown
// members ignored and a known member given twice refused.
func parseChunkManifest(data []byte) (*ChunkManifest, error) {
	// Unmarshal into a RawMessage first: it checks the whole body is one
	// JSON value with nothing after it.
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	top, err := jsonObject(raw, "struct ChunkManifest")
	if err != nil {
		return nil, err
	}
	entriesRaw, err := top.field("entries", "a sequence")
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(entriesRaw, &items); err != nil {
		return nil, err
	}

	m := &ChunkManifest{}
	for i, item := range items {
		e, err := parseChunkManifestEntry(item)
		if err != nil {
			return nil, fmt.Errorf("entries[%d]: %w", i, err)
		}
		m.Entries = append(m.Entries, e)
	}
	return m, nil
}

// parseChunkManifestEntry parses one entry of a chunk manifest.
func parseChunkManifestEntry(raw json.RawMessage) (ChunkManifestEntry, error) {
	obj, err := jsonObject(raw, "struct ChunkManifestEntry")
	if err != nil {
		return ChunkManifestEntry{}, err
	}
	// The members are read in the struct's declaration order, so that a
	// missing one is named as serde would name it.
	var e ChunkManifestEntry
	if e.Index, err = u32Field(obj, "index"); err != nil {
		return ChunkManifestEntry{}, err
	}
	v, err := obj.field("hash", "a hex string")
	if err != nil {
		return ChunkManifestEntry{}, err
	}
	var text string
	if err := json.Unmarshal(v, &text); err != nil {
		return ChunkManifestEntry{}, fmt.Errorf("hash: %w", err)
	}
	if e.Hash, err = parseChunkHash(text); err != nil {
		return ChunkManifestEntry{}, fmt.Errorf("hash: %w", err)
	}
	if e.CompressedSize, err = u32Field(obj, "compressed_size"); err != nil {
		return ChunkManifestEntry{}, err
	}
	if e.UncompressedSize, err = u32Field(obj, "uncompressed_size"); err != nil {
		return ChunkManifestEntry{}, err
	}
	return e, nil
}

// u32Field returns the required member name of obj as a u32.
func u32Field(obj object, name string) (uint32, error) {
	v, err := obj.field(name, "u32")
	if err != nil {
		return 0, err
	}
	var n uint32
	if err := json.Unmarshal(v, &n); err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return n, nil
}

// parseChunkHash parses a chunk hash's hex text, with the Rust hex_hash
// module's errors: "expected 64 hex chars, got {n}" for a length other
// than 64 bytes, "invalid utf8" for a digit pair that splits a multi-byte
// character, and "invalid hex: {pair}" for a pair that is not two hex
// digits.
func parseChunkHash(s string) (ident.Hash, error) {
	var h ident.Hash
	if len(s) != 2*len(h) {
		return ident.Hash{}, fmt.Errorf("expected %d hex chars, got %d", 2*len(h), len(s))
	}
	for i := 0; i < len(s); i += 2 {
		pair := s[i : i+2]
		if !utf8.ValidString(pair) {
			return ident.Hash{}, errors.New("invalid utf8")
		}
		if !isHexDigit(pair[0]) || !isHexDigit(pair[1]) {
			return ident.Hash{}, fmt.Errorf("invalid hex: %s", pair)
		}
	}
	if _, err := hex.Decode(h[:], []byte(s)); err != nil {
		// Unreachable: every byte was checked above.
		return ident.Hash{}, err
	}
	return h, nil
}

func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// object is a JSON object's members by exact name, with the names given
// more than once.
type object struct {
	members map[string]json.RawMessage
	repeats map[string]bool
}

// jsonObject reads raw, one complete JSON value, as an object. what names
// the type expected, for the error a value of another type gets.
func jsonObject(raw json.RawMessage, what string) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return object{}, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return object{}, fmt.Errorf("invalid type: %s, expected %s", jsonKind(raw), what)
	}
	obj := object{members: map[string]json.RawMessage{}, repeats: map[string]bool{}}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return object{}, err
		}
		// Inside an object every token in key position is a string.
		name, _ := tok.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return object{}, err
		}
		if _, ok := obj.members[name]; ok {
			obj.repeats[name] = true
		}
		obj.members[name] = v
	}
	return obj, nil
}

// field returns the value of the required member name, which must not be
// null or given twice. what names the type expected, for a null's error.
func (o object) field(name, what string) (json.RawMessage, error) {
	v, ok := o.members[name]
	if !ok {
		return nil, fmt.Errorf("missing field `%s`", name)
	}
	if o.repeats[name] {
		return nil, fmt.Errorf("duplicate field `%s`", name)
	}
	if string(v) == "null" {
		return nil, fmt.Errorf("invalid type: null, expected %s", what)
	}
	return v, nil
}

// jsonKind names the type of the JSON value raw, for an error.
func jsonKind(raw json.RawMessage) string {
	switch raw[0] {
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	case '"':
		return "string"
	case '[':
		return "sequence"
	}
	return "number"
}

// ChunkNegotiation is what of a [ChunkManifest] a holder of some chunks
// still needs, worked out locally by [NegotiateChunks]
// (docs/remote/transport.md#chunknegotiation). No request sends or
// receives it: at the pinned Rust commit nothing transfers single chunks
// (docs/remote/transport.md#delta-transfer-negotiation).
type ChunkNegotiation struct {
	// Needed are the entries whose hash is not held, in manifest order.
	Needed []ChunkManifestEntry

	// AlreadyHave is the number of entries whose hash is held.
	AlreadyHave int

	// BytesSaved is the sum of CompressedSize over the held entries.
	BytesSaved uint64

	// BytesNeeded is the sum of CompressedSize over the needed entries.
	BytesNeeded uint64

	// TotalChunks is the number of entries in the manifest.
	TotalChunks int
}

// NegotiateChunks returns what of m a holder of the chunks whose hashes
// are have still needs, as the Rust client's ChunkNegotiation::compute
// does: every entry whose hash is not in have is needed, in m's order, and
// each entry is counted on its own, so two entries with one hash are both
// held or both needed. A nil m is a manifest with no entries.
func NegotiateChunks(m *ChunkManifest, have []ident.Hash) ChunkNegotiation {
	var n ChunkNegotiation
	if m == nil {
		return n
	}
	held := make(map[ident.Hash]bool, len(have))
	for _, h := range have {
		held[h] = true
	}
	n.TotalChunks = len(m.Entries)
	for _, e := range m.Entries {
		if held[e.Hash] {
			n.AlreadyHave++
			n.BytesSaved += uint64(e.CompressedSize)
			continue
		}
		n.Needed = append(n.Needed, e)
		n.BytesNeeded += uint64(e.CompressedSize)
	}
	return n
}
