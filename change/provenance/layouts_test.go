package provenance

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/internal/postcard"
)

// july2026Fixture returns the 132-byte PROVENANCE payload transcribed,
// field by field, from docs/remote/change-format/sections.md#provenance-example,
// Atomic's july_2026_legacy_provenance.hex.
func july2026Fixture() []byte {
	return cat(
		[]byte{0x01},                       // list: 1 record
		[]byte{0x09},                       // vendor: AIVendor index 9 = Local
		[]byte{0x0a}, []byte("moonshotai"), // model
		[]byte{0x07}, []byte("kimi-k3"), // model_version (a bare String)
		[]byte{0x00},                     // tool: Api
		[]byte{0x04},                     // suggestion_type: Review
		[]byte{0x08}, []byte("opencode"), // agent_name
		[]byte{0x00}, // agent_version: None
		[]byte{0x03}, // prompt: None
		[]byte{0x00}, // system_prompt_hash: None
		[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},             // tokens
		[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // cost.usd
		[]byte{0x00}, // cost.micro_usd
		[]byte{0x00}, // temperature: None
		[]byte{0x01, 0x86, 0xe3, 0xea, 0xa5, 0x0d}, // timestamp: Some(1784502467)
		[]byte{0x00},                                                 // request_id: None
		[]byte{0x01, 0x1e}, []byte("ses_08365450affeTbe2zvcG63oKEX"), // session_id
		[]byte{0x02}, // metadata: 2 pairs
		[]byte{0x0b}, []byte("turn_number"), []byte{0x01}, []byte("1"),
		[]byte{0x0a}, []byte("agent_name"), []byte{0x08}, []byte("opencode"),
		[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // agent_mode .. task_plan
	)
}

func TestJuly2026FixtureIs132Bytes(t *testing.T) {
	require.Len(t, july2026Fixture(), 132)
}

func TestJuly2026FixtureCurrentLayoutFails(t *testing.T) {
	got, err := DecodeAs(july2026Fixture(), LayoutCurrent)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrMalformed)
	require.ErrorIs(t, err, postcard.ErrBadOption)
	var pe *postcard.Error
	require.ErrorAs(t, err, &pe)
	require.Equal(t, int64(13), pe.Offset)
}

// TestJuly2026FixtureFullReaderOrder checks the misreading in
// docs/remote/change-format/sections.md#july-2026-misread.
func TestJuly2026FixtureFullReaderOrder(t *testing.T) {
	fixture := july2026Fixture()
	got, layout, err := Decode(fixture, FullReaderOrder())
	require.NoError(t, err)
	require.Equal(t, LayoutBeforeXai, layout)
	require.Equal(t, []Record{{
		Vendor:         Vendor{Kind: VendorOther, Name: "moonshotai"},
		Model:          "kimi-k3",
		Tool:           Tool{Kind: ToolCLI, Name: "opencode"},
		SuggestionType: SuggestionComplete,
		Prompt:         Prompt{Kind: PromptNone},
		Timestamp:      ptr(int64(1_784_502_467)),
		SessionID:      ptr("ses_08365450affeTbe2zvcG63oKEX"),
		Metadata:       []Pair{{"turn_number", "1"}, {"agent_name", "opencode"}},
	}}, got)

	// The BeforeXai reading uses all 132 bytes, with nothing left over.
	exact, err := postcard.DecodeExact(fixture, func(d *postcard.Decoder) []Record {
		return postcard.Seq(d, decodeRecordBeforeXai)
	})
	require.NoError(t, err)
	require.Equal(t, got, exact)
}

func TestJuly2026FixtureSectionReaderOrder(t *testing.T) {
	got, layout, err := Decode(july2026Fixture(), SectionReaderOrder())
	require.NoError(t, err)
	require.Equal(t, LayoutJuly2026, layout)
	want := []Record{{
		Vendor:         Vendor{Kind: VendorLocal},
		Model:          "moonshotai",
		ModelVersion:   ptr("kimi-k3"),
		Tool:           Tool{Kind: ToolAPI},
		SuggestionType: SuggestionReview,
		Prompt:         Prompt{Kind: PromptNone},
		Timestamp:      ptr(int64(1_784_502_467)),
		SessionID:      ptr("ses_08365450affeTbe2zvcG63oKEX"),
		// agent_name opencode is already in the metadata, so it is not
		// appended again; agent_version is None, so nothing is added for it.
		Metadata: []Pair{{"turn_number", "1"}, {"agent_name", "opencode"}},
	}}
	require.Equal(t, want, got)

	t.Run("re-encodes in the current layout", func(t *testing.T) {
		out, err := AppendPayload(nil, got)
		require.NoError(t, err)
		require.Equal(t, cat(
			[]byte{0x01, 0x09},
			[]byte{0x0a}, []byte("moonshotai"),
			[]byte{0x01, 0x07}, []byte("kimi-k3"), // model_version: Some
			[]byte{0x00, 0x04}, // tool, suggestion_type; no agent fields
			[]byte{0x03, 0x00},
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, []byte{0x00},
			[]byte{0x00},
			[]byte{0x01, 0x86, 0xe3, 0xea, 0xa5, 0x0d},
			[]byte{0x00},
			[]byte{0x01, 0x1e}, []byte("ses_08365450affeTbe2zvcG63oKEX"),
			[]byte{0x02},
			[]byte{0x0b}, []byte("turn_number"), []byte{0x01}, []byte("1"),
			[]byte{0x0a}, []byte("agent_name"), []byte{0x08}, []byte("opencode"),
			[]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		), out)
		require.Contains(t, string(out), "\x01\x07kimi-k3")

		// The re-encoded payload is current, so the current layout reads
		// it back unchanged.
		back, layout, err := Decode(out, SectionReaderOrder())
		require.NoError(t, err)
		require.Equal(t, LayoutCurrent, layout)
		require.Equal(t, want, back)
	})
}

func TestJuly2026AgentFieldsAppendedToMetadata(t *testing.T) {
	// head: vendor Anthropic, empty model, model version "v", tool Api,
	// suggestion type Complete.
	head := cat([]byte{1, 0}, str(""), str("v"), []byte{0, 0})

	cases := []struct {
		name         string
		agentName    string
		agentVersion []byte
		metadata     []byte
		want         []Pair
	}{
		{
			name:         "no metadata, no version",
			agentName:    "opencode",
			agentVersion: []byte{0},
			metadata:     []byte{0},
			want:         []Pair{{"agent_name", "opencode"}},
		},
		{
			name:         "version present",
			agentName:    "opencode",
			agentVersion: some(str("1.2")...),
			metadata:     cat(uv(1), str("k"), str("v")),
			want:         []Pair{{"k", "v"}, {"agent_name", "opencode"}, {"agent_version", "1.2"}},
		},
		{
			name:         "empty agent name is still appended",
			agentName:    "",
			agentVersion: []byte{0},
			metadata:     []byte{0},
			want:         []Pair{{"agent_name", ""}},
		},
		{
			name:         "same key, different value is appended",
			agentName:    "opencode",
			agentVersion: some(str("2")...),
			metadata:     cat(uv(2), str("agent_name"), str("other"), str("agent_version"), str("2")),
			want:         []Pair{{"agent_name", "other"}, {"agent_version", "2"}, {"agent_name", "opencode"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := cat(head, str(tc.agentName), tc.agentVersion,
				[]byte{3, 0}, []byte{0, 0, 0, 0, 0, 0}, f64(0), []byte{0},
				[]byte{0, 0, 0, 0}, tc.metadata, []byte{0, 0, 0, 0, 0, 0, 0})
			got, err := DecodeAs(payload, LayoutJuly2026)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, ptr("v"), got[0].ModelVersion)
			require.Equal(t, tc.want, got[0].Metadata)
		})
	}
}

// TestVendorIndex8 checks that index 8 is XAI in the current numbering
// and Local in the pre-xAI one, and that the full reader order reads it
// as XAI because the current layout decodes
// (docs/remote/change-format/sections.md#aivendorv0).
func TestVendorIndex8(t *testing.T) {
	_, valid := emptyRecord()
	payload := cat([]byte{1}, valid)
	payload[1] = 8

	current, err := DecodeAs(payload, LayoutCurrent)
	require.NoError(t, err)
	require.Equal(t, Vendor{Kind: VendorXAI}, current[0].Vendor)

	before, err := DecodeAs(payload, LayoutBeforeXai)
	require.NoError(t, err)
	require.Equal(t, Vendor{Kind: VendorLocal}, before[0].Vendor)

	// The same record in the 14-field V0 layout.
	v0 := cat([]byte{1, 8}, str(""), []byte{0, 0, 0}, []byte{3, 0},
		[]byte{0, 0, 0, 0, 0}, f64(0), []byte{0}, []byte{0, 0, 0, 0}, []byte{0})
	old, err := DecodeAs(v0, LayoutV0)
	require.NoError(t, err)
	require.Equal(t, Vendor{Kind: VendorLocal}, old[0].Vendor)

	got, layout, err := Decode(payload, FullReaderOrder())
	require.NoError(t, err)
	require.Equal(t, LayoutCurrent, layout)
	require.Equal(t, Vendor{Kind: VendorXAI}, got[0].Vendor)
}

func TestVendorV0Numbering(t *testing.T) {
	want := []Vendor{
		{Kind: VendorAnthropic}, {Kind: VendorOpenAI}, {Kind: VendorGoogle},
		{Kind: VendorMeta}, {Kind: VendorMistral}, {Kind: VendorCohere},
		{Kind: VendorAmazonBedrock}, {Kind: VendorAzureOpenAI},
		{Kind: VendorLocal}, {Kind: VendorOther, Name: "acme"},
	}
	for i, v := range want {
		t.Run(v.Kind.String(), func(t *testing.T) {
			vb := []byte{byte(i)}
			if v.Kind == VendorOther {
				vb = cat(vb, str("acme"))
			}
			payload := cat([]byte{1}, vb, []byte{0, 0, 0, 0}, emptyFields())
			got, err := DecodeAs(payload, LayoutBeforeXai)
			require.NoError(t, err)
			require.Equal(t, v, got[0].Vendor)
		})
	}

	t.Run("index 10 is unknown", func(t *testing.T) {
		payload := cat([]byte{1, 10}, str("acme"), []byte{0, 0, 0}, emptyFields())
		got, err := DecodeAs(payload, LayoutBeforeXai)
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrMalformed)
		require.ErrorIs(t, err, postcard.ErrUnknownVariant)
		var pe *postcard.Error
		require.ErrorAs(t, err, &pe)
		require.Equal(t, int64(1), pe.Offset)
	})
}

