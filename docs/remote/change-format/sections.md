# Section payloads

This page specifies the decompressed payload of every V3 change-file section except GRAPH, SEMANTIC and CONTENT: HEADER, DEPS, PROVENANCE, SIGNATURE, SPAN and UNHASHED. It is written for a client that reads or writes change files byte for byte. The file header, section framing, zstd compression, section order, hash dedup table, trailer and change hash are on [container.md](container.md). The GRAPH and SEMANTIC payloads are on [graph.md](graph.md). Every citation is a permalink pinned to commit [`a6c3e359456e83cf5a967a2e8265f11e0ba45d80`](https://github.com/atomicdotdev/atomic/commit/a6c3e359456e83cf5a967a2e8265f11e0ba45d80).

A payload, on this page, is the byte string a reader holds after it strips a section's header and zstd-decompresses the `compressed_len` bytes that follow it. A section whose `compressed_len` is 0 has an empty payload, and zstd is not run on it ([`sections.rs#L121-L136`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/sections.rs#L121-L136)).

## Sections on this page

The type bytes come from `SectionType::from_byte` ([`section.rs#L142-L155`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L142-L155)). Whether a section is in the change hash comes from `SectionType::is_hashed` ([`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195)); see [Change hash coverage](#change-hash-coverage).

| Section | Type byte | Payload | Rust value | Hashed | `Change::serialize` writes it |
|---|---|---|---|---|---|
| [HEADER](#header-section) | `0x01` | postcard | [ChangeHeaderV2](#changeheaderv2) in schema versions 2 and 3; [ChangeHeader](#changeheader) in schema version 1 | Yes | Always ([`change.rs#L468-L470`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L468-L470)) |
| [DEPS](#deps-section) | `0x02` | postcard | `Vec<u16>` | Yes | Always, also when there are no dependencies ([`change.rs#L472-L479`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L472-L479)) |
| [PROVENANCE](#provenance-section) | `0x03` | postcard | `Vec<`[Provenance](#provenance)`>` | Yes | Only when the change has at least one provenance entry ([`change.rs#L414`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L414), [`change.rs#L481-L484`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L481-L484)) |
| [SIGNATURE](#signature-section) | `0x04` | postcard | [ChangeSignature](#changesignature) | No | Only when the change is signed ([`change.rs#L486-L490`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L486-L490)) |
| [SPAN](#span-section) | `0x11` | `ATSP` then postcard | [SpanPatch](#spanpatch) | Yes | Only when the change carries a span patch, and then the file is schema version 3 ([`change.rs#L457-L459`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L457-L459), [`change.rs#L514-L519`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L514-L519)) |
| [UNHASHED](#unhashed-section) | `0xF0` | JSON | `serde_json::Value` | No | Only when the change has unhashed metadata ([`change.rs#L536-L540`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L536-L540)) |

The schema version is the `version` field of the file header ([`change.rs#L604-L607`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L604-L607)); see [Versioning](container.md#versioning). The file-header flags that announce PROVENANCE, SIGNATURE and UNHASHED are on [FileHeaderFlags](container.md#fileheaderflags), and the order the sections must appear in is on [Section order and multiplicity](container.md#section-order-and-multiplicity).

## Encoding rules

**Postcard.** HEADER, DEPS, PROVENANCE, SIGNATURE and the body of SPAN are postcard. The writer serializes the value with `postcard::to_allocvec` and then compresses it ([`helpers.rs#L93-L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/helpers.rs#L93-L103)). Postcard writes no field names, tags or struct lengths: a struct is its fields in declaration order, and the field order in each table below is the byte layout. Atomic's own compatibility code says so: "Postcard serializes structs positionally" ([`compatibility.rs#L3-L5`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L3-L5)). The encodings of the primitives (unsigned varints, zigzag `i64`, strings, `Option`, `Vec`, tuples, fixed-size arrays, `f64`, enum variant indices) are on [Postcard encoding](container.md#postcard-encoding); this page uses them without repeating them.

**Enums.** Postcard writes an enum as its variant index followed by the variant's fields. The tables below give each variant's index, counted from 0 in declaration order. Inserting a variant renumbers every later one: the xAI vendor sits before `Local` in the current enum ([`types.rs#L38-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L38-L41)), and the full reader keeps the numbering from before it was added as [AIVendorV0](#aivendorv0) so that older provenance still decodes ([`mod.rs#L199-L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L199-L228)). Older records decode, but not always to the vendor that was written: index 8 means `Local` in the old numbering and `XAI` in the new, and Atomic reads it as `XAI` ([AIVendorV0](#aivendorv0)).

**Serde attributes.** Only three kinds of serde attribute appear on the types on this page:

| Attribute | Where | Effect on the bytes |
|---|---|---|
| `#[serde(default)]` | Fields of [ChangeHeader](#changeheader), [Author](#author), [Provenance](#provenance), [TokenUsage](#tokenusage), [Cost](#cost) | None. Postcard is positional, so every field is always written and always read; `default` cannot stand in for a missing or moved field ([`compatibility.rs#L3-L5`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L3-L5)). |
| `#[serde(rename_all = ...)]` | [AIVendor](#aivendor), [AITool](#aitool), [SuggestionType](#suggestiontype), [PromptContent](#promptcontent) ([`types.rs#L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L20), [`types.rs#L103`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L103), [`types.rs#L175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L175), [`types.rs#L443`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L443)) | None. It renames variants, and postcard writes the variant index, not the name. The [PROVENANCE example](#provenance-example) shows `Local` written as the index `09`. |
| `#[serde(with = "serde_bytes_array_64")]`, `#[serde(with = "serde_bytes_array_32")]` | [ChangeSignature](#changesignature) `signature` and `signed_hash` ([`section.rs#L515-L520`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L515-L520)) | The fixed-size array is written as a byte slice, that is a varint length (`0x40` or `0x20`) followed by the bytes, instead of the bare bytes a fixed-size array gets ([`section.rs#L558-L586`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L558-L586)). |

No `skip`, `skip_serializing_if`, `flatten`, `tag`, `untagged` or `transparent` attribute appears on these types.

**Hashes.** A field of type `Hash` (`Merkle`) is 32 raw bytes with no length prefix; see [Binary hash form](../types.md#binary-hash-form). Its text form, used by JSON and by humans, is on [Base32 hash text](../types.md#base32-hash-text).

<a id="hash-indices"></a>**Hash indices.** HEADER and DEPS refer to other changes by `HashIndex`, a `u16` index into the file's hash dedup table, written as a varint ([`hash_index.rs#L14-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/hash_index.rs#L14-L18), [`hash_index.rs#L33-L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/hash_index.rs#L33-L39)). What each index value means, and how the code differs from the constants' doc comments, is on [HashIndex](container.md#hashindex). In these two sections every index must name a real, non-zero hash: when the reader turns an index into a hash, it refuses `0xFFFF` and any index past the end of the table with `HashIndexOutOfBounds`, and any index whose table entry is the all-zero hash with `InvalidChangeMetadata` ([`envelope.rs#L110-L123`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L110-L123), [`hash_table.rs#L409-L417`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/hash_table.rs#L409-L417)). `Change::serialize` fills table entry 0 with zeros ([`change.rs#L391-L394`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L391-L394)), so in the files it writes index 0 is never a valid reference from these sections. The table itself is on [HashDedupTable](container.md#hashdeduptable).

**Trailing bytes.** `postcard::from_bytes` ignores bytes left over after the value ([Postcard encoding](container.md#postcard-encoding)). Atomic decodes HEADER, DEPS, PROVENANCE and SIGNATURE with it ([`envelope.rs#L65`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L65), [`mod.rs#L162-L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/mod.rs#L162-L164), [`mod.rs#L345-L347`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L345-L347), [`change.rs#L744-L749`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L744-L749)), so no Atomic reader checks that those payloads are fully consumed. Only SPAN is checked for trailing bytes ([`span_patch.rs#L163-L180`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L163-L180)).

**Required column.** Every field is always present in the bytes, because postcard is positional. In the tables below, **Required** means the value must be meaningful (a reader rejects the change otherwise, or the field has no empty form) and **Optional** means `None`, an empty list or an empty string is valid.

## HEADER section

In schema versions 2 and 3 the HEADER payload is the postcard encoding of [ChangeHeaderV2](#changeheaderv2): a [ChangeHeader](#changeheader) followed by the change's lifecycle and origin facts and its opaque hashed metadata. `Change::serialize` builds it from the change's hashed fields ([`envelope.rs#L26-L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L26-L44), [`change.rs#L466-L470`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L466-L470)). The low-level `ChangeWriter::write_change_header` writes a `Durable`, `Native` envelope with no supersedes, empty lists and empty metadata ([`state_machine.rs#L303-L306`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L303-L306), [`envelope.rs#L47-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L47-L58)). Schema version 3 uses the same envelope: the reader decodes `ChangeHeaderV2` for both versions ([`change.rs#L607`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L607), [`change.rs#L643-L654`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L643-L654)).

In schema version 1 the payload is the postcard encoding of [ChangeHeader](#changeheader) alone ([`change.rs#L655-L660`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L655-L660)). The reader then supplies `Durable`, no supersedes, `Native`, an empty causal frontier, no extra-known changes and empty metadata ([`change.rs#L786-L798`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L786-L798)). Writers refuse to emit version 1 ([`state_machine.rs#L232-L239`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L232-L239)), and re-serializing a version-1 change gives it a different hash ([`legacy_history_span_insert_tests.rs#L23-L29`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/tests/legacy_history_span_insert_tests.rs#L23-L29)), so a client must keep a version-1 file's original bytes.

`ChangeHeaderV2` starts with a `ChangeHeader`, and postcard ignores trailing bytes, so decoding a `ChangeHeader` from the start of a HEADER payload gives the human-readable header in every schema version. Atomic's change store does exactly that for every version ([`mod.rs#L409-L416`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/redb_change_store/mod.rs#L409-L416)).

### ChangeHeaderV2

Defined at [`envelope.rs#L136-L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L136-L146), with the comment "The field order and enum discriminants below are frozen for schema version 2".

| # | Field | Rust type | Wire encoding | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `header` | [ChangeHeader](#changeheader) | the struct's fields, inline | **Required** | Message, description, timestamp and authors. |
| 2 | `kind` | [WireChangeKindV2](#wirechangekindv2) | variant index, then fields | **Required** | `Durable` history or a working-copy `Snapshot` ([`classification.rs#L9-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L9-L20)). |
| 3 | `supersedes` | `Option<HashIndex>` | `00`, or `01` then a varint index | **Optional** | The previous snapshot this snapshot replaces ([`change.rs#L1084-L1085`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1084-L1085)). Only a `Snapshot` may set it, and it must not also be a dependency ([`change.rs#L1177-L1191`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1177-L1191)). |
| 4 | `origin` | [WireChangeOriginV2](#wirechangeoriginv2) | variant index, then fields | **Required** | `Native`, or the Git commit the change was synthesized from. |
| 5 | `causal_frontier` | `Vec<HashIndex>` | varint count, then varint indices | **Optional** | Roots whose closures this change knows. Must be non-empty exactly when `origin` is `GitResolution` ([`change.rs#L1198-L1204`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1198-L1204)). The hashes must be strictly ascending and non-zero ([`envelope.rs#L72-L77`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L72-L77), [`classification.rs#L177-L180`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L177-L180), [`classification.rs#L298-L311`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L298-L311)). |
| 6 | `extra_known` | `Vec<HashIndex>` | varint count, then varint indices | **Optional** | Changes known when this change was made but not required by it ([`change.rs#L1098-L1102`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1098-L1102)). The hashes must be strictly ascending and non-zero ([`envelope.rs#L78-L79`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L78-L79)) and must not repeat a dependency ([`change.rs#L1157-L1163`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1157-L1163)). |
| 7 | `metadata` | `Vec<u8>` | varint length, then the bytes | **Optional** | Opaque application bytes that are part of the change hash ([`change.rs#L1104-L1107`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1104-L1107)). They are never read as a span patch, even when they start with `ATSP` ([`change.rs#L1109-L1111`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1109-L1111), [`span_patch.rs#L295-L321`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L295-L321)). |

The indices in `supersedes`, `causal_frontier` and `extra_known` are resolved as described under [Hash indices](#hash-indices). `Change::serialize` adds the dependencies, then `extra_known`, then `supersedes`, then the causal-frontier roots to the hash table before the hunk hashes ([`change.rs#L396-L411`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L396-L411)).

<a id="header-example"></a>**Example.** The test `opaque_schema_two_matches_existing_wire_bytes` builds a schema-2 change with the message `opaque v2 fixture`, timestamp 1,700,000,000 and metadata `ATSP\x01\x00\x00`, and asserts that its bytes equal the fixture ([`span_patch.rs#L443-L469`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L443-L469); [`span_opaque_v2_reference.hex#L1-L4`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/span_opaque_v2_reference.hex#L1-L4)). Decompressing the fixture's HEADER section gives these 54 bytes:

```text
11                                  header.message: length 17
6f 70 61 71 75 65 20 76 32 20       "opaque v2 "
66 69 78 74 75 72 65                "fixture"
00                                  header.description: None
14                                  header.timestamp: length 20
32 30 32 33 2d 31 31 2d 31 34 54    "2023-11-14T"
32 32 3a 31 33 3a 32 30 5a          "22:13:20Z"
00                                  header.authors: 0 entries
00                                  kind: Durable (variant 0)
00                                  supersedes: None
00                                  origin: Native (variant 0)
00                                  causal_frontier: 0 entries
00                                  extra_known: 0 entries
07 41 54 53 50 01 00 00             metadata: 7 bytes "ATSP" 01 00 00
```

**Reader checks.** In schema versions 2 and 3, `Change::deserialize` decodes the envelope ([`envelope.rs#L61-L90`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L61-L90)), refuses a second HEADER ([`change.rs#L644-L650`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L644-L650)) and then validates the decoded change ([`change.rs#L838-L840`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L838-L840)). A change is refused when:

- `supersedes` is set on a change that is not a `Snapshot`, or names one of the change's dependencies ([`change.rs#L1177-L1191`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1177-L1191)).
- A `Snapshot` has an origin other than `Native`, or a Git origin has a kind other than `Durable` ([`change.rs#L1192-L1197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1192-L1197)).
- `causal_frontier` is empty for a `GitResolution` or non-empty for any other origin ([`change.rs#L1198-L1204`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1198-L1204)).
- A Git origin mixes SHA-1 and SHA-256 object IDs, has the wrong number of parents for its derivation, or is a `GitResolution` with fewer than two parents ([`classification.rs#L125-L166`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L125-L166)).
- A SHA-256 Git parent's 32 bytes equal one of the change's dependency hashes ([`change.rs#L1164-L1175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1164-L1175)).

A writer runs the same validation before it serializes ([`change.rs#L389`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L389)).

### ChangeHeader

Defined at [`header.rs#L196-L211`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L196-L211). It is the whole HEADER payload in schema version 1 and the first field of [ChangeHeaderV2](#changeheaderv2) otherwise.

| # | Field | Rust type | Wire encoding | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `message` | `String` | varint length, then UTF-8 | **Required** | Short summary ([`header.rs#L198-L199`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L198-L199)). The format accepts an empty string; only the builder's `try_build` refuses one ([`header.rs#L389-L392`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L389-L392)). |
| 2 | `description` | `Option<String>` | `00`, or `01` then a string | **Optional** | Longer body ([`header.rs#L201-L203`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L201-L203)). |
| 3 | `timestamp` | `chrono::DateTime<Utc>` | a string: RFC 3339 text, see below | **Required** | When the change was created ([`header.rs#L205-L206`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L205-L206)). |
| 4 | `authors` | `Vec<`[Author](#author)`>` | varint count, then each author | **Optional** | May be empty ([`header.rs#L208-L210`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L208-L210)). |

**The timestamp is text.** chrono serializes a `DateTime` as a string in every serde format, including postcard. This is external behaviour of chrono 0.4.45 ([`Cargo.lock#L774-L776`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L774-L776); [chrono `datetime/serde.rs#L32-L51`](https://github.com/chronotope/chrono/blob/170338250e836976a211e64728ec956e45e78a39/src/datetime/serde.rs#L32-L51), at the commit chrono 0.4.45 was published from). The text is `YYYY-MM-DDTHH:MM:SS`, then a fraction of a second, then `Z`. The fraction is left out when the nanoseconds are 0; otherwise it has 3 digits when the value is a whole number of milliseconds, 6 when it is a whole number of microseconds, and 9 in all other cases (external: [chrono `format/formatting.rs#L544-L553`](https://github.com/chronotope/chrono/blob/170338250e836976a211e64728ec956e45e78a39/src/format/formatting.rs#L544-L553)). The fixtures show both forms: `2023-11-14T22:13:20Z` above and `2026-11-12T16:23:20.451350Z` in the [ChangeHeader example](#changeheader-example). The string is part of the hashed HEADER payload, so a writer must produce it exactly. Go's `time.RFC3339Nano` layout drops trailing zeros ([Go `time` constants](https://pkg.go.dev/time#pkg-constants)) and would write `.45135` for the second value, which changes the change hash.

<a id="changeheader-example"></a>**Example.** The fixture `atomic_0_19_1_file_add.hex` is a schema-version-1 change recorded by atomic 0.19.1 ([`legacy_history_span_insert_tests.rs#L5-L10`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/tests/legacy_history_span_insert_tests.rs#L5-L10)), so its HEADER payload is a bare `ChangeHeader`. Decompressing that section of [`atomic_0_19_1_file_add.hex#L4-L8`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L4-L8) gives 153 bytes:

```text
28 "legacy v1 file recorded by atomic 0.19.1"        message (40 bytes)
00                                                   description: None
1b "2026-11-12T16:23:20.451350Z"                     timestamp (27 bytes)
01                                                   authors: 1 entry
07 "fixture"                                           name
01 12 "fixture@atomic.dev"                             email: Some, 18 bytes
01 34 "TKX4DF2DBALZPZRTUORRAZ74CX5BTFCLMBIRPF27PWYD3TDFUNYQ"
                                                       identity: Some, 52 bytes
```

### Author

Defined at [`header.rs#L42-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L42-L57).

| # | Field | Rust type | Wire encoding | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `name` | `String` | varint length, then UTF-8 | **Required** | Display name ([`header.rs#L44-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L44-L45)). Nothing in the format refuses an empty name. |
| 2 | `email` | `Option<String>` | `00`, or `01` then a string | **Optional** | Email address ([`header.rs#L47-L49`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L47-L49)). |
| 3 | `identity` | `Option<String>` | `00`, or `01` then a string | **Optional** | A reference to the author's cryptographic identity, for example a base32 public key ([`header.rs#L37-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L37-L41), [`header.rs#L51-L56`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/header.rs#L51-L56)). The format does not interpret it; identities are on [auth.md](../auth.md#identities-and-dids). |

### WireChangeKindV2

Defined at [`envelope.rs#L148-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L148-L152).

| Index | Variant | Fields after the index | Meaning |
|---|---|---|---|
| 0 | `Durable` | none | Durable repository history ([`classification.rs#L12-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L12-L14)). |
| 1 | `Snapshot` | `working_copy`: `[u8; 16]`, 16 bare bytes | A complete working-copy state ([`classification.rs#L15-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L15-L19)). The 16 bytes are the working copy's ULID ([`working_copy_id.rs#L8-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/working_copy_id.rs#L8-L14), [`envelope.rs#L155-L161`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L155-L161)). |

### WireChangeOriginV2

Defined at [`envelope.rs#L174-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L174-L186).

| Index | Variant | Fields after the index, in order | Meaning |
|---|---|---|---|
| 0 | `Native` | none | Authored natively by Atomic ([`classification.rs#L59-L61`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L59-L61)). |
| 1 | `GitSynthesized` | `commit`: [WireGitObjectIdV2](#wiregitobjectidv2); `parents`: `Vec<`[WireGitObjectIdV2](#wiregitobjectidv2)`>`; `derivation`: [WireGitDerivationV2](#wiregitderivationv2) | Synthesized from one Git commit. `parents` is the commit's full parent list in Git's order ([`classification.rs#L62-L70`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L62-L70)). |
| 2 | `GitResolution` | `merge_commit`: [WireGitObjectIdV2](#wiregitobjectidv2); `parents`: `Vec<`[WireGitObjectIdV2](#wiregitobjectidv2)`>` | Synthesized resolution of a Git merge commit, with the full parent list in Git's order ([`classification.rs#L71-L77`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L71-L77)). |

Parent order is part of the hash: a test reverses two parents and gets a different change hash ([`change.rs#L1661-L1666`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1661-L1666)).

### WireGitObjectIdV2

Defined at [`envelope.rs#L248-L252`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L248-L252).

| Index | Variant | Fields after the index |
|---|---|---|
| 0 | `Sha1` | `[u8; 20]`, 20 bare bytes |
| 1 | `Sha256` | `[u8; 32]`, 32 bare bytes |

The writer refuses an object ID whose length does not match its algorithm ([`envelope.rs#L255-L276`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L255-L276)).

### WireGitDerivationV2

Defined at [`envelope.rs#L289-L297`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/envelope.rs#L289-L297). Each variant is a bare index. The parent counts are checked by `ChangeOrigin::validate` ([`classification.rs#L134-L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L134-L142)).

| Index | Variant | Parents allowed |
|---|---|---|
| 0 | `Root` | exactly 0 |
| 1 | `FirstParent` | exactly 1 |
| 2 | `MultiParent` | 2 or more |
| 3 | `Squash` | any number |
| 4 | `EmptyCommit` | any number |
| 5 | `RewriteCandidate` | any number |

## DEPS section

The DEPS payload is the postcard encoding of a `Vec<u16>`: a varint count, then one varint [hash index](#hash-indices) per direct dependency ([`state_machine.rs#L358`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L358), [`state_machine.rs#L395`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L395)). A dependency is a change that must be applied before this one ([`change.rs#L1093-L1096`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1093-L1096)). The section is required even when the list is empty ([`state_machine.rs#L342-L344`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L342-L344)): in schema versions 2 and 3 the reader refuses a file without exactly one DEPS section ([`change.rs#L769-L770`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L769-L770)).

**Writer rules.** `ChangeWriter::write_dependencies` refuses index 0, index `0xFFFF`, and any index that is not greater than the one before it ([`state_machine.rs#L378-L393`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L378-L393)). In schema versions 2 and 3 the dependency hashes must also be strictly ascending and non-zero ([`change.rs#L1152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1152), [`classification.rs#L298-L311`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/classification.rs#L298-L311)). `Change::serialize` meets both rules: it refuses dependencies that are not already in ascending hash order ([`change.rs#L389`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L389)), then adds them to a new hash table before any other hash, so that they take indices 1 to n ([`change.rs#L393-L399`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L393-L399), [`hash_table.rs#L267-L280`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/hash_table.rs#L267-L280)).

**Reader checks.** `Change::deserialize` turns each index into a hash with the rules under [Hash indices](#hash-indices) and, in schema versions 2 and 3, refuses a second DEPS section ([`change.rs#L662-L680`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L662-L680)). It does not check the order of the indices; it checks the order of the resulting hashes, and that no dependency is also in `extra_known` ([`change.rs#L1152-L1163`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1152-L1163)). Schema-version-1 files skip those hash checks, because the reader validates only versions 2 and 3 ([`change.rs#L838-L840`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L838-L840)).

**Example.** The schema-2 fixture above has no dependencies; its DEPS payload is the single byte `00` ([`span_opaque_v2_reference.hex#L1-L4`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/span_opaque_v2_reference.hex#L1-L4)). The schema-1 fixture `cb_fmt1_legacy_v1_object.hex` has a two-entry hash table, and its DEPS payload decompresses to `01 01`: one dependency, at index 1 ([`cb_fmt1_legacy_v1_object.hex#L1`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/cb_fmt1_legacy_v1_object.hex#L1)). The writer test that passes `[1, 2, 3]` ([`tests.rs#L253-L264`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/tests.rs#L253-L264)) produces the payload `03 01 02 03`.

## PROVENANCE section

The PROVENANCE payload is the postcard encoding of a list of [Provenance](#provenance) records: a varint count, then each record ([`state_machine.rs#L416`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L416), [`state_machine.rs#L430-L432`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L430-L432)). Each record describes one AI contribution to the change ([`mod.rs#L67-L76`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L67-L76)).

`Change::serialize` writes the section only when the list is non-empty ([`change.rs#L481-L484`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L481-L484)). The low-level `ChangeWriter::write_provenance` also accepts an empty list ([`tests.rs#L317-L329`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/tests.rs#L317-L329)), whose payload is `00`. In schema versions 2 and 3 the reader refuses a second PROVENANCE section, and requires that a PROVENANCE section is present exactly when the file header's `HAS_PROVENANCE` flag is set ([`change.rs#L681-L687`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L681-L687), [`change.rs#L758-L781`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L758-L781)).

Writers emit only the current [Provenance](#provenance) layout, because `write_provenance` takes `&[Provenance]` ([`state_machine.rs#L416`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L416)). Readers also accept three older layouts; see [Historical layouts](#historical-layouts).

### Provenance

Defined at [`mod.rs#L76-L197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L76-L197). All 21 fields are always present.

| # | Field | Rust type | Wire encoding | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `vendor` | [AIVendor](#aivendor) | variant index, then fields | **Required** | AI provider ([`mod.rs#L78-L79`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L78-L79)). |
| 2 | `model` | `String` | string | **Required** | Model identifier, for example `claude-sonnet-4-20250514` ([`mod.rs#L81-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L81-L82)). |
| 3 | `model_version` | `Option<String>` | option of string | **Optional** | Model version or checkpoint ([`mod.rs#L84-L86`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L84-L86)). |
| 4 | `tool` | [AITool](#aitool) | variant index, then fields | **Required** | How the AI was used ([`mod.rs#L88-L89`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L88-L89)). |
| 5 | `suggestion_type` | [SuggestionType](#suggestiontype) | variant index | **Required** | Kind of contribution ([`mod.rs#L91-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L91-L92)). |
| 6 | `prompt` | [PromptContent](#promptcontent) | variant index, then fields | **Required** | The prompt, its hash, or `None` ([`mod.rs#L94-L96`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L94-L96)). |
| 7 | `system_prompt_hash` | `Option<Hash>` | `00`, or `01` then 32 bytes | **Optional** | Hash of a separate system prompt ([`mod.rs#L98-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L98-L100)). |
| 8 | `tokens` | [TokenUsage](#tokenusage) | six varints | **Required** | Token counts ([`mod.rs#L102-L104`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L102-L104)). |
| 9 | `cost` | [Cost](#cost) | `f64`, then a varint | **Required** | Cost of the generation ([`mod.rs#L106-L108`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L106-L108)). |
| 10 | `temperature` | `Option<u32>` | option of varint | **Optional** | Sampling temperature times 1000 ([`mod.rs#L110-L113`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L110-L113)). |
| 11 | `timestamp` | `Option<i64>` | option of zigzag varint | **Optional** | Request time in Unix epoch milliseconds; historical records hold seconds ([`mod.rs#L115-L118`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L115-L118)). Atomic reads a value whose magnitude is below 100,000,000,000 as seconds and any other value as milliseconds ([`timestamp.rs#L5-L21`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/timestamp.rs#L5-L21)). |
| 12 | `request_id` | `Option<String>` | option of string | **Optional** | Provider's request ID ([`mod.rs#L120-L122`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L120-L122)). |
| 13 | `session_id` | `Option<String>` | option of string | **Optional** | Session or conversation ID ([`mod.rs#L124-L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L124-L126)). |
| 14 | `metadata` | `Vec<(String, String)>` | varint count, then key and value strings for each pair | **Optional** | Provider-specific key-value pairs, order preserved ([`mod.rs#L128-L130`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L128-L130)). |
| 15 | `agent_mode` | `Option<String>` | option of string | **Optional** | Agent mode such as `build`, `code` or `ask` ([`mod.rs#L135-L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L135-L142)). |
| 16 | `finish_reason` | `Option<String>` | option of string | **Optional** | Why the model stopped, such as `stop`, `tool-calls` or `length` ([`mod.rs#L144-L150`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L144-L150)). |
| 17 | `step_count` | `Option<u32>` | option of varint | **Optional** | Model invocations in the turn ([`mod.rs#L152-L157`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L152-L157)). |
| 18 | `session_slug` | `Option<String>` | option of string | **Optional** | Human-readable session name ([`mod.rs#L159-L164`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L159-L164)). |
| 19 | `reasoning_signature` | `Option<String>` | option of string | **Optional** | The provider's signature on the last reasoning block ([`mod.rs#L166-L175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L166-L175)). |
| 20 | `reasoning_text` | `Option<String>` | option of string | **Optional** | Reasoning blocks joined by `\n---\n`; the comment says it is truncated to 10 KB ([`mod.rs#L177-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L177-L186)). |
| 21 | `task_plan` | `Option<String>` | option of string | **Optional** | The agent's task list as a JSON string ([`mod.rs#L188-L196`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L188-L196)). |

### AIVendor

Defined at [`types.rs#L19-L44`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L19-L44).

| Index | Variant | Fields after the index |
|---|---|---|
| 0 | `Anthropic` | none |
| 1 | `OpenAI` | none |
| 2 | `Google` | none |
| 3 | `Meta` | none |
| 4 | `Mistral` | none |
| 5 | `Cohere` | none |
| 6 | `AmazonBedrock` | none |
| 7 | `AzureOpenAI` | none |
| 8 | `XAI` | none |
| 9 | `Local` | none |
| 10 | `Other` | `String`: the provider's name |

### AITool

Defined at [`types.rs#L102-L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L102-L121).

| Index | Variant | Fields after the index |
|---|---|---|
| 0 | `Api` | none |
| 1 | `Chat` | none |
| 2 | `Editor` | `String`: editor name |
| 3 | `IdePlugin` | `String`: plugin name |
| 4 | `Cli` | `String`: tool name |
| 5 | `CI` | `String`: CI system name |
| 6 | `CodeReview` | `String`: review tool name |
| 7 | `Other` | `String`: tool name |

### SuggestionType

Defined at [`types.rs#L174-L196`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L174-L196). Each variant is a bare index.

| Index | Variant | Meaning |
|---|---|---|
| 0 | `Complete` | AI generated the change |
| 1 | `Partial` | AI suggested, a human modified |
| 2 | `Collaborative` | A human started, AI completed; the default |
| 3 | `Selection` | A human chose among AI options |
| 4 | `Review` | AI reviewed human code |
| 5 | `Documentation` | AI documented code |
| 6 | `Debugging` | AI helped debug |
| 7 | `Refactoring` | AI refactored code |
| 8 | `Testing` | AI generated tests |

### PromptContent

Defined at [`types.rs#L442-L454`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L442-L454).

| Index | Variant | Fields after the index | Meaning |
|---|---|---|---|
| 0 | `Hashed` | `Hash`: 32 bytes | Only the prompt's hash is kept. `PromptContent::hash_from` uses `Hash::of` over the prompt's UTF-8 bytes ([`types.rs#L457-L460`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L457-L460)). |
| 1 | `Full` | `String` | The full prompt text. |
| 2 | `Compressed` | `Vec<u8>`: varint length, then the bytes | A compressed prompt; see [Open questions](#open-questions). |
| 3 | `None` | none | No prompt information; the default. |

### TokenUsage

Defined at [`types.rs#L246-L269`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L246-L269). Six `u64` varints, in this order:

| # | Field | Meaning |
|---|---|---|
| 1 | `input_tokens` | Prompt tokens |
| 2 | `output_tokens` | Completion tokens |
| 3 | `total_tokens` | Total as recorded. `TokenUsage::new` stores input plus output ([`types.rs#L272-L282`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L272-L282)); `TokenUsage::full` adds reasoning tokens too ([`types.rs#L296-L312`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L296-L312)). |
| 4 | `cache_read_tokens` | Cache reads |
| 5 | `cache_write_tokens` | Cache writes |
| 6 | `reasoning_tokens` | Reasoning or thinking tokens |

### Cost

Defined at [`types.rs#L364-L372`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L364-L372).

| # | Field | Rust type | Wire encoding | Meaning |
|---|---|---|---|---|
| 1 | `usd` | `f64` | 8 bytes, see [Postcard encoding](container.md#postcard-encoding) | Cost in US dollars. |
| 2 | `micro_usd` | `u64` | varint | Cost in millionths of a dollar: `Cost::from_usd` stores `usd * 1_000_000` ([`types.rs#L375-L381`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L375-L381)). Equality compares only this field ([`types.rs#L410-L414`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L410-L414)). |

### Historical layouts

Payloads written before the current layout have different field lists or variant numbering, and postcard has no field names to tell them apart ([`compatibility.rs#L1-L5`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L1-L5)). Atomic's two decoders fall back to different sets of them:

| Decoder | Tries, in order | Source |
|---|---|---|
| `Change::deserialize` (the full change reader) | [Provenance](#provenance), then [ProvenanceBeforeXai](#provenancebeforexai), then [ProvenanceV0](#provenancev0) | [`change.rs#L688`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L688), [`mod.rs#L345-L357`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L345-L357) |
| `ReadSection::deserialize` (the section-level helper) | [Provenance](#provenance), then [July2026Provenance](#july2026provenance) | [`mod.rs#L176-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/mod.rs#L176-L186) |

Each decoder returns the first layout that decodes, and reports the error from the current layout when none does ([`mod.rs#L345-L357`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L345-L357), [`mod.rs#L176-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/mod.rs#L176-L186)). The first layout that decodes is not always the one that was written, because postcard writes no type tags or field names. Two cases follow from Atomic's own bytes: the full reader reads a pre-xAI `Local` vendor as `XAI` ([AIVendorV0](#aivendorv0)), and it misreads the July 2026 fixture in the `ProvenanceBeforeXai` layout ([July 2026 payloads in the full reader](#july-2026-misread)). A Go reader that copies these fallback orders gets the same values.

### ProvenanceBeforeXai

Defined at [`mod.rs#L230-L253`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L230-L253). The same 21 fields as [Provenance](#provenance), except that `vendor` is an [AIVendorV0](#aivendorv0). A test decodes one through the fallback ([`tests.rs#L436-L469`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/tests.rs#L436-L469)).

### AIVendorV0

Defined at [`mod.rs#L199-L211`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L199-L211). [AIVendor](#aivendor) without `XAI`: indices 0 to 7 are the same, `Local` is 8 and `Other(String)` is 9. It converts to the current variant of the same name ([`mod.rs#L213-L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L213-L228)).

**Index 8 is ambiguous, and Atomic reads it as `XAI`.** Index 8 is `Local` here and `XAI` in the current [AIVendor](#aivendor) ([`types.rs#L37-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L37-L41)). Both are unit variants, so the bytes cannot tell the two numberings apart. Apart from `vendor`, [ProvenanceBeforeXai](#provenancebeforexai) has exactly the fields of [Provenance](#provenance) ([`mod.rs#L230-L253`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L230-L253), [`mod.rs#L76-L197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L76-L197)). A record written in that layout with vendor `Local` therefore decodes cleanly in the current layout, as `XAI`, and `deserialize_postcard` returns it without trying a fallback ([`mod.rs#L345-L347`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L345-L347)). The fallback runs only when the current layout fails. It does for the record in Atomic's test, whose vendor is `Other("openrouter")`: read in the current layout, index 9 becomes `Local`, the vendor's name becomes the model, and the model's length byte `12` is not a valid `model_version` option tag ([`tests.rs#L436-L469`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/tests.rs#L436-L469)). See [Open questions](#open-questions).

### ProvenanceV0

Defined at [`mod.rs#L255-L271`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L255-L271). The first 14 fields of [Provenance](#provenance), `vendor` through `metadata`, with `vendor` an [AIVendorV0](#aivendorv0) and `tokens` a [TokenUsageV0](#tokenusagev0). Fields 15 to 21 do not exist; conversion sets them to `None` and `reasoning_tokens` to 0 ([`mod.rs#L310-L343`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L310-L343)). A test decodes one through the fallback ([`tests.rs#L399-L434`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/tests.rs#L399-L434)).

### TokenUsageV0

Defined at [`mod.rs#L273-L280`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L273-L280). The first five fields of [TokenUsage](#tokenusage), without `reasoning_tokens`.

### July2026Provenance

Defined at [`compatibility.rs#L20-L45`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L20-L45). It differs from [Provenance](#provenance) in two places: `model_version` is a plain `String`, not an option, and two fields, `agent_name: String` and `agent_version: Option<String>`, sit between `suggestion_type` and `prompt` ([`compatibility.rs#L13-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L13-L19)). It uses the current [AIVendor](#aivendor), [TokenUsage](#tokenusage) and [Cost](#cost). On conversion, `model_version` becomes `Some`, and `agent_name` and `agent_version` are appended to `metadata` under those keys unless an identical pair is already there ([`compatibility.rs#L47-L88`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L47-L88)).

<a id="provenance-example"></a>**Example.** The fixture `july_2026_legacy_provenance.hex` is an uncompressed PROVENANCE payload in this layout ([`july_2026_legacy_provenance.hex#L1-L9`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/july_2026_legacy_provenance.hex#L1-L9)). A test shows that the current layout fails on it and the compatibility layout decodes it to vendor `Local`, model `moonshotai`, model version `kimi-k3`, tool `Api`, suggestion type `Review` and timestamp 1,784,502,467 ([`compatibility.rs#L114-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L114-L138)). Its bytes, field by field:

```text
01                               list: 1 record
09                               vendor: AIVendor index 9 = Local
0a "moonshotai"                  model
07 "kimi-k3"                     model_version (a bare String in this layout)
00                               tool: AITool index 0 = Api
04                               suggestion_type: index 4 = Review
08 "opencode"                    agent_name (only in this layout)
00                               agent_version: None (only in this layout)
03                               prompt: PromptContent index 3 = None
00                               system_prompt_hash: None
00 00 00 00 00 00                tokens: six zero counters
00 00 00 00 00 00 00 00          cost.usd: 0.0
00                               cost.micro_usd: 0
00                               temperature: None
01 86 e3 ea a5 0d                timestamp: Some, zigzag varint of 1784502467 (seconds)
00                               request_id: None
01 1e "ses_08365450affeTbe2zvcG63oKEX"
                                 session_id: Some, 30 bytes
02                               metadata: 2 pairs
0b "turn_number" 01 "1"
0a "agent_name" 08 "opencode"
00 00 00 00 00 00 00             agent_mode .. task_plan: seven Nones
```

In the current layout the same record would carry `01 07 "kimi-k3"` for `model_version` and no `agent_name` or `agent_version` bytes ([`compatibility.rs#L16-L19`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L16-L19)).

<a id="july-2026-misread"></a>**July 2026 payloads in the full reader.** `Change::deserialize` does not refuse this payload; it misreads it. The current layout fails at byte 13, where `model_version`'s option tag should be and `07` is, with `DeserializeBadOption`. The full reader's first fallback, [ProvenanceBeforeXai](#provenancebeforexai), then decodes all 132 bytes with nothing left over ([`change.rs#L688`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L688), [`mod.rs#L345-L357`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L345-L357), [`mod.rs#L230-L253`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L230-L253)). Where the two layouts differ, the same bytes line up like this:

```text
bytes            July 2026 layout                 read as ProvenanceBeforeXai
09               vendor: AIVendor 9 = Local       vendor: AIVendorV0 9 = Other(String)
0a "moonshotai"  model                            the vendor's name
07 "kimi-k3"     model_version                    model
00               tool: AITool 0 = Api             model_version: None
04               suggestion_type: 4 = Review      tool: AITool 4 = Cli(String)
08 "opencode"    agent_name                       the tool's name
00               agent_version: None              suggestion_type: 0 = Complete
```

From `03` (`prompt`) on, both layouts read the same bytes. So `Change::deserialize` returns vendor `Other("moonshotai")`, model `kimi-k3`, no model version, tool `Cli("opencode")` and suggestion type `Complete`, where the July 2026 decoder returns `Local`, `moonshotai`, `kimi-k3`, `Api` and `Review` ([`compatibility.rs#L114-L138`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L114-L138)). Only `ReadSection::deserialize` tries the July 2026 layout ([`mod.rs#L176-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/mod.rs#L176-L186)). The alignment also works the other way: `ReadSection::deserialize` decodes the pre-xAI record from Atomic's test ([`tests.rs#L436-L469`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/tests.rs#L436-L469)) in the July 2026 layout, as vendor `Local`, model `openrouter`, model version `openai/gpt-5.6-sol`, tool `Api` and suggestion type `Review`. Whether some other July 2026 payload fails or is misread depends on its bytes.

## SIGNATURE section

The SIGNATURE payload is the postcard encoding of a [ChangeSignature](#changesignature) ([`state_machine.rs#L459-L485`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L459-L485)). The writer allows at most one, after DEPS and before any GRAPH section ([`state_machine.rs#L463-L480`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L463-L480)). The section is not part of the change hash, so signing or re-signing a change never changes its identity ([`section.rs#L64-L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L64-L74), [`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195)); a test checks that serializing a signed change gives the unsigned hash ([`tests_signing.rs#L27-L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/tests_signing.rs#L27-L35)). Schema-version-1 files can carry it too: the 0.19.1 fixture below is version 1 and has one ([`legacy_history_span_insert_tests.rs#L5-L7`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/tests/legacy_history_span_insert_tests.rs#L5-L7), [`atomic_0_19_1_file_add.hex#L1`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L1)). What is signed, and how a verifier checks it, is on [Signing](container.md#signing).

### ChangeSignature

Defined at [`section.rs#L508-L527`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L508-L527).

| # | Field | Rust type | Wire encoding | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `signer_did` | `String` | string | **Required** | The signer's `did:atomic:` identifier: `did:atomic:` followed by the base32 of BLAKE3 of the signer's 32-byte Ed25519 public key, a fingerprint of the key rather than the key itself. The CLI's record command builds it that way ([`builder.rs#L159-L162`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-cli/src/commands/record/builder.rs#L159-L162), [`did.rs#L27-L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-canonical/src/did.rs#L27-L35), [`identity.rs#L56-L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-identity/src/identity.rs#L56-L59), [`identity.rs#L108-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-identity/src/identity.rs#L108-L110)). The signature cannot be verified from the DID alone. It is a discovery hint, never the trust root: a verifier must resolve the public key elsewhere, and can then check that the key's fingerprint matches the DID ([`section.rs#L500-L502`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L500-L502), [`section.rs#L510-L512`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L510-L512), [`did.rs#L54-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-canonical/src/did.rs#L54-L58)). DIDs are on [DID forms](../auth.md#did-forms). |
| 2 | `signature` | `[u8; 64]` | `40`, then 64 bytes (`serde_bytes_array_64`) | **Required** | Ed25519 signature ([`section.rs#L514-L516`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L514-L516)). The reader refuses any length other than 64 ([`section.rs#L566-L570`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L566-L570)). |
| 3 | `signed_hash` | `[u8; 32]` | `20`, then 32 bytes (`serde_bytes_array_32`) | **Required** | The change hash that was signed ([`section.rs#L518-L520`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L518-L520), [`signing.rs#L48`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/signing.rs#L48)). Verification fails unless it equals the change's verified hash ([`signing.rs#L84-L91`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/signing.rs#L84-L91)). The reader refuses any length other than 32 ([`section.rs#L581-L585`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L581-L585)). |
| 4 | `timestamp` | `i64` | zigzag varint | **Required** | When the signature was made, in Unix epoch seconds ([`section.rs#L522-L523`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L522-L523)). |
| 5 | `method` | `String` | string | **Required** | Signature scheme; `ed25519` is the only value written ([`section.rs#L525-L531`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L525-L531), [`section.rs#L548`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L548)). Verification refuses any other method ([`signing.rs#L78-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/signing.rs#L78-L82)). |

**Example.** Decompressing the SIGNATURE section of the 0.19.1 fixture ([`atomic_0_19_1_file_add.hex#L9-L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L9-L15)) gives 175 bytes:

```text
3f "did:atomic:45LZQUIZNX5VFO7UXZHBOU4LMUB6U62NZEUN6QP2VPQ7ANLLTC2Q"
                                   signer_did (63 bytes)
40 8a c8 92 ac 7b 92 ... ee a6 14 e1 0b
                                   signature: length 64, then 64 bytes
20 14 12 76 4a fb 5f 34 60 f3 a0 e4 f9 9a c6 c5 f0
   60 8d 5f e4 cc 46 2f 82 fb 8e 4e b0 6e a3 c0 13
                                   signed_hash: length 32, then 32 bytes
f0 9f af af 0d                     timestamp: zigzag varint of 1794500600
07 "ed25519"                       method
```

The `signed_hash` bytes equal the file's 32-byte trailer, which ends the fixture ([`atomic_0_19_1_file_add.hex#L23-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L23-L24)). The timestamp, 1794500600, is 2026-11-12T16:23:20Z, the same second as the change's HEADER timestamp. The author's `identity` in the same change's HEADER ([ChangeHeader example](#changeheader-example)) is the signer's public key in base32: BLAKE3 of the decoded 32 bytes, in base32, is the `signer_did` suffix, and the key verifies the signature over the message described on [Signing](container.md#signing) ([`atomic_0_19_1_file_add.hex#L4-L15`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L4-L15)). The DID suffix itself is not the key and does not verify the signature.

## SPAN section

SPAN appears only in schema-version-3 files, exactly once, after the GRAPH sections and before any SEMANTIC section ([`sections.rs#L48-L67`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/sections.rs#L48-L67), [`state_machine.rs#L517-L521`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L517-L521)). It carries a change's immutable span operations, which address authored bytes by change hash and byte range ([`span_patch.rs#L1-L4`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L1-L4)). It is part of the change hash ([`section.rs#L118-L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L118-L120)).

The payload is not plain postcard. It is the four ASCII bytes `ATSP`, then the postcard encoding of a [SpanPatch](#spanpatch), or of a [SpanPatchV1](#spanpatchv1) when the version is 1 ([`span_patch.rs#L131-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L131-L152)):

```text
offset 0   41 54 53 50     "ATSP"
offset 4   version         u8: one bare byte, and the first postcard field
offset 5   rest of the postcard struct
```

The reader reads the version from the byte after the magic, before it decodes anything ([`span_patch.rs#L155-L162`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L155-L162)).

| Version | Constant | Layout | Meaning |
|---|---|---|---|
| 1 | none | [SpanPatchV1](#spanpatchv1) | Original layout, kept so that its bytes and hashes are preserved ([`span_patch.rs#L89-L95`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L89-L95)). |
| 2 | `VERSION` | [SpanPatch](#spanpatch) | What `SpanPatch::new` writes ([`span_patch.rs#L10`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L10), [`span_patch.rs#L121-L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L121-L128)). |
| 3 | `LEGACY_SOURCE_VERSION` | [SpanPatch](#spanpatch) | Same layout as 2; also allows verified legacy graph ranges as authored sources ([`span_patch.rs#L11-L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L11-L13)). |

### SpanPatch

Defined at [`span_patch.rs#L77-L87`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L77-L87). Used for versions 2 and 3.

| # | Field | Rust type | Wire encoding | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `version` | `u8` | one byte | **Required** | 2 or 3. |
| 2 | `prior_metadata` | `Vec<u8>` | varint length, then the bytes | **Optional** | Metadata carried by older span payloads; separate from the HEADER's `metadata` ([`span_patch.rs#L75-L82`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L75-L82)). |
| 3 | `ops` | `Vec<`[SpanOp](#spanop)`>` | varint count, then each op | **Optional** | The change's span operations ([`span_patch.rs#L83-L84`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L83-L84)). |
| 4 | `semantic_owners` | `Vec<`[SemanticOwner](#semanticowner)`>` | varint count, then each owner | **Optional** | Maps the non-ROOT semantic IDs used by this change's SEMANTIC operations to the changes that own them ([`span_patch.rs#L85-L86`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L85-L86), [`semantic_context.rs#L15-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/semantic_context.rs#L15-L24)). |

### SpanPatchV1

Defined at [`span_patch.rs#L89-L95`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L89-L95). The first three fields of [SpanPatch](#spanpatch) (`version` = 1, `prior_metadata`, `ops`) and no `semantic_owners`. A version-1 patch with semantic owners cannot be encoded ([`span_patch.rs#L136-L139`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L136-L139)).

### SpanOp

Defined at [`span_patch.rs#L45-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L45-L73).

| Index | Variant | Fields after the index, in order | Meaning |
|---|---|---|---|
| 0 | `Root` | `path`: `String`; `ordinal`: `u32` varint; `start`: `u64` varint; `end`: `u64` varint | Introduces a file's initial authored range. `start` is inclusive and `end` exclusive, both offsets into this change's contents; `ordinal` forms the file's portable identity ([`span_patch.rs#L47-L57`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L47-L57)). |
| 1 | `Edit` | `file`: [ExternalFile](graph.md#externalfile); `target`: [ExternalSpan](#externalspan); `start`, `end`, `insert_start`, `insert_end`: `u64` varints | Replaces `start..end` of the target span (offsets relative to the target's start) with `insert_start..insert_end` of this change's contents ([`span_patch.rs#L58-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L58-L72)). |

### ExternalSpan

Defined at [`span_patch.rs#L28-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L28-L37).

| # | Field | Rust type | Wire encoding | Meaning |
|---|---|---|---|---|
| 1 | `owner` | `Hash` | 32 raw bytes ([Binary hash form](../types.md#binary-hash-form)) | Hash of the change that owns the bytes. |
| 2 | `start` | `u64` | varint | Inclusive offset in the owner's contents. |
| 3 | `end` | `u64` | varint | Exclusive offset in the owner's contents. |

Span operations name other changes by full 32-byte hash, not by hash index.

### SemanticOwner

Defined at [`semantic_context.rs#L15-L24`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/semantic_context.rs#L15-L24).

| # | Field | Rust type | Wire encoding | Meaning |
|---|---|---|---|---|
| 1 | `authored_id` | `u64` | varint | A non-ROOT numeric name used by this change's semantic encoding. |
| 2 | `owner` | `Hash` | 32 raw bytes ([Binary hash form](../types.md#binary-hash-form)) | The change that introduced the named items. |

**Reader checks.** `SpanPatch::decode` refuses a payload without the `ATSP` prefix, a version other than 1, 2 or 3, and any trailing byte after the struct ([`span_patch.rs#L155-L186`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L155-L186)). `Change::deserialize` also refuses a SPAN in a file that is not schema version 3, a second SPAN, and a payload that does not re-encode to the same bytes ([`change.rs#L708-L724`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L708-L724)), so only the canonical encoding is accepted. A schema-3 file without a SPAN is refused. The header's section total counts one SPAN in schema version 3, and the reader frames exactly that many sections before the trailer ([`header.rs#L372-L390`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/header.rs#L372-L390), [`mod.rs#L427-L428`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/mod.rs#L427-L428)). So a file that simply leaves the SPAN out usually fails first, when the reader takes the trailer's bytes for a section frame: typically `InvalidSectionType`, otherwise an I/O or decompression error ([Reader validation](container.md#reader-validation) step 5). Only a file with another section in the SPAN's place reaches the section-manifest check ([`change.rs#L769-L781`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L769-L781)). The layer-selective section readers refuse schema-3 files ([`sections.rs#L395-L409`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/sections.rs#L395-L409)).

`Change::deserialize` runs the range and owner checks below when it decodes a schema-3 file, and `Change::serialize` runs them before it writes one. Both go through `Change::validate_v2`, which calls `validate_content_ranges` on the span patch ([`change.rs#L324-L338`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L324-L338), [`change.rs#L838-L840`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L838-L840), [`change.rs#L386-L389`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L386-L389)). Applying the change runs them again ([`span_patch_storage.rs#L136-L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/pristine/span_patch_storage.rs#L136-L141)). Only the checks that need repository state wait until then: dependency closure, file ownership and conflicts ([`span_patch.rs#L188-L189`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L188-L189)), and whether `semantic_owners` declares exactly the IDs the SEMANTIC section uses ([Node IDs in semantic identities](graph.md#node-ids-in-semantic-identities)). A failure at decode is `ChangeError::Invalid` carrying the `SpanPatchError` text ([`change.rs#L333-L336`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L333-L336)): `invalid span operation N: <reason>` for a range, and `invalid span patch encoding` for every owner-list failure ([`span_patch.rs#L106-L116`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L106-L116), [`span_patch.rs#L197-L198`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L197-L198)).

`validate_content_ranges` refuses a `Root` range that is reversed or runs past the contents, an `Edit` whose `file` or `target` owner is the zero hash, a reversed target, an edit range that is reversed or runs past the target's length, and an inserted range that is reversed or runs past the contents ([`span_patch.rs#L190-L231`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L190-L231)). It also requires `semantic_owners` to have no `authored_id` of 0, no zero-hash owner, strictly ascending `authored_id`s and no owner hash used twice ([`semantic_context.rs#L80-L100`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/semantic_context.rs#L80-L100)).

**Example.** A test encodes a version-2 patch with no prior metadata, no operations and one semantic owner, `authored_id` 300 and an owner hash of 32 `0xA5` bytes, and asserts these bytes ([`semantic_context.rs#L342-L353`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/semantic_context.rs#L342-L353)):

```text
41 54 53 50        "ATSP"
02                 version: 2
00                 prior_metadata: 0 bytes
00                 ops: 0 entries
01                 semantic_owners: 1 entry
ac 02              authored_id: varint 300
a5 a5 ... a5       owner: 32 bytes of 0xA5
```

The test `legacy_span_payload_roundtrips_without_changing_its_bytes` builds a version-1 payload holding one `Root` op for `file`, ordinal 0, range 0 to 4, with prior metadata `legacy` ([`span_patch.rs#L261-L278`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L261-L278)). By the rules above its bytes are `41 54 53 50 01 06 6c 65 67 61 63 79 01 00 04 66 69 6c 65 00 00 04`: magic, version 1, `prior_metadata` of 6 bytes, one op, variant 0, the 4-byte path, then ordinal, start and end.

## UNHASHED section

The UNHASHED payload is JSON text. `Change::serialize` writes `serde_json::to_vec` of the change's `unhashed` value ([`change.rs#L536-L540`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L536-L540)), which is an arbitrary `serde_json::Value` ([`change.rs#L129-L133`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L129-L133)). `serde_json::to_vec` writes compact JSON with no whitespace between tokens; this is external behaviour of serde_json 1.0.154 ([`Cargo.lock#L3950-L3952`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L3950-L3952); external: [serde_json `ser.rs#L2177-L2184`](https://github.com/serde-rs/json/blob/20572a6d066958542d79cdd6cf1a7cd9f0c8e1f8/src/ser.rs#L2177-L2184) and [`ser.rs#L2206-L2220`](https://github.com/serde-rs/json/blob/20572a6d066958542d79cdd6cf1a7cd9f0c8e1f8/src/ser.rs#L2206-L2220), at the commit serde_json 1.0.154 was published from). Object keys are written in the order of serde_json's `Map`: insertion order when serde_json is built with its `preserve_order` feature, sorted by key otherwise (external: [serde_json `map.rs#L28-L41`](https://github.com/serde-rs/json/blob/20572a6d066958542d79cdd6cf1a7cd9f0c8e1f8/src/map.rs#L28-L41)). The workspace lockfile resolves serde_json with `indexmap` and `foldhash`, the optional dependencies that feature adds ([`Cargo.lock#L3949-L3962`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.lock#L3949-L3962); external: [serde_json `Cargo.toml#L13-L15`](https://github.com/serde-rs/json/blob/20572a6d066958542d79cdd6cf1a7cd9f0c8e1f8/Cargo.toml#L13-L15), [`Cargo.toml#L62-L65`](https://github.com/serde-rs/json/blob/20572a6d066958542d79cdd6cf1a7cd9f0c8e1f8/Cargo.toml#L62-L65)), but the workspace declares serde_json without features ([`Cargo.toml#L59`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/Cargo.toml#L59)), so which order a given binary writes is not fixed by the format ([Open questions](#open-questions)). UNHASHED is outside the change hash, so the key order never changes a change's identity. The reader parses it with `serde_json::from_slice`, and in schema versions 2 and 3 refuses a second UNHASHED section ([`change.rs#L735-L743`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L735-L743)).

The low-level `ChangeWriter::write_unhashed` does not check that its input is JSON ([`sections.rs#L178-L214`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/sections.rs#L178-L214)); a writer test passes it the bytes `unhashed data` ([`tests.rs#L514-L523`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/tests.rs#L514-L523)). `Change::deserialize` would fail on such a file with a JSON error ([`change.rs#L742`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L742), [`change.rs#L90-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L90-L92)), so a writer should only put valid JSON here.

UNHASHED is not part of the change hash, so it can change without changing the change's identity ([`sections.rs#L174-L176`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/sections.rs#L174-L176), [`sections.rs#L216-L217`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/sections.rs#L216-L217)); a test writes the same change with and without it and gets the same hash ([`tests.rs#L605-L638`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/tests.rs#L605-L638)).

**Example.** A test stores `{"editor": "vim", "custom_field": 42}` and reads both keys back ([`change.rs#L1500-L1520`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1500-L1520)).

**Keys Atomic uses.** The core format puts no schema on the value. Atomic's own code treats it as a JSON object and merges namespaced keys into it ([`record.rs#L4032-L4039`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/record.rs#L4032-L4039), [`move_evidence.rs#L271-L281`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/record/move_evidence.rs#L271-L281)). The top-level keys found at this commit are:

| Key | Written by | Source |
|---|---|---|
| `atomic.move_evidence` | Recorded move evidence | [`move_evidence.rs#L15-L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/record/move_evidence.rs#L15-L16) |
| `agent_turn` | Agent turn data from `atomic-agent` | [`storage.rs#L146-L147`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-agent/src/transcript/storage.rs#L146-L147) |
| `git` | Git import; read back as `git.diff_lines` | [`diff_engine.rs#L996-L1012`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/diff_engine.rs#L996-L1012) |

The schemas under these keys are application data, not part of the change format, and are not documented here.

## Change hash coverage

The change hash and how it is computed are on [Change hash](container.md#change-hash). For the sections on this page:

| Section | In the change hash | Source |
|---|---|---|
| HEADER | Yes | [`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195) |
| DEPS | Yes | [`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195) |
| PROVENANCE | Yes | [`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195) |
| SPAN | Yes | [`section.rs#L118-L120`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L118-L120), [`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195) |
| SIGNATURE | No | [`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195); test [`tests_signing.rs#L33-L35`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/tests_signing.rs#L33-L35) |
| UNHASHED | No | [`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195); test [`tests.rs#L605-L638`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/tests.rs#L605-L638) |

For a hashed section, the writer and the reader feed the hasher the section header and the compressed bytes, not the decompressed payload ([`helpers.rs#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/helpers.rs#L113-L117), [`sections.rs#L125-L129`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/sections.rs#L125-L129)). A writer that wants to reproduce an existing change's hash therefore needs both the payload bytes on this page and the same compressed bytes; see [Compression](container.md#compression).

## Open questions

- **Is SIGNATURE hashed?** The code says no: `is_hashed` excludes it ([`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195)), and the writer skips the hasher for it ([`helpers.rs#L113-L117`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/helpers.rs#L113-L117)). The doc comment on `write_signature` says "The SIGNATURE section is a hashed section" and describes a two-level hash ([`state_machine.rs#L438-L445`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/writer/state_machine.rs#L438-L445)). This page follows the code.
- **Stale payload descriptions.** The `SectionType` comment gives the HEADER payload as `postcard(ChangeHeader)` ([`section.rs#L49-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L49-L52)), and so does the module documentation of `change.rs` ([`change.rs#L17-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L17-L18)). That is true only of schema version 1; versions 2 and 3 write [ChangeHeaderV2](#changeheaderv2) ([`change.rs#L466-L470`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L466-L470)). The same module documentation, and the reader's, say every section except UNHASHED is hashed ([`change.rs#L30-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L30-L37), [`mod.rs#L31-L36`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/reader/mod.rs#L31-L36)), which leaves out SIGNATURE.
- **Is the split between the two PROVENANCE decoders intended?** The full change reader falls back to [ProvenanceBeforeXai](#provenancebeforexai) and [ProvenanceV0](#provenancev0) but not to [July2026Provenance](#july2026provenance); the section helper falls back only to July2026Provenance ([Historical layouts](#historical-layouts)). As a result each one misreads a payload the other reads correctly ([July 2026 payloads in the full reader](#july-2026-misread)). A test expects `Change::deserialize` to read the reachable July 2026 change with model `moonshotai` and model version `kimi-k3` ([`sections.rs#L776-L795`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L776-L795)), and the July 2026 layout's doc comment says that change was written in this layout ([`compatibility.rs#L13-L14`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compatibility.rs#L13-L14)). Decoding the fixture in the full reader's fallback gives model `kimi-k3` and no model version instead. The test returns early when that change file is absent ([`sections.rs#L782-L784`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L782-L784)), and `.atomic/` is ignored by Git ([`.gitignore#L141`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/.gitignore#L141)), so it does not run on a clean checkout. The source does not say whether the split is intended. This page documents what each decoder does.
- **Vendor index 8 in pre-xAI provenance.** The old numbering is kept so that older provenance still decodes ([`mod.rs#L199-L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/mod.rs#L199-L228)), but a pre-xAI record with vendor `Local` decodes in the current layout as `XAI` and never reaches that fallback ([AIVendorV0](#aivendorv0)). Whether Atomic wrote such records, and whether reading them as `XAI` is intended, is not stated.
- **`PromptContent::Compressed`.** The variant holds "compressed" prompt bytes ([`types.rs#L449-L450`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L449-L450)), and the compression scheme is not named. No code at this commit builds one. Apart from `PromptContent::hash`, the only code that matches the variant is the Git export check, which treats a compressed prompt, like a full one, as private material that must not enter Git ([`pack.rs#L335-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/git_binding/pack.rs#L335-L341), [`pack.rs#L348-L350`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/git_binding/pack.rs#L348-L350)). Its hash is taken over the stored bytes ([`types.rs#L477`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L477)).
- **`Cost.micro_usd` units.** The field comment says "the smallest currency unit (e.g., cents for USD)" ([`types.rs#L368-L371`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L368-L371)), but `Cost::from_usd` and `Cost::from_micro_usd` use millionths of a dollar ([`types.rs#L375-L389`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/provenance/types.rs#L375-L389)). This page follows the code.
- **Provenance timestamps are untagged.** Nothing in the bytes says whether `Provenance.timestamp` is seconds or milliseconds; Atomic guesses from the magnitude, and its own comment calls values near the epoch "inherently ambiguous" ([`timestamp.rs#L5-L9`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/timestamp.rs#L5-L9)).
- **UNHASHED key order.** Whether a given Atomic binary writes UNHASHED object keys in insertion or sorted order depends on whether some dependency linked into it turns on serde_json's `preserve_order` ([UNHASHED section](#unhashed-section)). The source does not pin it, and the order does not affect the change hash. A reader should not depend on key order.
- **UNHASHED key schemas.** The values stored under `atomic.move_evidence`, `agent_turn` and `git` are defined by `atomic-repository` and `atomic-agent`, not by the change format. Whether a hosted server reads, rewrites or strips UNHASHED is decided by the server, which is not in the source.
