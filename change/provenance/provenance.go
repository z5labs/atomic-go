// Package provenance reads and writes the AI provenance an Atomic change
// records: who made a contribution (the vendor and model), how (the tool
// and the kind of suggestion), from what (the prompt), at what cost (tokens
// and dollars), and in which session. A change's PROVENANCE section is a
// list of [Record]s.
//
// [DecodeAs] decodes a PROVENANCE payload in a named [Layout], and
// [AppendPayload] writes one, in the current layout only, as Atomic's
// writer does. The payload is documented in
// docs/remote/change-format/sections.md#provenance-section.
package provenance

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/postcard"
)

var (
	// ErrMalformed is wrapped by the error [DecodeAs] returns for a
	// payload that does not decode in the layout asked for. The error
	// also wraps the postcard failure, which carries the byte offset at
	// which the value that failed begins.
	ErrMalformed = errors.New("provenance: malformed payload")

	// ErrInvalidRecord is wrapped by the error [AppendPayload] returns for
	// a record the payload cannot carry.
	ErrInvalidRecord = errors.New("provenance: invalid record")

	// ErrUnsupportedLayout is wrapped by the error [DecodeAs] returns for
	// a layout it cannot decode.
	ErrUnsupportedLayout = errors.New("provenance: unsupported layout")
)

// Layout is a PROVENANCE payload layout. Postcard writes no field names or
// type tags, so the layout a payload was written in cannot be read from
// its bytes and has to be named
// (docs/remote/change-format/sections.md#historical-layouts).
type Layout uint8

const (
	// LayoutCurrent is the 21-field Provenance every current writer emits.
	LayoutCurrent Layout = iota

	// LayoutBeforeXai is ProvenanceBeforeXai: the current fields, with the
	// vendor numbered as it was before xAI was added.
	LayoutBeforeXai

	// LayoutV0 is ProvenanceV0: the first 14 fields, with the pre-xAI
	// vendor numbering and five token counters.
	LayoutV0

	// LayoutJuly2026 is July2026Provenance: a plain model version and two
	// agent fields before the prompt.
	LayoutJuly2026
)

// String returns the layout's name, or Layout(n) for a value with no name.
func (l Layout) String() string {
	switch l {
	case LayoutCurrent:
		return "Current"
	case LayoutBeforeXai:
		return "BeforeXai"
	case LayoutV0:
		return "V0"
	case LayoutJuly2026:
		return "July2026"
	default:
		return fmt.Sprintf("Layout(%d)", uint8(l))
	}
}

