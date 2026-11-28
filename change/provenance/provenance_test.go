package provenance

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/postcard"
)

// The helpers below build expected payloads by hand, independently of the
// postcard encoder, so a round trip is checked against bytes the codec did
// not produce.

// uv returns the minimal varint of n.
func uv(n uint64) []byte { return binary.AppendUvarint(nil, n) }

// zz returns the zigzag varint of n.
func zz(n int64) []byte { return uv(uint64(n<<1) ^ uint64(n>>63)) }

// str returns a postcard string: a varint length, then the bytes.
func str(s string) []byte { return append(uv(uint64(len(s))), s...) }

// some returns an option tag of Some followed by b.
func some(b ...byte) []byte { return append([]byte{1}, b...) }

// f64 returns the 8 little-endian bytes of bits.
func f64(bits uint64) []byte { return binary.LittleEndian.AppendUint64(nil, bits) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

func hashOf(b byte) ident.Hash {
	var h ident.Hash
	for i := range h {
		h[i] = b + byte(i)
	}
	return h
}

// fullRecord returns a record with every field set, and its bytes.
func fullRecord() (Record, []byte) {
	h := hashOf(0x40)
	r := Record{
		Vendor:           Vendor{Kind: VendorOther, Name: "openrouter"},
		Model:            "claude-sonnet-4-20250514",
		ModelVersion:     ptr("2025-05-14"),
		Tool:             Tool{Kind: ToolCodeReview, Name: "reviewer"},
		SuggestionType:   SuggestionTesting,
		Prompt:           Prompt{Kind: PromptFull, Text: "write the tests"},
		SystemPromptHash: &h,
		Tokens: TokenUsage{
			Input: 1, Output: 2, Total: 300, CacheRead: 4, CacheWrite: 5, Reasoning: 1 << 40,
		},
		Cost:               Cost{USD: 0.125, MicroUSD: 125_000},
		Temperature:        ptr(uint32(700)),
		Timestamp:          ptr(int64(1_784_502_467_000)),
		RequestID:          ptr("req_1"),
		SessionID:          ptr("ses_1"),
		Metadata:           []Pair{{"turn_number", "1"}, {"k", "v"}, {"k", "v2"}},
		AgentMode:          ptr("build"),
		FinishReason:       ptr("tool-calls"),
		StepCount:          ptr(uint32(3)),
		SessionSlug:        ptr("brave-otter"),
		ReasoningSignature: ptr("sig"),
		ReasoningText:      ptr("a\n---\nb"),
		TaskPlan:           ptr(`[{"task":"x"}]`),
	}
	b := cat(
		[]byte{10}, str("openrouter"), // vendor: Other(openrouter)
		str("claude-sonnet-4-20250514"),
		some(str("2025-05-14")...),
		[]byte{6}, str("reviewer"), // tool: CodeReview(reviewer)
		[]byte{8},                         // suggestion_type: Testing
		[]byte{1}, str("write the tests"), // prompt: Full
		some(h[:]...),
		uv(1), uv(2), uv(300), uv(4), uv(5), uv(1<<40),
		f64(math.Float64bits(0.125)), uv(125_000),
		some(uv(700)...),
		some(zz(1_784_502_467_000)...),
		some(str("req_1")...),
		some(str("ses_1")...),
		uv(3), str("turn_number"), str("1"), str("k"), str("v"), str("k"), str("v2"),
		some(str("build")...),
		some(str("tool-calls")...),
		some(uv(3)...),
		some(str("brave-otter")...),
		some(str("sig")...),
		some(str("a\n---\nb")...),
		some(str(`[{"task":"x"}]`)...),
	)
	return r, b
}

// emptyFields is the bytes of every field after vendor, model,
// model_version, tool and suggestion_type for a record holding nothing:
// prompt None, no system prompt hash, zero counters and cost, and every
// option None.
func emptyFields() []byte {
	return cat(
		[]byte{3},                // prompt: None
		[]byte{0},                // system_prompt_hash: None
		[]byte{0, 0, 0, 0, 0, 0}, // tokens
		f64(0), []byte{0},        // cost
		[]byte{0, 0, 0, 0},          // temperature, timestamp, request_id, session_id
		[]byte{0},                   // metadata: empty
		[]byte{0, 0, 0, 0, 0, 0, 0}, // agent_mode .. task_plan
	)
}

// emptyRecord returns the record emptyFields describes, with vendor
// Anthropic, an empty model, no model version, tool Api and suggestion
// type Complete, and its bytes.
func emptyRecord() (Record, []byte) {
	return Record{Prompt: Prompt{Kind: PromptNone}}, cat([]byte{0, 0, 0, 0, 0}, emptyFields())
}

func roundTrip(t *testing.T, payload []byte, want []Record) {
	t.Helper()
	got, err := DecodeAs(payload, LayoutCurrent)
	require.NoError(t, err)
	require.Equal(t, want, got)

	out, err := AppendPayload(nil, got)
	require.NoError(t, err)
	require.Equal(t, payload, out)
}

func TestRoundTripEveryField(t *testing.T) {
	full, fullBytes := fullRecord()
	empty, emptyBytes := emptyRecord()
	payload := cat([]byte{2}, fullBytes, emptyBytes)
	roundTrip(t, payload, []Record{full, empty})
}

func TestRoundTripEveryVariant(t *testing.T) {
	// head returns the bytes up to and including suggestion_type, with an
	// empty model and no model version.
	head := func(vendor, tool []byte, suggestion byte) []byte {
		return cat(vendor, str(""), []byte{0}, tool, []byte{suggestion})
	}

	t.Run("vendor", func(t *testing.T) {
		for k := VendorAnthropic; k <= VendorOther; k++ {
			t.Run(k.String(), func(t *testing.T) {
				v := Vendor{Kind: k}
				vb := []byte{byte(k)}
				if k == VendorOther {
					v.Name = "acme"
					vb = cat(vb, str("acme"))
				}
				want := Record{Vendor: v, Prompt: Prompt{Kind: PromptNone}}
				roundTrip(t, cat([]byte{1}, head(vb, []byte{0}, 0), emptyFields()), []Record{want})
			})
		}
	})

	t.Run("tool", func(t *testing.T) {
		for k := ToolAPI; k <= ToolOther; k++ {
			t.Run(k.String(), func(t *testing.T) {
				tool := Tool{Kind: k}
				tb := []byte{byte(k)}
				if k != ToolAPI && k != ToolChat {
					tool.Name = "name-" + k.String()
					tb = cat(tb, str(tool.Name))
				}
				want := Record{Tool: tool, Prompt: Prompt{Kind: PromptNone}}
				roundTrip(t, cat([]byte{1}, head([]byte{0}, tb, 0), emptyFields()), []Record{want})
			})
		}
	})

	t.Run("suggestion type", func(t *testing.T) {
		for s := SuggestionComplete; s <= SuggestionTesting; s++ {
			t.Run(s.String(), func(t *testing.T) {
				want := Record{SuggestionType: s, Prompt: Prompt{Kind: PromptNone}}
				roundTrip(t, cat([]byte{1}, head([]byte{0}, []byte{0}, byte(s)), emptyFields()), []Record{want})
			})
		}
	})

	t.Run("prompt", func(t *testing.T) {
		h := hashOf(0x80)
		prompts := []struct {
			p Prompt
			b []byte
		}{
			{Prompt{Kind: PromptHashed, Hash: h}, cat([]byte{0}, h[:])},
			{Prompt{Kind: PromptFull, Text: "hello"}, cat([]byte{1}, str("hello"))},
			{Prompt{Kind: PromptCompressed, Compressed: []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00}}, cat([]byte{2}, uv(5), []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00})},
			{Prompt{Kind: PromptNone}, []byte{3}},
		}
		for _, tc := range prompts {
			t.Run(tc.p.Kind.String(), func(t *testing.T) {
				// emptyFields starts with prompt None; replace it.
				payload := cat([]byte{1}, head([]byte{0}, []byte{0}, 0), tc.b, emptyFields()[1:])
				roundTrip(t, payload, []Record{{Prompt: tc.p}})
			})
		}
	})
}