// v0Record returns a ProvenanceV0 record like the one in Atomic's
// postcard_decoder_reads_provenance_before_reasoning_fields test, its
// bytes, and the record it converts to.
func v0Record() ([]byte, Record) {
	b := cat(
		[]byte{1},                  // vendor: AIVendorV0 OpenAI
		str("gpt-4"),               // model
		some(str("legacy")...),     // model_version
		[]byte{4}, str("opencode"), // tool: Cli(opencode)
		[]byte{0},                               // suggestion_type: Complete
		[]byte{3},                               // prompt: None
		[]byte{0},                               // system_prompt_hash: None
		uv(100), uv(25), uv(125), uv(10), uv(5), // five token counters
		f64(math.Float64bits(0.00125)), uv(1250), // cost
		some(uv(700)...),                    // temperature
		some(zz(1_700_000_000)...),          // timestamp
		some(str("request-1")...),           // request_id
		some(str("session-1")...),           // session_id
		uv(1), str("source"), str("legacy"), // metadata
	)
	r := Record{
		Vendor:         Vendor{Kind: VendorOpenAI},
		Model:          "gpt-4",
		ModelVersion:   ptr("legacy"),
		Tool:           Tool{Kind: ToolCLI, Name: "opencode"},
		SuggestionType: SuggestionComplete,
		Prompt:         Prompt{Kind: PromptNone},
		Tokens:         TokenUsage{Input: 100, Output: 25, Total: 125, CacheRead: 10, CacheWrite: 5},
		Cost:           Cost{USD: 0.00125, MicroUSD: 1250},
		Temperature:    ptr(uint32(700)),
		Timestamp:      ptr(int64(1_700_000_000)),
		RequestID:      ptr("request-1"),
		SessionID:      ptr("session-1"),
		Metadata:       []Pair{{"source", "legacy"}},
	}
	return b, r
}