// DecodeAs decodes the PROVENANCE payload b, a list of records, in layout
// l. Like postcard::from_bytes, which Atomic reads the section with, it
// ignores bytes left over after the list
// (docs/remote/change-format/sections.md#encoding-rules). An empty list
// decodes to nil.
//
// Only [LayoutCurrent] is decoded so far; any other layout fails with an
// error wrapping [ErrUnsupportedLayout]. A payload that does not decode
// fails with an error wrapping both [ErrMalformed] and the postcard
// failure: a vendor, tool, suggestion type or prompt variant index Atomic
// does not declare is an unknown variant, an option tag other than 0 or 1
// a bad option, and a string that is not UTF-8 invalid UTF-8.
func DecodeAs(b []byte, l Layout) ([]Record, error) {
	if l != LayoutCurrent {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLayout, l)
	}
	records, err := postcard.Decode(b, func(d *postcard.Decoder) []Record {
		return postcard.Seq(d, decodeRecord)
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %s layout: %w", ErrMalformed, l, err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	return records, nil
}

// AppendPayload appends the PROVENANCE payload of records to dst, in the
// current layout, the only one Atomic writes: a count, then each record's
// 21 fields. Every field's value is written exactly as held, so a record
// decoded by [DecodeAs] encodes back to the bytes it came from, apart from
// any non-minimal varints those bytes held.
//
// It fails, with an error wrapping [ErrInvalidRecord], and returns dst
// unchanged, when a record holds a kind Atomic does not declare, a
// [Vendor.Name] on a vendor other than [VendorOther], a [Tool.Name] on
// [ToolAPI] or [ToolChat], a [Prompt] field its Kind does not carry, or a
// string that is not valid UTF-8. Each of those would otherwise be lost or
// unreadable.
func AppendPayload(dst []byte, records []Record) ([]byte, error) {
	for i, r := range records {
		if err := r.check(); err != nil {
			return dst, fmt.Errorf("record %d: %w", i, err)
		}
	}
	e := postcard.NewEncoder(dst)
	postcard.PutSeq(e, records, putRecord)
	return e.Bytes(), nil
}

// check reports whether r can be written.
func (r Record) check() error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidRecord, fmt.Sprintf(format, args...))
	}
	switch {
	case r.Vendor.Kind >= vendorVariants:
		return invalid("vendor %s is not one Atomic declares", r.Vendor.Kind)
	case r.Vendor.Kind != VendorOther && r.Vendor.Name != "":
		return invalid("vendor %s carries no name, but Name is %q", r.Vendor.Kind, r.Vendor.Name)
	case r.Tool.Kind >= toolVariants:
		return invalid("tool %s is not one Atomic declares", r.Tool.Kind)
	case !r.Tool.Kind.named() && r.Tool.Name != "":
		return invalid("tool %s carries no name, but Name is %q", r.Tool.Kind, r.Tool.Name)
	case r.SuggestionType >= suggestionVariants:
		return invalid("suggestion type %s is not one Atomic declares", r.SuggestionType)
	}
	if err := r.Prompt.check(); err != nil {
		return err
	}

	strs := []struct {
		name string
		s    *string
	}{
		{"vendor name", &r.Vendor.Name},
		{"model", &r.Model},
		{"model version", r.ModelVersion},
		{"tool name", &r.Tool.Name},
		{"prompt text", &r.Prompt.Text},
		{"request ID", r.RequestID},
		{"session ID", r.SessionID},
		{"agent mode", r.AgentMode},
		{"finish reason", r.FinishReason},
		{"session slug", r.SessionSlug},
		{"reasoning signature", r.ReasoningSignature},
		{"reasoning text", r.ReasoningText},
		{"task plan", r.TaskPlan},
	}
	for _, f := range strs {
		if f.s != nil && !utf8.ValidString(*f.s) {
			return invalid("%s is not valid UTF-8", f.name)
		}
	}
	for i, p := range r.Metadata {
		if !utf8.ValidString(p.Key) || !utf8.ValidString(p.Value) {
			return invalid("metadata pair %d is not valid UTF-8", i)
		}
	}
	return nil
}

// check reports whether p can be written: its kind is declared and it
// holds nothing its kind does not carry.
func (p Prompt) check() error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidRecord, fmt.Sprintf(format, args...))
	}
	if p.Kind >= promptVariants {
		return invalid("prompt %s is not one Atomic declares", p.Kind)
	}
	if p.Kind != PromptHashed && !p.Hash.IsZero() {
		return invalid("a %s prompt carries no hash", p.Kind)
	}
	if p.Kind != PromptFull && p.Text != "" {
		return invalid("a %s prompt carries no text", p.Kind)
	}
	if p.Kind != PromptCompressed && len(p.Compressed) > 0 {
		return invalid("a %s prompt carries no compressed bytes", p.Kind)
	}
	return nil
}

// decodeRecord decodes one record in the current layout
// (docs/remote/change-format/sections.md#provenance).
func decodeRecord(d *postcard.Decoder) Record {
	r := Record{
		Vendor:         decodeVendor(d),
		Model:          d.Str(),
		ModelVersion:   postcard.Option(d, (*postcard.Decoder).Str),
		Tool:           decodeTool(d),
		SuggestionType: SuggestionType(d.Variant(suggestionVariants)),
		Prompt:         decodePrompt(d),
	}
	if h := postcard.Option(d, (*postcard.Decoder).Array32); h != nil {
		hash := ident.Hash(*h)
		r.SystemPromptHash = &hash
	}
	r.Tokens = decodeTokens(d)
	r.Cost = Cost{USD: d.F64(), MicroUSD: d.U64()}
	r.Temperature = postcard.Option(d, (*postcard.Decoder).U32)
	r.Timestamp = postcard.Option(d, (*postcard.Decoder).I64)
	r.RequestID = postcard.Option(d, (*postcard.Decoder).Str)
	r.SessionID = postcard.Option(d, (*postcard.Decoder).Str)
	if m := postcard.Seq(d, decodePair); len(m) > 0 {
		r.Metadata = m
	}
	r.AgentMode = postcard.Option(d, (*postcard.Decoder).Str)
	r.FinishReason = postcard.Option(d, (*postcard.Decoder).Str)
	r.StepCount = postcard.Option(d, (*postcard.Decoder).U32)
	r.SessionSlug = postcard.Option(d, (*postcard.Decoder).Str)
	r.ReasoningSignature = postcard.Option(d, (*postcard.Decoder).Str)
	r.ReasoningText = postcard.Option(d, (*postcard.Decoder).Str)
	r.TaskPlan = postcard.Option(d, (*postcard.Decoder).Str)
	return r
}