func TestCompressedPromptDoesNotAliasPayload(t *testing.T) {
	payload := cat([]byte{1}, []byte{0, 0, 0, 0, 0}, []byte{2}, uv(3), []byte{7, 8, 9}, emptyFields()[1:])
	got, err := DecodeAs(payload, LayoutCurrent)
	require.NoError(t, err)
	require.Equal(t, []byte{7, 8, 9}, got[0].Prompt.Compressed)

	payload[8] = 0xff
	require.Equal(t, []byte{7, 8, 9}, got[0].Prompt.Compressed)
}

func TestTrailingBytesIgnored(t *testing.T) {
	full, fullBytes := fullRecord()
	payload := cat([]byte{1}, fullBytes)

	got, err := DecodeAs(append(payload, 0xff), LayoutCurrent)
	require.NoError(t, err)
	require.Equal(t, []Record{full}, got)

	// The junk is not part of the list, so it is not written back.
	out, err := AppendPayload(nil, got)
	require.NoError(t, err)
	require.Equal(t, payload, out)
}

func TestUSDBitsPreserved(t *testing.T) {
	cases := map[string]uint64{
		"negative zero":    math.Float64bits(math.Copysign(0, -1)),
		"positive zero":    0,
		"NaN with payload": 0x7ff8_0000_dead_beef,
		"negative NaN":     0xfff8_0000_0000_0001,
		"+Inf":             math.Float64bits(math.Inf(1)),
		"subnormal":        1,
		"0.1":              math.Float64bits(0.1),
	}
	for name, bits := range cases {
		t.Run(name, func(t *testing.T) {
			payload := cat([]byte{1}, []byte{0, 0, 0, 0, 0},
				[]byte{3, 0}, []byte{0, 0, 0, 0, 0, 0},
				f64(bits), uv(42),
				[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
			got, err := DecodeAs(payload, LayoutCurrent)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, bits, math.Float64bits(got[0].Cost.USD))
			require.Equal(t, uint64(42), got[0].Cost.MicroUSD)

			out, err := AppendPayload(nil, got)
			require.NoError(t, err)
			require.Equal(t, payload, out)
		})
	}
}

// requireDecodeError checks that DecodeAs fails on payload with a postcard
// error wrapping want, at offset.
func requireDecodeError(t *testing.T, payload []byte, want error, offset int64) {
	t.Helper()
	got, err := DecodeAs(payload, LayoutCurrent)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrMalformed)
	require.ErrorIs(t, err, want)
	var pe *postcard.Error
	require.True(t, errors.As(err, &pe), "%v is not a *postcard.Error", err)
	require.Equal(t, offset, pe.Offset)
}

