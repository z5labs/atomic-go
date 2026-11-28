package provenance

import (
	"fmt"
	"time"

	"github.com/z5labs/atomic-go/ident"
)

// Record is one AI contribution to a change: Atomic's Provenance, in the
// current 21-field layout
// (docs/remote/change-format/sections.md#provenance). The fields are in
// wire order. An optional field is nil when the record holds None.
type Record struct {
	// Vendor is the AI provider.
	Vendor Vendor

	// Model is the model identifier, such as claude-sonnet-4-20250514.
	Model string

	// ModelVersion is the model version or checkpoint.
	ModelVersion *string

	// Tool is how the AI was used.
	Tool Tool

	// SuggestionType is the kind of contribution.
	SuggestionType SuggestionType

	// Prompt is the prompt, its hash, or none.
	Prompt Prompt

	// SystemPromptHash is the hash of a separate system prompt.
	SystemPromptHash *ident.Hash

	// Tokens is the token counts.
	Tokens TokenUsage

	// Cost is the cost of the generation.
	Cost Cost

	// Temperature is the sampling temperature times 1000.
	Temperature *uint32

	// Timestamp is the request time as written: Unix epoch milliseconds,
	// or seconds in historical records. [Record.Time] interprets it.
	Timestamp *int64

	// RequestID is the provider's request ID.
	RequestID *string

	// SessionID is the session or conversation ID.
	SessionID *string

	// Metadata is provider-specific key-value pairs, in the order they
	// were written. Keys may repeat. A record with none holds nil.
	Metadata []Pair

	// AgentMode is the agent mode, such as build, code or ask.
	AgentMode *string

	// FinishReason is why the model stopped, such as stop, tool-calls or
	// length.
	FinishReason *string

	// StepCount is the number of model invocations in the turn.
	StepCount *uint32

	// SessionSlug is a human-readable session name.
	SessionSlug *string

	// ReasoningSignature is the provider's signature on the last
	// reasoning block.
	ReasoningSignature *string

	// ReasoningText is the reasoning blocks joined by "\n---\n".
	ReasoningText *string

	// TaskPlan is the agent's task list as a JSON string. It is kept as
	// text and not parsed.
	TaskPlan *string
}

// secondsBound is the magnitude below which Atomic reads a timestamp as
// seconds rather than milliseconds.
const secondsBound = 100_000_000_000

// Time returns the instant r.Timestamp names, in UTC, and false when the
// record has no timestamp. As Atomic does, it reads a value whose
// magnitude is below 100,000,000,000 as Unix seconds and any other value as
// Unix milliseconds, since historical records hold seconds.
func (r Record) Time() (time.Time, bool) {
	if r.Timestamp == nil {
		return time.Time{}, false
	}
	ts := *r.Timestamp
	if ts > -secondsBound && ts < secondsBound {
		return time.Unix(ts, 0).UTC(), true
	}
	return time.UnixMilli(ts).UTC(), true
}

// Pair is one metadata key-value pair, a Rust (String, String).
type Pair struct {
	Key   string
	Value string
}

// VendorKind is an AI provider. The values are the variant indices of the
// current AIVendor (docs/remote/change-format/sections.md#aivendor).
type VendorKind uint8

const (
	VendorAnthropic VendorKind = iota
	VendorOpenAI
	VendorGoogle
	VendorMeta
	VendorMistral
	VendorCohere
	VendorAmazonBedrock
	VendorAzureOpenAI
	VendorXAI
	VendorLocal

	// VendorOther is a provider Atomic does not name; [Vendor.Name]
	// holds its name.
	VendorOther
)

// vendorVariants is the number of variants the current AIVendor declares.
const vendorVariants = 11

// String returns the kind's name as Atomic spells it, or VendorKind(n) for
// a value with no name.
func (k VendorKind) String() string {
	switch k {
	case VendorAnthropic:
		return "Anthropic"
	case VendorOpenAI:
		return "OpenAI"
	case VendorGoogle:
		return "Google"
	case VendorMeta:
		return "Meta"
	case VendorMistral:
		return "Mistral"
	case VendorCohere:
		return "Cohere"
	case VendorAmazonBedrock:
		return "AmazonBedrock"
	case VendorAzureOpenAI:
		return "AzureOpenAI"
	case VendorXAI:
		return "XAI"
	case VendorLocal:
		return "Local"
	case VendorOther:
		return "Other"
	default:
		return fmt.Sprintf("VendorKind(%d)", uint8(k))
	}
}

// Vendor is an AI provider (docs/remote/change-format/sections.md#aivendor).
type Vendor struct {
	// Kind is the provider.
	Kind VendorKind

	// Name is the provider's name when Kind is [VendorOther], and empty
	// otherwise.
	Name string
}

// ToolKind is how an AI was used. The values are the variant indices of
// AITool (docs/remote/change-format/sections.md#aitool).
type ToolKind uint8

const (
	ToolAPI ToolKind = iota
	ToolChat
	ToolEditor
	ToolIDEPlugin
	ToolCLI
	ToolCI
	ToolCodeReview
	ToolOther
)

// toolVariants is the number of variants AITool declares.
const toolVariants = 8