// putRecord writes one record in the current layout.
func putRecord(e *postcard.Encoder, r Record) {
	putVendor(e, r.Vendor)
	e.Str(r.Model)
	postcard.PutOption(e, r.ModelVersion, (*postcard.Encoder).Str)
	putTool(e, r.Tool)
	e.Variant(uint32(r.SuggestionType))
	putPrompt(e, r.Prompt)
	e.OptionTag(r.SystemPromptHash != nil)
	if r.SystemPromptHash != nil {
		e.Array32(*r.SystemPromptHash)
	}
	putTokens(e, r.Tokens)
	e.F64(r.Cost.USD)
	e.U64(r.Cost.MicroUSD)
	postcard.PutOption(e, r.Temperature, (*postcard.Encoder).U32)
	postcard.PutOption(e, r.Timestamp, (*postcard.Encoder).I64)
	postcard.PutOption(e, r.RequestID, (*postcard.Encoder).Str)
	postcard.PutOption(e, r.SessionID, (*postcard.Encoder).Str)
	postcard.PutSeq(e, r.Metadata, putPair)
	postcard.PutOption(e, r.AgentMode, (*postcard.Encoder).Str)
	postcard.PutOption(e, r.FinishReason, (*postcard.Encoder).Str)
	postcard.PutOption(e, r.StepCount, (*postcard.Encoder).U32)
	postcard.PutOption(e, r.SessionSlug, (*postcard.Encoder).Str)
	postcard.PutOption(e, r.ReasoningSignature, (*postcard.Encoder).Str)
	postcard.PutOption(e, r.ReasoningText, (*postcard.Encoder).Str)
	postcard.PutOption(e, r.TaskPlan, (*postcard.Encoder).Str)
}

// decodeVendor decodes a current AIVendor.
func decodeVendor(d *postcard.Decoder) Vendor {
	v := Vendor{Kind: VendorKind(d.Variant(vendorVariants))}
	if v.Kind == VendorOther {
		v.Name = d.Str()
	}
	return v
}

func putVendor(e *postcard.Encoder, v Vendor) {
	e.Variant(uint32(v.Kind))
	if v.Kind == VendorOther {
		e.Str(v.Name)
	}
}

// decodeTool decodes an AITool.
func decodeTool(d *postcard.Decoder) Tool {
	t := Tool{Kind: ToolKind(d.Variant(toolVariants))}
	if d.Err() == nil && t.Kind.named() {
		t.Name = d.Str()
	}
	return t
}

func putTool(e *postcard.Encoder, t Tool) {
	e.Variant(uint32(t.Kind))
	if t.Kind.named() {
		e.Str(t.Name)
	}
}

// decodePrompt decodes a PromptContent. A compressed prompt's bytes are
// copied, so the record does not alias the payload.
func decodePrompt(d *postcard.Decoder) Prompt {
	p := Prompt{Kind: PromptKind(d.Variant(promptVariants))}
	if d.Err() != nil {
		return Prompt{}
	}
	switch p.Kind {
	case PromptHashed:
		p.Hash = d.Array32()
	case PromptFull:
		p.Text = d.Str()
	case PromptCompressed:
		p.Compressed = append([]byte{}, d.ByteSeq()...)
	}
	return p
}

func putPrompt(e *postcard.Encoder, p Prompt) {
	e.Variant(uint32(p.Kind))
	switch p.Kind {
	case PromptHashed:
		e.Array32(p.Hash)
	case PromptFull:
		e.Str(p.Text)
	case PromptCompressed:
		e.ByteSeq(p.Compressed)
	}
}

// decodeTokens decodes a current TokenUsage, six counters.
func decodeTokens(d *postcard.Decoder) TokenUsage {
	return TokenUsage{
		Input:      d.U64(),
		Output:     d.U64(),
		Total:      d.U64(),
		CacheRead:  d.U64(),
		CacheWrite: d.U64(),
		Reasoning:  d.U64(),
	}
}

func putTokens(e *postcard.Encoder, t TokenUsage) {
	e.U64(t.Input)
	e.U64(t.Output)
	e.U64(t.Total)
	e.U64(t.CacheRead)
	e.U64(t.CacheWrite)
	e.U64(t.Reasoning)
}

func decodePair(d *postcard.Decoder) Pair {
	return Pair{Key: d.Str(), Value: d.Str()}
}

func putPair(e *postcard.Encoder, p Pair) {
	e.Str(p.Key)
	e.Str(p.Value)
}