func TestDecodeErrors(t *testing.T) {
	_, valid := emptyRecord()

	t.Run("unknown vendor", func(t *testing.T) {
		payload := cat([]byte{1}, valid)
		payload[1] = vendorVariants
		requireDecodeError(t, payload, postcard.ErrUnknownVariant, 1)
	})

	t.Run("unknown tool", func(t *testing.T) {
		payload := cat([]byte{1}, valid)
		payload[4] = toolVariants
		requireDecodeError(t, payload, postcard.ErrUnknownVariant, 4)
	})

	t.Run("unknown suggestion type", func(t *testing.T) {
		payload := cat([]byte{1}, valid)
		payload[5] = suggestionVariants
		requireDecodeError(t, payload, postcard.ErrUnknownVariant, 5)
	})

	t.Run("unknown prompt", func(t *testing.T) {
		payload := cat([]byte{1}, valid)
		payload[6] = promptVariants
		requireDecodeError(t, payload, postcard.ErrUnknownVariant, 6)
	})

	t.Run("bad option tag", func(t *testing.T) {
		payload := cat([]byte{1}, valid)
		payload[3] = 2 // model_version
		requireDecodeError(t, payload, postcard.ErrBadOption, 3)
	})

	t.Run("invalid UTF-8", func(t *testing.T) {
		payload := cat([]byte{1}, []byte{0}, []byte{1, 0xff}, valid[2:])
		requireDecodeError(t, payload, postcard.ErrBadUTF8, 2)
	})

	t.Run("truncated", func(t *testing.T) {
		payload := cat([]byte{1}, valid)
		requireDecodeError(t, payload[:len(payload)-1], postcard.ErrUnexpectedEnd, int64(len(payload)-1))
	})

	t.Run("count beyond input", func(t *testing.T) {
		requireDecodeError(t, cat([]byte{2}, valid), postcard.ErrUnexpectedEnd, int64(1+len(valid)))
	})
}

func TestEmptyList(t *testing.T) {
	got, err := DecodeAs([]byte{0}, LayoutCurrent)
	require.NoError(t, err)
	require.Nil(t, got)

	out, err := AppendPayload(nil, nil)
	require.NoError(t, err)
	require.Equal(t, []byte{0}, out)
}

func TestUnsupportedLayout(t *testing.T) {
	_, valid := emptyRecord()
	for _, l := range []Layout{LayoutBeforeXai, LayoutV0, LayoutJuly2026, Layout(9)} {
		t.Run(l.String(), func(t *testing.T) {
			got, err := DecodeAs(cat([]byte{1}, valid), l)
			require.Nil(t, got)
			require.ErrorIs(t, err, ErrUnsupportedLayout)
		})
	}
}