// String returns the kind's name as Atomic spells it, or ToolKind(n) for a
// value with no name.
func (k ToolKind) String() string {
	switch k {
	case ToolAPI:
		return "Api"
	case ToolChat:
		return "Chat"
	case ToolEditor:
		return "Editor"
	case ToolIDEPlugin:
		return "IdePlugin"
	case ToolCLI:
		return "Cli"
	case ToolCI:
		return "CI"
	case ToolCodeReview:
		return "CodeReview"
	case ToolOther:
		return "Other"
	default:
		return fmt.Sprintf("ToolKind(%d)", uint8(k))
	}
}

// named reports whether a tool of kind k carries a name. Only [ToolAPI]
// and [ToolChat] do not.
func (k ToolKind) named() bool {
	return k != ToolAPI && k != ToolChat
}

// Tool is how an AI was used (docs/remote/change-format/sections.md#aitool).
type Tool struct {
	// Kind is how the AI was used.
	Kind ToolKind

	// Name names the editor, plugin, CLI tool, CI system, review tool or
	// other tool. It is empty for [ToolAPI] and [ToolChat], which carry no
	// name.
	Name string
}

// SuggestionType is the kind of an AI contribution. The values are the
// variant indices of SuggestionType
// (docs/remote/change-format/sections.md#suggestiontype).
type SuggestionType uint8

const (
	// SuggestionComplete is a change the AI generated.
	SuggestionComplete SuggestionType = iota

	// SuggestionPartial is an AI suggestion a human modified.
	SuggestionPartial

	// SuggestionCollaborative is a change a human started and the AI
	// completed. It is Atomic's default.
	SuggestionCollaborative

	// SuggestionSelection is a human's choice among AI options.
	SuggestionSelection

	// SuggestionReview is an AI review of human code.
	SuggestionReview

	// SuggestionDocumentation is AI-written documentation.
	SuggestionDocumentation

	// SuggestionDebugging is AI help with debugging.
	SuggestionDebugging

	// SuggestionRefactoring is an AI refactoring.
	SuggestionRefactoring

	// SuggestionTesting is AI-generated tests.
	SuggestionTesting
)

// suggestionVariants is the number of variants SuggestionType declares.
const suggestionVariants = 9

// String returns the type's name as Atomic spells it, or SuggestionType(n)
// for a value with no name.
func (s SuggestionType) String() string {
	switch s {
	case SuggestionComplete:
		return "Complete"
	case SuggestionPartial:
		return "Partial"
	case SuggestionCollaborative:
		return "Collaborative"
	case SuggestionSelection:
		return "Selection"
	case SuggestionReview:
		return "Review"
	case SuggestionDocumentation:
		return "Documentation"
	case SuggestionDebugging:
		return "Debugging"
	case SuggestionRefactoring:
		return "Refactoring"
	case SuggestionTesting:
		return "Testing"
	default:
		return fmt.Sprintf("SuggestionType(%d)", uint8(s))
	}
}

// PromptKind says what a [Prompt] keeps of the prompt. The values are the
// variant indices of PromptContent
// (docs/remote/change-format/sections.md#promptcontent).
type PromptKind uint8

const (
	// PromptHashed keeps only the prompt's hash, in [Prompt.Hash].
	PromptHashed PromptKind = iota

	// PromptFull keeps the prompt's text, in [Prompt.Text].
	PromptFull

	// PromptCompressed keeps a compressed prompt, in [Prompt.Compressed].
	PromptCompressed

	// PromptNone keeps nothing. It is Atomic's default.
	PromptNone
)

// promptVariants is the number of variants PromptContent declares.
const promptVariants = 4

// String returns the kind's name as Atomic spells it, or PromptKind(n) for
// a value with no name.
func (k PromptKind) String() string {
	switch k {
	case PromptHashed:
		return "Hashed"
	case PromptFull:
		return "Full"
	case PromptCompressed:
		return "Compressed"
	case PromptNone:
		return "None"
	default:
		return fmt.Sprintf("PromptKind(%d)", uint8(k))
	}
}

// Prompt is what a record keeps of its prompt
// (docs/remote/change-format/sections.md#promptcontent). Only the field
// Kind names is meaningful; the others are zero.
//
// The zero Prompt is [PromptHashed] with the zero hash, because that is
// variant 0. A record with no prompt information sets Kind to
// [PromptNone].
type Prompt struct {
	// Kind says which of the fields below holds the prompt.
	Kind PromptKind

	// Hash is the prompt's hash, for [PromptHashed].
	Hash ident.Hash

	// Text is the prompt's text, for [PromptFull].
	Text string

	// Compressed is the compressed prompt, for [PromptCompressed]. Its
	// compression is not specified, so the bytes are kept opaque and are
	// never decompressed.
	Compressed []byte
}

// TokenUsage is a record's token counts
// (docs/remote/change-format/sections.md#tokenusage).
type TokenUsage struct {
	Input  uint64
	Output uint64

	// Total is the total as recorded. Atomic does not derive it on read,
	// so it need not equal the sum of the other counters.
	Total uint64

	CacheRead  uint64
	CacheWrite uint64
	Reasoning  uint64
}

// Cost is the cost of a generation
// (docs/remote/change-format/sections.md#cost). Both fields are kept as
// written; neither is derived from the other.
type Cost struct {
	// USD is the cost in US dollars. Its IEEE 754 bits are preserved
	// exactly, including negative zero and NaN payloads.
	USD float64

	// MicroUSD is the cost in millionths of a dollar. Atomic's equality
	// on Cost compares only this field.
	MicroUSD uint64
}