func TestV0Conversion(t *testing.T) {
	b, want := v0Record()
	payload := cat([]byte{1}, b)

	got, err := DecodeAs(payload, LayoutV0)
	require.NoError(t, err)
	require.Equal(t, []Record{want}, got)
	require.Zero(t, got[0].Tokens.Reasoning)
	require.Nil(t, got[0].AgentMode)
	require.Nil(t, got[0].TaskPlan)

	// Neither the current nor the BeforeXai layout reads it, so the full
	// reader order falls through to V0.
	got, layout, err := Decode(payload, FullReaderOrder())
	require.NoError(t, err)
	require.Equal(t, LayoutV0, layout)
	require.Equal(t, []Record{want}, got)

	// It re-encodes in the current layout with the seven new fields None
	// and a reasoning counter of 0.
	out, err := AppendPayload(nil, got)
	require.NoError(t, err)
	back, err := DecodeAs(out, LayoutCurrent)
	require.NoError(t, err)
	require.Equal(t, got, back)
}

// TestV0TrailingJunk checks that a layout decodes whatever bytes follow
// the list, as postcard::from_bytes does, so the junk does not change
// which layout wins.
func TestV0TrailingJunk(t *testing.T) {
	b, want := v0Record()
	payload := cat([]byte{1}, b, []byte{0xff, 0xff, 0xff})

	got, err := DecodeAs(payload, LayoutV0)
	require.NoError(t, err)
	require.Equal(t, []Record{want}, got)

	got, layout, err := Decode(payload, FullReaderOrder())
	require.NoError(t, err)
	require.Equal(t, LayoutV0, layout)
	require.Equal(t, []Record{want}, got)
}