func TestAppendPayloadAppends(t *testing.T) {
	r, b := emptyRecord()
	out, err := AppendPayload([]byte("prefix"), []Record{r})
	require.NoError(t, err)
	require.Equal(t, cat([]byte("prefix"), []byte{1}, b), out)
}

func TestAppendPayloadRefusesInvalidRecords(t *testing.T) {
	valid, _ := emptyRecord()
	cases := map[string]func(*Record){
		"unknown vendor":            func(r *Record) { r.Vendor.Kind = vendorVariants },
		"name on a named vendor":    func(r *Record) { r.Vendor = Vendor{Kind: VendorLocal, Name: "x"} },
		"unknown tool":              func(r *Record) { r.Tool.Kind = toolVariants },
		"name on Api":               func(r *Record) { r.Tool = Tool{Kind: ToolAPI, Name: "x"} },
		"name on Chat":              func(r *Record) { r.Tool = Tool{Kind: ToolChat, Name: "x"} },
		"unknown suggestion type":   func(r *Record) { r.SuggestionType = suggestionVariants },
		"unknown prompt":            func(r *Record) { r.Prompt.Kind = promptVariants },
		"hash on a None prompt":     func(r *Record) { r.Prompt.Hash = hashOf(1) },
		"text on a Hashed prompt":   func(r *Record) { r.Prompt = Prompt{Kind: PromptHashed, Text: "x"} },
		"bytes on a Full prompt":    func(r *Record) { r.Prompt = Prompt{Kind: PromptFull, Compressed: []byte{1}} },
		"invalid UTF-8 model":       func(r *Record) { r.Model = "\xff" },
		"invalid UTF-8 option":      func(r *Record) { r.TaskPlan = ptr("\xff") },
		"invalid UTF-8 vendor name": func(r *Record) { r.Vendor = Vendor{Kind: VendorOther, Name: "\xff"} },
		"invalid UTF-8 metadata":    func(r *Record) { r.Metadata = []Pair{{"k", "\xff"}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bad := valid
			mutate(&bad)
			dst := []byte("prefix")
			out, err := AppendPayload(dst, []Record{valid, bad})
			require.ErrorIs(t, err, ErrInvalidRecord)
			require.ErrorContains(t, err, "record 1")
			require.Equal(t, []byte("prefix"), out)
		})
	}
}

func TestTime(t *testing.T) {
	cases := []struct {
		name string
		ts   int64
		want time.Time
	}{
		{"largest seconds", 99_999_999_999, time.Unix(99_999_999_999, 0).UTC()},
		{"smallest milliseconds", 100_000_000_000, time.UnixMilli(100_000_000_000).UTC()},
		{"negative seconds", -99_999_999_999, time.Unix(-99_999_999_999, 0).UTC()},
		{"negative milliseconds", -100_000_000_000, time.UnixMilli(-100_000_000_000).UTC()},
		{"historical seconds", 1_784_502_467, time.Unix(1_784_502_467, 0).UTC()},
		{"current milliseconds", 1_784_502_467_123, time.UnixMilli(1_784_502_467_123).UTC()},
		{"zero", 0, time.Unix(0, 0).UTC()},
		{"minimum", math.MinInt64, time.UnixMilli(math.MinInt64).UTC()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Record{Timestamp: ptr(tc.ts)}.Time()
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("none", func(t *testing.T) {
		got, ok := Record{}.Time()
		require.False(t, ok)
		require.True(t, got.IsZero())
	})

	// 100,000,000,000 seconds would be in the year 5138; as milliseconds
	// it is 1973.
	got, _ := Record{Timestamp: ptr(int64(100_000_000_000))}.Time()
	require.Equal(t, 1973, got.Year())
}

func TestStrings(t *testing.T) {
	require.Equal(t, "XAI", VendorXAI.String())
	require.Equal(t, "Other", VendorOther.String())
	require.Equal(t, "VendorKind(11)", VendorKind(11).String())
	require.Equal(t, "IdePlugin", ToolIDEPlugin.String())
	require.Equal(t, "ToolKind(8)", ToolKind(8).String())
	require.Equal(t, "Collaborative", SuggestionCollaborative.String())
	require.Equal(t, "SuggestionType(9)", SuggestionType(9).String())
	require.Equal(t, "Compressed", PromptCompressed.String())
	require.Equal(t, "PromptKind(4)", PromptKind(4).String())
	require.Equal(t, "July2026", LayoutJuly2026.String())
	require.Equal(t, "Layout(4)", Layout(4).String())
}
