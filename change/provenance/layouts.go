package provenance

import (
	"fmt"

	"github.com/z5labs/atomic-go/internal/postcard"
)

// FullReaderOrder returns the layouts Atomic's full change reader,
// Change::deserialize, tries a PROVENANCE payload in: [LayoutCurrent],
// then [LayoutBeforeXai], then [LayoutV0]
// (docs/remote/change-format/sections.md#historical-layouts). It returns a
// new slice on every call.
func FullReaderOrder() []Layout {
	return []Layout{LayoutCurrent, LayoutBeforeXai, LayoutV0}
}

// SectionReaderOrder returns the layouts Atomic's section-level helper,
// ReadSection::deserialize, tries a PROVENANCE payload in:
// [LayoutCurrent], then [LayoutJuly2026]
// (docs/remote/change-format/sections.md#historical-layouts). It returns a
// new slice on every call.
func SectionReaderOrder() []Layout {
	return []Layout{LayoutCurrent, LayoutJuly2026}
}

// Decode decodes the PROVENANCE payload b in each layout of order in turn,
// as [DecodeAs] does, and returns the records and layout of the first one
// that decodes. A layout decodes when its fields do, whatever bytes are
// left over after the list, as with postcard::from_bytes; leftover bytes
// never send Decode on to the next layout.
//
// When no layout decodes, Decode returns the error from the first layout
// in order, with that layout. Both of Atomic's orders begin with
// [LayoutCurrent], so that is the current layout's error, as Atomic
// reports. An empty order, or one holding a layout this package does not
// name, fails with an error wrapping [ErrUnsupportedLayout] before
// anything is decoded.
//
// The layout that decodes first is not always the one the payload was
// written in, because postcard writes no type tags or field names. The
// two orders are both exposed because Atomic's two readers disagree:
// [FullReaderOrder] reads the July 2026 example payload as
// [LayoutBeforeXai], with the vendor's name taken for the model and the
// model version for the tool's name, where [SectionReaderOrder] reads it
// as [LayoutJuly2026]
// (docs/remote/change-format/sections.md#july-2026-misread). And a pre-xAI
// record with vendor index 8 decodes in [LayoutCurrent] as [VendorXAI],
// where it was written as [VendorLocal]
// (docs/remote/change-format/sections.md#aivendorv0). The returned layout
// lets a caller tell which reading it got.
func Decode(b []byte, order []Layout) ([]Record, Layout, error) {
	if len(order) == 0 {
		return nil, LayoutCurrent, fmt.Errorf("%w: no layouts to try", ErrUnsupportedLayout)
	}
	for _, l := range order {
		if _, ok := recordDecoder(l); !ok {
			return nil, l, fmt.Errorf("%w: %s", ErrUnsupportedLayout, l)
		}
	}
	var firstErr error
	for _, l := range order {
		records, err := DecodeAs(b, l)
		if err == nil {
			return records, l, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, order[0], firstErr
}

// vendorV0Variants is the number of variants AIVendorV0 declares.
const vendorV0Variants = 10

// decodeVendorV0 decodes an AIVendorV0, the vendor numbering from before
// xAI was added, and converts it to the current vendor of the same name
// (docs/remote/change-format/sections.md#aivendorv0).
func decodeVendorV0(d *postcard.Decoder) Vendor {
	switch i := d.Variant(vendorV0Variants); i {
	case 8:
		return Vendor{Kind: VendorLocal}
	case 9:
		return Vendor{Kind: VendorOther, Name: d.Str()}
	default:
		return Vendor{Kind: VendorKind(i)}
	}
}

// decodeRecordBeforeXai decodes one ProvenanceBeforeXai: the current
// fields with an AIVendorV0
// (docs/remote/change-format/sections.md#provenancebeforexai).
func decodeRecordBeforeXai(d *postcard.Decoder) Record {
	r := decodeHead(d, decodeVendorV0)
	decodeBody(d, &r, decodeTokens)
	decodeAgentFields(d, &r)
	return r
}

// decodeRecordV0 decodes one ProvenanceV0: the first 14 fields, with an
// AIVendorV0 and a TokenUsageV0. Fields 15 to 21 stay nil
// (docs/remote/change-format/sections.md#provenancev0).
func decodeRecordV0(d *postcard.Decoder) Record {
	r := decodeHead(d, decodeVendorV0)
	decodeBody(d, &r, decodeTokensV0)
	return r
}

// decodeTokensV0 decodes a TokenUsageV0, the first five counters of
// TokenUsage. Reasoning is 0, as Atomic converts it
// (docs/remote/change-format/sections.md#tokenusagev0).
func decodeTokensV0(d *postcard.Decoder) TokenUsage {
	return TokenUsage{
		Input:      d.U64(),
		Output:     d.U64(),
		Total:      d.U64(),
		CacheRead:  d.U64(),
		CacheWrite: d.U64(),
	}
}

// decodeRecordJuly2026 decodes one July2026Provenance and converts it to
// the current layout: the model version becomes set, and agent_name, and
// agent_version when it is present, are appended to the metadata unless
// an identical pair is already there
// (docs/remote/change-format/sections.md#july2026provenance).
func decodeRecordJuly2026(d *postcard.Decoder) Record {
	vendor := decodeVendor(d)
	model := d.Str()
	modelVersion := d.Str()
	r := Record{
		Vendor:         vendor,
		Model:          model,
		ModelVersion:   &modelVersion,
		Tool:           decodeTool(d),
		SuggestionType: SuggestionType(d.Variant(suggestionVariants)),
	}
	agentName := d.Str()
	agentVersion := postcard.Option(d, (*postcard.Decoder).Str)
	decodeBody(d, &r, decodeTokens)
	decodeAgentFields(d, &r)

	r.Metadata = preserveMetadata(r.Metadata, Pair{Key: "agent_name", Value: agentName})
	if agentVersion != nil {
		r.Metadata = preserveMetadata(r.Metadata, Pair{Key: "agent_version", Value: *agentVersion})
	}
	return r
}

// preserveMetadata appends p to metadata unless an identical pair is
// already there, as Atomic's preserve_metadata does.
func preserveMetadata(metadata []Pair, p Pair) []Pair {
	for _, m := range metadata {
		if m == p {
			return metadata
		}
	}
	return append(metadata, p)
}
