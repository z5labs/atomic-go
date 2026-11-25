# GRAPH and SEMANTIC sections

This page describes the decompressed payload bytes of the two section types that carry a change's edits: GRAPH (type byte `0x10`) and SEMANTIC (type byte `0x30`) ([`section.rs#L142-L155`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L142-L155)). The section framing ([File layout](container.md#file-layout)), the zstd compression around each payload ([Compression](container.md#compression)), the hash dedup table ([HashDedupTable](container.md#hashdeduptable)) and the change hash ([Change hash](container.md#change-hash)) are described on the container page. The other section payloads are described in [sections.md](sections.md).

Both payloads are postcard-encoded Rust values ([`change.rs#L510-L511`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L510-L511), [`change.rs#L524-L526`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L524-L526)). This page gives each value's field order, each field's wire encoding and each enum's variant indices, so that a Go program can produce and parse the bytes without Atomic. The recording algorithm, which decides what edits to write, is out of scope. Gaps are listed under [Open questions](#open-questions).

## Overview

| Section | Rust value serialized | Written by `Change::serialize` when | Sections per change |
|---|---|---|---|
| GRAPH | [GraphSectionPayload](#graphsectionpayload) | the change has at least one hunk | 0 or 1 |
| SEMANTIC | `Vec<FileOps>` (a list of [FileOps](#fileops)) | the change has at least one `FileOps` | 0 or 1 |

- `Change::serialize` builds the GRAPH and SEMANTIC payloads from a change ([`change.rs#L492-L527`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L492-L527)). The local change store can also rebuild a file from the payloads it stored: it decompresses each stored GRAPH and SEMANTIC payload and passes those bytes to the writer unchanged, which compresses them again at its default level; the postcard values are not re-encoded ([`queries.rs#L440-L442`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/redb_change_store/queries.rs#L440-L442), [`queries.rs#L479-L517`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/redb_change_store/queries.rs#L479-L517)).
- The writer puts every hunk of the change into one GRAPH section; the source comment calls per-file splitting future work ([`change.rs#L426-L431`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L426-L431)).
- It writes every `FileOps` of the change into one SEMANTIC section ([`change.rs#L432-L436`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L432-L436), [`change.rs#L521-L527`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L521-L527)).
- The section counts go into the file header ([`change.rs#L440-L444`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L440-L444)). For format versions 2 and 3, the reader rejects a file whose GRAPH or SEMANTIC section count differs from the header ([`change.rs#L604-L607`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L604-L607), [`change.rs#L758-L782`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L758-L782)). See [Versioning](container.md#versioning).
- Sections appear in a fixed order: GRAPH, then SPAN, then SEMANTIC, then CONTENT ([`section.rs#L254-L266`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L254-L266)). For format versions 2 and 3 the reader rejects a section that goes backwards in that order ([`change.rs#L631-L640`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L631-L640)).
- Both sections are hashed: they count toward the change hash ([`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195)). See [Change hash](container.md#change-hash).

**Write exactly what `Change::serialize` writes.** Atomic sometimes rebuilds a change file from the decoded change instead of keeping the original bytes: `Change::hash` re-serializes ([`change.rs#L355-L367`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L355-L367)), and the daemon's push falls back to re-serializing a change whose file is missing ([`services_sync_remote.rs#L388-L398`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/libatomic/src/daemon/services_sync_remote.rs#L388-L398)). A file laid out differently (per-file GRAPH sections, a non-empty `path`, different hash indices) still decodes, but the rebuilt file would not have the same bytes or hash; see [Identity comes from the original bytes](container.md#identity-comes-from-the-original-bytes).

### Reading

`Change::deserialize` handles each section as follows:

- **GRAPH**: decodes a [GraphSectionPayload](#graphsectionpayload), expands each op through the hash table and appends it to the change's hunks. It reads only `ops`; `path`, `content_start` and `content_end` are decoded and discarded ([`change.rs#L695-L707`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L695-L707)).
- **SEMANTIC**: decodes a `Vec<FileOps>` and appends its entries to the change's `file_ops` ([`change.rs#L725-L730`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L725-L730)).

Several sections of the same type are therefore concatenated in file order. Format version 1 files go through the same decoding; only the expansion of name-conflict ops differs (see [Legacy name-conflict markers](#legacy-name-conflict-markers)) ([`change.rs#L700-L705`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L700-L705)).

The reader decodes both payloads with `postcard::from_bytes` ([`sections.rs#L295-L297`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L295-L297), [`change.rs#L727-L728`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L727-L728)), which ignores bytes left over after the value ([Postcard encoding](container.md#postcard-encoding)). Trailing bytes after a GRAPH or SEMANTIC payload are therefore not an error in Atomic's reader.

## Encoding rules used on this page

Every payload is postcard. The rules, including varints, strings, `Option`, `Vec`, structs, newtypes, fixed-size arrays, enum variant indices and the serde attributes that do and do not change the bytes, are on [Postcard encoding](container.md#postcard-encoding); this page uses them without repeating them. Each is visible in the [GRAPH example](#example-graph-payload) and the [SEMANTIC example](#example-semantic-payload), decoded from Atomic's own fixture. Notes specific to the types on this page:

- The [hash index](container.md#hashindex) `0xFFFF`, which GRAPH payloads use for this change, is the varint `ff ff 03`, three bytes. It appears throughout the GRAPH example.
- `usize` fields, the `LineOps` line numbers, are written as `u64` varints ([Postcard encoding](container.md#postcard-encoding)).
- `NodeId` is a newtype over a `u64`, so it is written as that varint ([Node IDs in semantic identities](#node-ids-in-semantic-identities)).
- `Hash` fields, `ExternalFile.owner` and `SpanProjectionId`, are 32 raw bytes ([Binary hash form](../types.md#binary-hash-form)).
- `#[serde(default)]` on some fields of `CompactGraphOp`, `LineOps` and `BranchOp` does not let a writer leave the field out; every field must be written ([Postcard encoding](container.md#postcard-encoding)).
- `#[serde(rename_all = "lowercase")]` on [Encoding](#encoding), and `#[repr(u8)]` with explicit discriminants on [InodeKind](#inodekind), do not change the bytes: serde writes the declaration index. For `InodeKind` the index and the discriminant agree.

## Hash indices and positions

Graph ops refer to other changes by a `u16` index into the file's hash dedup table instead of a 32-byte hash ([`hash_index.rs#L14-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/hash_index.rs#L14-L18), [`hash_index.rs#L39`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/hash_index.rs#L39)). The table, its place in the file and its limits are described in [HashDedupTable](container.md#hashdeduptable).

### What an index means

Every GRAPH index is defined on [HashIndex](container.md#hashindex), which also lists the doc comments that say the opposite. In brief, as Atomic's compactor writes and reads them, `0xFFFF` is this change, index 0 is whatever slot 0 holds (the virtual ROOT, in files Atomic writes), and any other index is the change in that slot ([`compactor.rs#L249-L279`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L249-L279)).

A GRAPH payload carries an index in three places: the `change` of a [CompactPosition](#compactposition), the `change` of a [CompactGraphNode](#compactgraphnode), and the `introduced_by` of a [CompactNewEdge](#compactnewedge). The compactor converts all three with the same `hash_to_index` call ([`compactor.rs#L71-L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L71-L74), [`compactor.rs#L82-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L82-L92), [`compactor.rs#L119-L131`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L119-L131)). Some ops restrict which of these values a field may hold once expanded; for example, a name-conflict op's `name.inode` must name another change, neither `0xFFFF` nor ROOT ([CompactGraphOp](#compactgraphop)). In the [GRAPH example](#example-graph-payload), a new file's name vertex hangs off ROOT (`{0, 0}`), and every position inside the new file is `{0xFFFF, n}`.

### Assigning indices when writing

`Change::serialize` fills the table in this order, and each hash keeps the index of its first insertion ([`hash_table.rs#L267-L280`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/hash_table.rs#L267-L280)):

1. Index 0: 32 zero bytes ([`change.rs#L393-L394`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L393-L394)).
2. The dependencies, the extra-known changes, the superseded change, then the causal-frontier roots, from the change metadata ([`change.rs#L396-L408`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L396-L408)). The [HEADER section](sections.md#header-section) and [DEPS section](sections.md#deps-section) describe these fields.
3. Every `Some` hash in the hunks, op by op, in the order the fields appear in the GRAPH payload ([`change.rs#L966-L1052`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L966-L1052)). Within an insertion that is predecessors, successors, then `inode` ([`change.rs#L919-L931`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L919-L931)); within an edge, `from`, `to`, then `introduced_by` ([`change.rs#L933-L943`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L933-L943)); within an edge update, the edges, then `inode` ([`change.rs#L945-L954`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L945-L954)). `None` references add nothing ([`change.rs#L899-L907`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L899-L907)), and a ROOT reference finds the zero entry already at index 0.

A writer that references a hash it did not put in the table gets `HashNotFound` from Atomic's compactor ([`compactor.rs#L258-L265`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L258-L265); [`hash_table.rs#L330-L334`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/hash_table.rs#L330-L334)).

### CompactPosition

A position in the graph: a byte offset in some change's content ([`hash_index.rs#L115-L132`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/hash_index.rs#L115-L132)). Encoded as two varints; `{0xFFFF, 5}` is `ff ff 03 05`.

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `change` | `HashIndex` (`u16`) | varint | **Required** | Which change the offset is in; see [HashIndex](container.md#hashindex). |
| 2 | `pos` | `u32` | varint | **Required** | Byte offset in that change's content blob ([`hash_index.rs#L125-L131`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/hash_index.rs#L125-L131)). The content blob is the change's CONTENT chunks concatenated in file order ([`change.rs#L731-L734`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L731-L734)); see [the container page](container.md#file-layout). |

In memory, positions are `u64`; the compactor narrows them with `as u32` and does not check for overflow ([`compactor.rs#L71-L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L71-L74), [`compactor.rs#L108-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L108-L115)). Offsets of 2³² or more are silently truncated.

## GRAPH section

The GRAPH payload is one postcard-encoded [GraphSectionPayload](#graphsectionpayload) ([`change.rs#L492-L512`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L492-L512)). Its `ops` are the change's hunks in order, each converted to a [CompactGraphOp](#compactgraphop) by replacing every hash with a table index ([`change.rs#L494-L501`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L494-L501); [`convert.rs#L26-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/convert.rs#L26-L52)). The section does not name a file: Atomic writes an empty `path`, and each op carries its own path ([`change.rs#L503-L508`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L503-L508); [`graph_op.rs#L262-L284`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L262-L284)).

Every field below is always present in the bytes, because postcard has no field tags or defaults. In these tables **Required** means a reader acts on the value; **Optional** means any value of the type is accepted, but it still has to be encoded; **Ignored** means Atomic's reader decodes and discards it.

### GraphSectionPayload

Declared at [`sections.rs#L169-L200`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L169-L200).

| # | Field | Type | Wire | Required | Atomic writes |
|---|---|---|---|---|---|
| 1 | `path` | `String` | string | **Ignored**: not read by `Change::deserialize` ([`change.rs#L695-L707`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L695-L707)) | `""` ([`change.rs#L503-L504`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L503-L504)). The doc comment says this is the file the section covers ([`sections.rs#L171-L175`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L171-L175)); see [Open questions](#open-questions). |
| 2 | `ops` | `Vec<CompactGraphOp>` | count, then each [CompactGraphOp](#compactgraphop) | **Required** | every hunk of the change, in order ([`change.rs#L496-L501`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L496-L501)) |
| 3 | `content_start` | `u64` | varint | **Ignored** | `0` ([`change.rs#L506`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L506)) |
| 4 | `content_end` | `u64` | varint | **Ignored** | the length of the change's whole content blob ([`change.rs#L507`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L507)) |

A test notes that an empty payload with path `src/main.rs` and range `0..1024` is about 16 bytes: 12 for the path, 1 for the empty op list, 1 and 2 for the two varints ([`sections.rs#L1010-L1027`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L1010-L1027)).

### CompactGraphOp

One graph operation (a hunk) ([`graph_op.rs#L19-L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L19-L25)). An enum: a varint variant index, then the variant's fields in the order listed. No variant attribute changes the layout; the `#[serde(default)]` on some `Option` fields has no effect in postcard (see [Encoding rules](#encoding-rules-used-on-this-page)).

| Index | Variant | Fields, in order | Declared at |
|---|---|---|---|
| 0 | `FileAdd` | `add_name`: [CompactInsertion](#compactinsertion), `add_inode`: CompactInsertion, `contents`: `Option<CompactInsertion>`, `path`: `String`, `encoding`: `Option<`[Encoding](#encoding)`>` | [`graph_op.rs#L26-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L26-L40) |
| 1 | `DirAdd` | `add_name`: CompactInsertion, `add_inode`: CompactInsertion, `path`: `String` | [`graph_op.rs#L42-L50`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L42-L50) |
| 2 | `DirDel` | `del`: [CompactEdgeUpdate](#compactedgeupdate), `path`: `String` | [`graph_op.rs#L52-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L52-L58) |
| 3 | `DirUndel` | `undel`: CompactEdgeUpdate, `path`: `String` | [`graph_op.rs#L60-L66`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L60-L66) |
| 4 | `FileDel` | `del`: CompactEdgeUpdate, `contents`: `Option<CompactEdgeUpdate>`, `path`: `String`, `encoding`: `Option<Encoding>` | [`graph_op.rs#L68-L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L68-L80) |
| 5 | `FileUndel` | `undel`: CompactEdgeUpdate, `contents`: `Option<CompactEdgeUpdate>`, `path`: `String`, `encoding`: `Option<Encoding>` | [`graph_op.rs#L82-L94`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L82-L94) |
| 6 | `FileMove` | `del`: CompactEdgeUpdate, `add`: CompactInsertion, `path`: `String` (the new path) | [`graph_op.rs#L96-L104`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L96-L104) |
| 7 | `Edit` | `change`: [CompactAtom](#compactatom), `local`: [Local](#local), `encoding`: `Option<Encoding>` | [`graph_op.rs#L106-L115`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L106-L115) |
| 8 | `Replacement` | `change`: CompactEdgeUpdate, `replacement`: CompactInsertion, `local`: Local, `encoding`: `Option<Encoding>` | [`graph_op.rs#L117-L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L117-L128) |
| 9 | `SolveNameConflict` | `name`: CompactEdgeUpdate, `path`: `String` | [`graph_op.rs#L130-L136`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L130-L136) |
| 10 | `UnsolveNameConflict` | `name`: CompactEdgeUpdate, `path`: `String` | [`graph_op.rs#L138-L144`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L138-L144) |
| 11 | `SolveOrderConflict` | `change`: CompactEdgeUpdate, `local`: Local | [`graph_op.rs#L146-L152`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L146-L152) |
| 12 | `UnsolveOrderConflict` | `change`: CompactEdgeUpdate, `local`: Local | [`graph_op.rs#L154-L160`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L154-L160) |
| 13 | `ResurrectZombies` | `change`: CompactEdgeUpdate, `local`: Local, `encoding`: `Option<Encoding>` | [`graph_op.rs#L162-L171`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L162-L171) |
| 14 | `AddRoot` | `name`: CompactInsertion, `inode`: CompactInsertion | [`graph_op.rs#L173-L179`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L173-L179) |
| 15 | `DelRoot` | `name`: CompactEdgeUpdate, `inode`: CompactEdgeUpdate | [`graph_op.rs#L181-L187`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L181-L187) |
| 16 | `SetAttr` | `inode`: [CompactPosition](#compactposition), `path`: `String`, `value`: [InodeAttr](#inodeattr) | [`graph_op.rs#L189-L197`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L189-L197) |
| 17 | `PrototypeContext` | `effect`: `PrototypeContextEffect` | [`graph_op.rs#L198-L202`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L198-L202) |

Index 17 exists only when atomic-core is built with the `prototype-context-completion` feature, which is off by default ([`graph_op.rs#L198-L202`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L198-L202); [`Cargo.toml#L66-L68`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/Cargo.toml#L66-L68)). Its doc comment calls it a private experimental operation and "not a production discriminant". A default build declares only indices 0 to 16, so it cannot decode index 17; a client should reject it too.

The `path` of an op, or `local.path` for ops that carry a [Local](#local), is how a reader tells which file the op touches; `AddRoot`, `DelRoot` and `PrototypeContext` have none ([`graph_op.rs#L262-L284`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L262-L284)).

**Checks the reader runs.** Atomic validates each op before compacting it and both before and after expanding it, and fails with `InvalidGraphOp` ([`convert.rs#L26-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/convert.rs#L26-L31), [`convert.rs#L236-L240`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/convert.rs#L236-L240), [`convert.rs#L393-L399`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/convert.rs#L393-L399); [`error.rs#L116-L123`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/error.rs#L116-L123)). All other variants pass unchecked at this layer.

- `SetAttr`: `path` must not be empty and `value` must be valid ([InodeAttr](#inodeattr)) ([`graph_op.rs#L207-L215`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L207-L215)). After expansion, `inode` must not be ROOT ([`graph_op.rs#L474-L481`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/graph_op.rs#L474-L481)).
- `SolveNameConflict` and `UnsolveNameConflict`: `path` must not be empty and `name.edges` must not be empty ([`graph_op.rs#L228-L235`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L228-L235)). Every edge must go exactly from `previous` `0x11` (`FOLDER | BLOCK`) to `flag` `0x91` (`FOLDER | BLOCK | DELETED`) for `SolveNameConflict`, and the reverse for `UnsolveNameConflict`; must target a non-empty vertex (`to.start < to.end`); and must not repeat an earlier edge ([`graph_op.rs#L216-L226`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L216-L226), [`graph_op.rs#L241-L258`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L241-L258)). After expansion, `name.inode`, each edge's `to` and each edge's `introduced_by` must each reference another change: not this change (`0xFFFF`) and not ROOT. Each edge's `from` must not be this change, but may be ROOT ([`graph_op.rs#L490-L535`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/graph_op.rs#L490-L535)). A test turns `name.inode.change` into `0xFFFF` and expansion fails ([`tests.rs#L1095-L1118`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/tests.rs#L1095-L1118)).

#### Legacy name-conflict markers

Format version 1 writers emitted `SolveNameConflict` and `UnsolveNameConflict` with no edges. When reading a version 1 file, Atomic accepts such an op when its `path` is not empty and skips the checks above. Version 2 and 3 files, and all writes, reject it ([`convert.rs#L200-L235`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/convert.rs#L200-L235); [`change.rs#L700-L705`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L700-L705); [`tests.rs#L1297-L1321`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/tests.rs#L1297-L1321)). A Go writer must not produce them.

### CompactAtom

Either new content or a set of edge changes ([`types.rs#L277-L287`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L277-L287)). Used by `Edit`.

| Index | Variant | Payload |
|---|---|---|
| 0 | `Insertion` | [CompactInsertion](#compactinsertion) |
| 1 | `EdgeUpdate` | [CompactEdgeUpdate](#compactedgeupdate) |

### CompactInsertion

A new vertex: a byte range of this change's content, joined to the graph after its predecessors and before its successors ([`types.rs#L127-L146`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L127-L146); [`atom.rs#L163-L182`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/atom.rs#L163-L182)).

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `predecessors` | `Vec<CompactPosition>` | count, then each [CompactPosition](#compactposition) | **Required** | Positions that come before the new content; apply adds an edge from each to the new vertex ([`atom.rs#L185-L189`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/atom.rs#L185-L189)). |
| 2 | `successors` | `Vec<CompactPosition>` | count, then each | **Required** | Positions that come after it; apply adds an edge from the new vertex to each ([`atom.rs#L191-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/atom.rs#L191-L195)). |
| 3 | `flag` | `u8` | 1 raw byte | **Required** | [EdgeFlags](#edgeflags) bits for the new edges, typically `BLOCK` for content and `FOLDER` for directory structure ([`atom.rs#L197-L201`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/atom.rs#L197-L201)). |
| 4 | `start` | `u32` | varint | **Required** | Start of the vertex in this change's content blob, inclusive ([`types.rs#L138-L139`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L138-L139)). |
| 5 | `end` | `u32` | varint | **Required** | End, exclusive ([`types.rs#L141-L142`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L141-L142)). `start == end` is an empty vertex, used for structural nodes such as file roots and directory entries ([`atom.rs#L222-L229`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/atom.rs#L222-L229)). |
| 6 | `inode` | `CompactPosition` | as above | **Required** | The file (inode vertex) the new vertex belongs to ([`types.rs#L144-L145`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L144-L145)). |

### CompactEdgeUpdate

A list of edge changes in one file ([`types.rs#L241-L251`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L241-L251)).

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `edges` | `Vec<CompactNewEdge>` | count, then each [CompactNewEdge](#compactnewedge) | **Required** | The edge changes. |
| 2 | `inode` | [CompactPosition](#compactposition) | two varints | **Required** | The file the edges belong to. |

### CompactNewEdge

One edge whose flags change from `previous` to `flag`, for example `BLOCK` to `BLOCK | DELETED` to delete content ([`types.rs#L205-L225`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L205-L225); [`atom.rs#L293-L304`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/atom.rs#L293-L304)).

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `previous` | `u8` | 1 raw byte | **Required** | [EdgeFlags](#edgeflags) the edge has now. |
| 2 | `flag` | `u8` | 1 raw byte | **Required** | EdgeFlags it should have after the change. |
| 3 | `from` | [CompactPosition](#compactposition) | two varints | **Required** | Source position of the edge. |
| 4 | `to` | [CompactGraphNode](#compactgraphnode) | three varints | **Required** | Destination vertex of the edge. |
| 5 | `introduced_by` | `HashIndex` (`u16`) | varint | **Required** | The change that introduced the edge, as a [hash index](container.md#hashindex). The field comment repeats the inverted index meanings ([`types.rs#L219-L224`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L219-L224)); the compactor maps it like any other reference ([`compactor.rs#L119-L131`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L119-L131)). |

### CompactGraphNode

A vertex: a byte range in some change's content ([`types.rs#L27-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L27-L41)). `{change: 0, start: 0, end: 0}` is 3 bytes ([`tests.rs#L136-L145`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/tests.rs#L136-L145)).

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `change` | `HashIndex` (`u16`) | varint | **Required** | The change whose content holds the vertex; see [HashIndex](container.md#hashindex). |
| 2 | `start` | `u32` | varint | **Required** | Start offset, inclusive ([`types.rs#L36-L37`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L36-L37)). |
| 3 | `end` | `u32` | varint | **Required** | End offset, exclusive ([`types.rs#L39-L40`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/types.rs#L39-L40)). Narrowed from `u64` with `as u32`, like positions ([`compactor.rs#L82-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L82-L92)). |

### EdgeFlags

The `flag` and `previous` bytes are the bits of `EdgeFlags` ([`compactor.rs#L111`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L111), [`compactor.rs#L125-L126`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L125-L126)). The bits are defined at [`graph_edge.rs#L19-L41`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/graph_edge.rs#L19-L41):

| Bit | Name | Meaning (from the source comments) |
|---|---|---|
| `0x01` | `BLOCK` | Connects sequential content within a file. |
| `0x04` | `PSEUDO` | Computed during apply; not explicitly stored in changes. |
| `0x10` | `FOLDER` | File-system hierarchy. |
| `0x20` | `PARENT` | The reverse direction of another edge. |
| `0x80` | `DELETED` | The content is removed but stays in the graph. |

When reading, Atomic drops any other bits without an error (`from_bits_truncate`) ([`compactor.rs#L202`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L202), [`compactor.rs#L216-L217`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/compactor.rs#L216-L217)). Name-conflict edges are the exception: before expansion their raw `previous` and `flag` bytes must equal `0x11` or `0x91` exactly, so any other bit fails there ([`graph_op.rs#L241-L247`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L241-L247); [`tests.rs#L1120-L1129`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/tests.rs#L1120-L1129)).

### Local

Where an edit is, for display: a path and a line ([`local.rs#L36-L54`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/local.rs#L36-L54)). The expander copies it unchanged ([`convert.rs#L319-L327`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/convert.rs#L319-L327)).

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `path` | `String` | string | **Optional** | File path relative to the repository root. It is the op's path for `Edit`, `Replacement`, `SolveOrderConflict`, `UnsolveOrderConflict` and `ResurrectZombies` ([`graph_op.rs#L274-L278`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/graph_op.rs#L274-L278)). |
| 2 | `line` | `u64` | varint | **Optional** | 1-based line number: the line after which content is inserted, or the first line deleted ([`local.rs#L49-L53`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/local.rs#L49-L53)). |

The module comment says this information is not part of the change hash ([`local.rs#L11-L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/local.rs#L11-L13)). In V3 it is: it sits in the GRAPH payload, which is hashed ([`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195)).

### Encoding

The text encoding of a file's content ([`encoding.rs#L38-L74`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/encoding.rs#L38-L74)). Written as a varint variant index; `#[serde(rename_all = "lowercase")]` does not change the bytes. Also used by [TrunkOp](#trunkop) `Create`.

| Index | Variant | Meaning |
|---|---|---|
| 0 | `Utf8` | UTF-8 (the Rust default) |
| 1 | `Utf16Le` | UTF-16, little-endian |
| 2 | `Utf16Be` | UTF-16, big-endian |
| 3 | `Latin1` | ISO-8859-1 |
| 4 | `Binary` | not text |

### InodeAttr

A file attribute value set by `SetAttr` ([`attribute.rs#L69-L76`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/attribute.rs#L69-L76)).

| Index | Variant | Payload | Valid values |
|---|---|---|---|
| 0 | `Mode` | `u16`, varint | Unix permission bits `0o000` to `0o777`; larger values fail validation ([`attribute.rs#L86-L92`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/attribute.rs#L86-L92)) |
| 1 | `Kind` | [InodeKind](#inodekind) | any variant |

A test round-trips `SetAttr { inode: <index 1, pos 7>, path: "link", value: Kind(Symlink) }` through postcard and rejects `Mode(0o1000)` ([`tests.rs#L60-L80`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/compact/tests.rs#L60-L80)).

### InodeKind

The kind of a non-directory inode ([`attribute.rs#L6-L16`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/attribute.rs#L6-L16)). Written as a varint variant index, which matches the declared discriminants. Also used by [TrunkOp](#trunkop) `SetKind`.

| Index | Variant | Meaning |
|---|---|---|
| 0 | `Regular` | A regular file. |
| 1 | `Symlink` | A symbolic link; the target is the file content. |
| 2 | `Gitlink` | A Git submodule link; the object ID is the file content. |

### Example GRAPH payload

atomic-core keeps a change file recorded by atomic 0.19.1, as format version 1. It adds `a.txt` holding `alpha\nbeta\ngamma\n` and has no dependencies ([`legacy_history_span_insert_tests.rs#L5-L11`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/repository/tests/legacy_history_span_insert_tests.rs#L5-L11)). Its hash table has one entry, 32 zero bytes ([`atomic_0_19_1_file_add.hex#L1-L3`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L1-L3)). Its content blob is `a.txt` followed by the file content, 22 bytes in all; the decoded bytes are `61 2e 74 78 74 61 6c 70 68 61 0a 62 65 74 61 0a 67 61 6d 6d 61 0a` (CONTENT section at [`atomic_0_19_1_file_add.hex#L20-L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L20-L23)). Version 1 GRAPH payloads decode with the same types as versions 2 and 3 ([`change.rs#L695-L707`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L695-L707)).

Its GRAPH section is at [`atomic_0_19_1_file_add.hex#L15-L18`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L15-L18): 83 compressed bytes. Decompressing them gives these 97 bytes, annotated here by hand. Index 0 is ROOT and `ff ff 03` (`0xFFFF`) is this change.

```text
00                         path: ""                                 GraphSectionPayload
03                         ops: 3
00                         ── op 0: CompactGraphOp 0 = FileAdd
                              add_name: CompactInsertion
01 00 00                        predecessors: [ {change 0, pos 0} ]     ROOT:0
00                              successors: []
11                              flag: 0x11 = FOLDER | BLOCK
00 05                           start 0, end 5                          content "a.txt"
00 00                           inode: {0, 0}                           ROOT:0
                              add_inode: CompactInsertion
01 ff ff 03 05                  predecessors: [ {0xFFFF, 5} ]           this change:5
00                              successors: []
11                              flag: FOLDER | BLOCK
05 05                           start 5, end 5                          empty vertex
ff ff 03 05                     inode: {0xFFFF, 5}
                              contents: Option<CompactInsertion>
01                              Some
01 ff ff 03 05                  predecessors: [ {0xFFFF, 5} ]
00                              successors: []
01                              flag: 0x01 = BLOCK
05 0b                           start 5, end 11                         "alpha\n"
ff ff 03 05                     inode: {0xFFFF, 5}
05 61 2e 74 78 74             path: "a.txt"
01 00                         encoding: Some(Utf8)
07                         ── op 1: CompactGraphOp 7 = Edit
00                            change: CompactAtom 0 = Insertion
01 ff ff 03 0b                  predecessors: [ {0xFFFF, 11} ]
00                              successors: []
01                              flag: BLOCK
0b 10                           start 11, end 16                        "beta\n"
ff ff 03 05                     inode: {0xFFFF, 5}
05 61 2e 74 78 74 02          local: { path "a.txt", line 2 }
01 00                         encoding: Some(Utf8)
07 00 01 ff ff 03 10 00 01 10 16 ff ff 03 05
05 61 2e 74 78 74 03 01 00 ── op 2: Edit, Insertion after {0xFFFF, 16}, BLOCK,
                              16..22 "gamma\n", inode {0xFFFF, 5}, local "a.txt" line 3,
                              Some(Utf8)
00                         content_start: 0
16                         content_end: 22
```

The ops show the shape Atomic gives a new file: a name vertex under ROOT holding the file name, an empty inode vertex after it, and content vertices chained after the inode, each with the inode vertex as `inode`.

## SEMANTIC section

The SEMANTIC payload is a postcard-encoded `Vec<FileOps>`: a varint count, then each [FileOps](#fileops) ([`change.rs#L521-L527`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L521-L527)). It is the change's `file_ops`, the line- and token-level view of the edits used for diffs, blame and review ([`change.rs#L1128-L1140`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L1128-L1140)). The section does not name a file; each `FileOps` carries its own `path`.

It is **not** a [SemanticSectionPayload](#semanticsectionpayload), although the sections module and the section-type comment describe one ([`sections.rs#L10-L13`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L10-L13); [`section.rs#L97-L107`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L97-L107)). The reader decodes `Vec<FileOps>` ([`change.rs#L725-L730`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L725-L730)). The local semantic regenerator also builds a one-element `Vec<FileOps>` per file, "matching the format that Change::serialize produces" ([`semantic_regen.rs#L584-L588`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-repository/src/semantic_regen.rs#L584-L588)).

### FileOps

The operations on one file ([`ops.rs#L88-L110`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L88-L110)). The fields are private in Rust but serialized in this order.

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `trunk_id` | [TrunkId](#trunkid) | two varints | **Required** | The file's identity. See [Node IDs in semantic identities](#node-ids-in-semantic-identities). |
| 2 | `path` | `String` | string | **Required** | The file path. Downstream coverage checks match `FileOps` by path ([`crdt.rs#L71-L73`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/record/workflow/record/crdt.rs#L71-L73)). |
| 3 | `trunk_op` | `Option<`[TrunkOp](#trunkop)`>` | `00`, or `01` then the op | **Optional** | `Some` for a file create, delete, move, undelete or attribute change; `None` when only the content changes ([`ops.rs#L102-L106`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L102-L106), [`ops.rs#L205-L215`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L205-L215)). |
| 4 | `line_ops` | `Vec<LineOps>` | count, then each [LineOps](#lineops) | **Required** | Line operations in the file; may be empty. |

### TrunkOp

A file-level operation ([`trunk.rs#L285-L339`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/trunk.rs#L285-L339)).

| Index | Variant | Fields, in order |
|---|---|---|
| 0 | `Create` | `path`: `String`, `encoding`: `Option<`[Encoding](#encoding)`>` (`None` for binary or unknown) |
| 1 | `Delete` | `trunk`: [TrunkId](#trunkid) |
| 2 | `Move` | `trunk`: TrunkId, `new_path`: `String` |
| 3 | `Undelete` | `trunk`: TrunkId |
| 4 | `SetMode` | `trunk`: TrunkId, `mode`: `u16` (varint) |
| 5 | `SetKind` | `trunk`: TrunkId, `kind`: [InodeKind](#inodekind) |
| 6 | `ProjectedSpanEdit` | `file`: [ExternalFile](#externalfile), `baseline`: [SpanProjectionId](#spanprojectionid) |

`ProjectedSpanEdit` is an edit to a file whose history is held as spans (see the [SPAN section](sections.md#span-section)) rather than as a CRDT trunk ([`trunk.rs#L329-L338`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/trunk.rs#L329-L338)). Atomic builds that `FileOps` with `trunk_id` set to `{0, 0}` ([`ops.rs#L217-L231`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L217-L231)).

### LineOps

An operation on one line ([`ops.rs#L573-L614`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L573-L614)). The three `Option` fields carry `#[serde(default)]`, which has no effect in postcard: they must always be written.

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `branch_id` | [BranchId](#branchid) | two varints | **Required** | The line's identity. |
| 2 | `operation` | [BranchOp](#branchop) | enum | **Required** | What happens to the line. |
| 3 | `old_line_num` | `Option<usize>` | `00`, or `01` then a varint `u64` | **Optional** | Line number in the old file; `None` for pure insertions ([`ops.rs#L587-L592`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L587-L592)). |
| 4 | `new_line_num` | `Option<usize>` | same | **Optional** | Line number in the new file; `None` for pure deletions ([`ops.rs#L594-L599`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L594-L599)). The fixture writes 1, 2, 3 for a file's three lines ([example](#example-semantic-payload)). |
| 5 | `content_range` | `Option<(u64, u64)>` (two `ChangePosition`s) | `00`, or `01` then two varints | **Optional** | Start and end offsets in this change's content blob of the graph vertex that holds the line. Set during recording for inserts; `None` otherwise ([`ops.rs#L601-L613`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L601-L613)). `ChangePosition` serializes as a `u64` ([`node_id.rs#L105-L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/node_id.rs#L105-L121), [`node_id.rs#L190-L196`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/node_id.rs#L190-L196)). |

### BranchOp

A line-level operation ([`branch.rs#L270-L361`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/branch.rs#L270-L361)). The `#[serde(default)]` on `Delete.content` has no effect in postcard.

| Index | Variant | Fields, in order | Meaning |
|---|---|---|---|
| 0 | `Insert` | `after`: `Option<`[BranchId](#branchid)`>`, `content`: `Vec<`[LeafOp](#leafop)`>` | A new line after `after`, or at the start of the file when `None`, holding the tokens in `content` ([`branch.rs#L272-L280`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/branch.rs#L272-L280)). |
| 1 | `Delete` | `branch`: BranchId, `content`: `Vec<LeafOp>` | Delete a line; `content` is the old tokens, kept for display ([`branch.rs#L282-L293`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/branch.rs#L282-L293)). |
| 2 | `Modify` | `branch`: BranchId, `old_content`: `Vec<LeafOp>`, `new_content`: `Vec<LeafOp>` | Replace a line's content, keeping its identity ([`branch.rs#L295-L322`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/branch.rs#L295-L322)). |
| 3 | `Restore` | `branch`: BranchId | Bring back a deleted line ([`branch.rs#L324-L330`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/branch.rs#L324-L330)). |
| 4 | `Reparent` | `branch`: BranchId, `new_after`: `Option<BranchId>` | Move a line to after `new_after`, or to the start of the file when `None` ([`branch.rs#L332-L360`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/branch.rs#L332-L360)). |

### LeafOp

A token-level operation within a line ([`leaf.rs#L310-L350`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/leaf.rs#L310-L350)). Token bytes are a `Vec<u8>`: a varint length, then the raw bytes.

| Index | Variant | Fields, in order | Meaning |
|---|---|---|---|
| 0 | `Insert` | `after`: `Option<`[LeafId](#leafid)`>`, `kind`: [TokenKind](#tokenkind), `content`: `Vec<u8>` | A new token after `after`, or at the start of the line when `None` ([`leaf.rs#L312-L322`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/leaf.rs#L312-L322)). |
| 1 | `Delete` | `leaf`: LeafId | Delete a token ([`leaf.rs#L324-L330`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/leaf.rs#L324-L330)). |
| 2 | `Replace` | `leaf`: LeafId, `new_content`: `Vec<u8>` | Replace a token's bytes, keeping its identity ([`leaf.rs#L332-L341`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/leaf.rs#L332-L341)). |
| 3 | `Restore` | `leaf`: LeafId | Bring back a deleted token ([`leaf.rs#L343-L349`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/leaf.rs#L343-L349)). |

### TokenKind

The category of a token ([`kind.rs#L3-L72`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/diff/token/kind.rs#L3-L72)). Written as a varint variant index.

| Index | Variant | Meaning (from the source comments) |
|---|---|---|
| 0 | `Word` | Identifier or keyword |
| 1 | `Whitespace` | Spaces and tabs, not newlines |
| 2 | `Operator` | Operators such as `+`, `==`, `->` |
| 3 | `Punctuation` | Structural punctuation such as `(`, `;` |
| 4 | `String` | A string literal with its delimiters |
| 5 | `Number` | A numeric literal |
| 6 | `Comment` | A `//` comment |
| 7 | `Newline` | `\n` or `\r\n` |
| 8 | `Other` | Anything else |

### TrunkId

A file's identity: the node that created it and an index ([`ids.rs#L34-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/ids.rs#L34-L52)).

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `change_id` | `NodeId` | varint `u64` | **Required** | See [Node IDs in semantic identities](#node-ids-in-semantic-identities). |
| 2 | `file_idx` | `u32` | varint | **Required** | Index of the file within that node. |

### BranchId

A line's identity ([`ids.rs#L136-L140`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/ids.rs#L136-L140)): `change_id`: `NodeId` (varint `u64`), then `branch_idx`: `u32` (varint). Both **Required**.

### LeafId

A token's identity ([`ids.rs#L224-L228`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/ids.rs#L224-L228)): `change_id`: `NodeId` (varint `u64`), then `leaf_idx`: `u32` (varint). Both **Required**.

### Node IDs in semantic identities

`NodeId` is a newtype over a `u64` and serializes as a plain varint ([`node_id.rs#L123-L130`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/node_id.rs#L123-L130), [`node_id.rs#L105-L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/node_id.rs#L105-L121)). Its doc comment says node IDs are repository-local and not meaningful outside the repository that created them ([`node_id.rs#L123-L127`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/types/node_id.rs#L123-L127)). Unlike the GRAPH section, the SEMANTIC section does not use the hash table. Two values mean different things:

- **`0` names this change.** IDs this change creates carry the placeholder `0` (`NodeId::ROOT`) until the change is applied ([`ops.rs#L263-L270`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L263-L270)). The recorder builds new files, lines and tokens with `NodeId::new(0)` ([`crdt.rs#L29-L31`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/record/workflow/record/crdt.rs#L29-L31)). Every ID in the [fixture](#example-semantic-payload) is `0`.
- **Any other value names an existing node** that the operation refers to ([`ops.rs#L263-L265`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L263-L265)). Atomic applies these IDs as they are, and leaves checking them to whoever applies a change it did not record ([`ops.rs#L267-L269`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L267-L269)). For a change without a SPAN section, Atomic uses the `file_ops` unchanged ([`span_patch.rs#L238-L245`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L238-L245)). For a format version 3 change, which has a SPAN section, each non-zero ID must be declared in the SPAN section's `semantic_owners` as a [SemanticOwner](sections.md#semanticowner): an `authored_id` and the owner change's hash. When a repository applies the change, Atomic maps each declared ID to that repository's ID for the owner hash, and fails on an undeclared or unused ID ([`span_patch.rs#L234-L252`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L234-L252); [`semantic_context.rs#L106-L121`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/semantic_context.rs#L106-L121), [`semantic_context.rs#L126-L173`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/semantic_context.rs#L126-L173)). Decoding the file checks only the shape of the owner list, not whether it matches the IDs used; see the reader checks under [SPAN section](sections.md#span-section), which also describes the SPAN payload.

### ExternalFile

A portable file identity: the change that created the file and the file's ordinal within that change ([`span_patch.rs#L19-L26`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L19-L26)). This is the one definition in this reference. It is used by [TrunkOp](#trunkop) `ProjectedSpanEdit` here, and by the `file` field of the SPAN section's [`Edit` op](sections.md#spanop) ([`span_patch.rs#L58-L63`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L58-L63)).

| # | Field | Type | Wire | Required | Meaning |
|---|---|---|---|---|---|
| 1 | `owner` | `Hash` | 32 raw bytes, no length prefix ([Binary hash form](../types.md#binary-hash-form)) | **Required** | Hash of the change that created the file ([`span_patch.rs#L22-L23`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L22-L23)). |
| 2 | `ordinal` | `u32` | varint | **Required** | Ordinal of the file within that change: the `ordinal` of the SPAN `Root` op that introduced it ([`span_patch.rs#L24-L25`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L24-L25), [`span_patch.rs#L51-L52`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L51-L52); [SpanOp](sections.md#spanop)). |

### SpanProjectionId

A content-addressed identity of a span projection, a newtype over a `Hash` ([`span_patch.rs#L39-L42`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/span_patch.rs#L39-L42)). On the wire it is the hash's 32 raw bytes, since a newtype is written as its inner value ([Binary hash form](../types.md#binary-hash-form)). In `ProjectedSpanEdit` it names the semantic snapshot that the edit is based on, which must be in the edit's dependency closure ([`trunk.rs#L334-L337`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/crdt/trunk.rs#L334-L337)).

### SemanticSectionPayload

Defined in the sections module, and built there by the `SectionPair` helper, but not written to a change file by any writer at this commit; `Change::serialize` writes `Vec<FileOps>` instead ([`change.rs#L521-L527`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L521-L527); [`sections.rs#L377-L400`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L377-L400), [`sections.rs#L553-L619`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L553-L619)). Its fields, in order, are `path`: `String`, `file_ops`: one [FileOps](#fileops), `content_start`: `u64`, `content_end`: `u64`. A SEMANTIC section in this shape is not read as one: `Change::deserialize` decodes every SEMANTIC payload as a `Vec<FileOps>` ([`change.rs#L725-L730`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L725-L730)). By the postcard rules, the `path` length would then be read as the number of `FileOps`: an empty path reads as an empty list, with the rest ignored as trailing bytes, and any other path is decoded as `FileOps` bytes. Do not write it.

### Example SEMANTIC payload

The same 0.19.1 fixture has a SEMANTIC section at [`atomic_0_19_1_file_add.hex#L18-L20`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/fixtures/atomic_0_19_1_file_add.hex#L18-L20): 84 compressed bytes. Decompressing them gives these 83 bytes, annotated here by hand:

```text
01                         Vec<FileOps>: 1 entry
00 00                      trunk_id: {change_id 0, file_idx 0}
05 61 2e 74 78 74          path: "a.txt"
01 00                      trunk_op: Some(TrunkOp 0 = Create)
05 61 2e 74 78 74            path: "a.txt"
01 00                        encoding: Some(Utf8)
03                         line_ops: 3
00 00                      ── line: branch_id {0, 0}
00 00                         operation: BranchOp 0 = Insert, after: None
01                            content: 1 LeafOp
00 00 00                        LeafOp 0 = Insert, after: None, kind: TokenKind 0 = Word
05 61 6c 70 68 61               content: "alpha"
00                            old_line_num: None
01 01                         new_line_num: Some(1)
01 05 0b                      content_range: Some((5, 11))
00 01                      ── line: branch_id {0, 1}
00 01 00 00                   Insert, after: Some({0, 0})
01 00 00 00 04 62 65 74 61    content: [Insert, after None, Word, "beta"]
00 01 02                      old_line_num: None, new_line_num: Some(2)
01 0b 10                      content_range: Some((11, 16))
00 02 00 01 00 01 01 00 00 00 05 67 61 6d 6d 61 00 01 03 01 10 16
                           ── line: branch_id {0, 2}, Insert after Some({0, 1}),
                              [Insert, after None, Word, "gamma"], None, Some(3),
                              Some((16, 22))
```

Each line's `content_range` covers the same bytes as the matching GRAPH insertion, newline included, while its one token holds the word without the newline.

## Open questions

- **Per-file sections.** The doc comments describe one GRAPH and one SEMANTIC section per modified file, named by a `path` and given a content range ([`sections.rs#L41-L58`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L41-L58); [`section.rs#L76-L83`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L76-L83)). The writer writes one GRAPH section with an empty path and one SEMANTIC section holding a `Vec<FileOps>`, not a per-file [SemanticSectionPayload](#semanticsectionpayload) ([`change.rs#L426-L436`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L426-L436)). The source does not say whether the server ever splits sections per file, or whether a later format version will.
- **Thin pulls and hashing.** The SEMANTIC section is described as optional, left out by a thin pull and able to be regenerated ([`sections.rs#L341-L352`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/sections.rs#L341-L352)). It is also hashed ([`section.rs#L193-L195`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/section.rs#L193-L195)), and for versions 2 and 3 the reader requires the section count to match the header ([`change.rs#L769-L781`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/change.rs#L769-L781)). How a client should verify a change downloaded without its SEMANTIC section (see [Layer](../types.md#layer)) is not settled here.
- **Portability of non-zero node IDs.** For a change without a SPAN section, non-zero `NodeId`s in the SEMANTIC section are the recording repository's local IDs, and Atomic applies them unchanged ([Node IDs in semantic identities](#node-ids-in-semantic-identities)). The source does not show how a receiving repository or the server makes sense of them, or whether a client may safely write any non-zero ID outside a version 3 change.
- **`#[serde(default)]` on `LineOps`.** The comments say the default lets missing fields decode as `None` ([`ops.rs#L590-L591`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/ops.rs#L590-L591)). In postcard a missing field cannot be detected, so a payload written before these fields existed would not decode. Whether such payloads exist is unknown.
- **`Local` and the hash.** `local.rs` says `Local` is not part of the change hash, but in V3 it is inside the hashed GRAPH payload ([Local](#local)). Changing a line number changes the change's identity.
- **Unknown flag bits.** Atomic drops undefined `EdgeFlags` bits on read without an error ([EdgeFlags](#edgeflags)), so re-serializing such a change gives different bytes. Whether the server rejects unknown bits is unknown.
- **Positions above 4 GiB.** A doc comment says a change's content is limited to 4 GB ([`hash_index.rs#L127-L128`](https://github.com/atomicdotdev/atomic/blob/a6c3e359456e83cf5a967a2e8265f11e0ba45d80/atomic-core/src/change/format_v3/types/hash_index.rs#L127-L128)), but the compactor truncates positions to `u32` without a check ([CompactPosition](#compactposition)). Where, if anywhere, the limit is enforced is not shown.
- **`PrototypeContext`.** Variant 17 exists only behind a feature that no crate in the repository enables by default ([CompactGraphOp](#compactgraphop)). Whether a server built with that feature could send it is unknown.