func TestDecodeReturnsFirstLayoutsError(t *testing.T) {
	// A truncated record decodes in no layout.
	payload := cat([]byte{1}, []byte{0})
	for name, order := range map[string][]Layout{
		"full":    FullReaderOrder(),
		"section": SectionReaderOrder(),
	} {
		t.Run(name, func(t *testing.T) {
			got, layout, err := Decode(payload, order)
			require.Nil(t, got)
			require.Equal(t, LayoutCurrent, layout)
			require.ErrorIs(t, err, ErrMalformed)
			require.ErrorIs(t, err, postcard.ErrUnexpectedEnd)
			require.ErrorContains(t, err, "Current layout")
		})
	}

	t.Run("current error reported when a later layout fails differently", func(t *testing.T) {
		// The fixture fails in the current layout with a bad option at
		// offset 13; with only Current and V0 to try, the V0 failure is
		// not the one reported.
		got, layout, err := Decode(july2026Fixture()[:40], []Layout{LayoutCurrent, LayoutV0})
		require.Nil(t, got)
		require.Equal(t, LayoutCurrent, layout)
		require.ErrorIs(t, err, postcard.ErrBadOption)
		var pe *postcard.Error
		require.ErrorAs(t, err, &pe)
		require.Equal(t, int64(13), pe.Offset)
	})
}

func TestDecodeRefusesBadOrders(t *testing.T) {
	_, valid := emptyRecord()
	payload := cat([]byte{1}, valid)

	t.Run("empty", func(t *testing.T) {
		got, _, err := Decode(payload, nil)
		require.Nil(t, got)
		require.ErrorIs(t, err, ErrUnsupportedLayout)
	})

	t.Run("unknown layout after one that would decode", func(t *testing.T) {
		got, layout, err := Decode(payload, []Layout{LayoutCurrent, Layout(7)})
		require.Nil(t, got)
		require.Equal(t, Layout(7), layout)
		require.ErrorIs(t, err, ErrUnsupportedLayout)
	})
}

func TestDecodeEmptyList(t *testing.T) {
	got, layout, err := Decode([]byte{0}, FullReaderOrder())
	require.NoError(t, err)
	require.Nil(t, got)
	require.Equal(t, LayoutCurrent, layout)
}

func TestOrders(t *testing.T) {
	require.Equal(t, []Layout{LayoutCurrent, LayoutBeforeXai, LayoutV0}, FullReaderOrder())
	require.Equal(t, []Layout{LayoutCurrent, LayoutJuly2026}, SectionReaderOrder())

	// Each call returns a new slice.
	o := FullReaderOrder()
	o[0] = LayoutJuly2026
	require.Equal(t, LayoutCurrent, FullReaderOrder()[0])
}
