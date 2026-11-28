// Package changefixture builds the change files that the tests of package
// changefile and package change share.
//
// It imports only ident and zstdframe, and never changefile or change, so
// an in-package test of either can import it without an import cycle. It
// builds each file from the bytes the documentation gives, not with the
// code under test.
package changefixture

import (
	"encoding/binary"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/zstdframe"
)

const (
	// WorkedExampleHash is the change hash of [WorkedExample], the value
	// Atomic's test asserts.
	WorkedExampleHash = "Z6ICEFZQ3TKMR4YIRONQAAA4GF6CKGQPMZJWVK6YU5O5SAX456HA"

	// WorkedExampleContent is the content blob of [WorkedExample], its one
	// CONTENT chunk.
	WorkedExampleContent = "old bytes\n"
)

// WorkedExampleHeader returns the 54-byte decompressed HEADER payload of
// [WorkedExample], from docs/remote/change-format/sections.md#header-example:
// the schema-2 envelope of a change with the message "opaque v2 fixture",
// timestamp 1700000000 and metadata "ATSP" 01 00 00.
func WorkedExampleHeader() []byte {
	b := []byte{17} // header.message: length 17
	b = append(b, "opaque v2 fixture"...)
	b = append(b, 0)  // header.description: None
	b = append(b, 20) // header.timestamp: length 20
	b = append(b, "2023-11-14T22:13:20Z"...)
	b = append(b,
		0, // header.authors: 0 entries
		0, // kind: Durable
		0, // supersedes: None
		0, // origin: Native
		0, // causal_frontier: 0 entries
		0, // extra_known: 0 entries
	)
	b = append(b, 7) // metadata: 7 bytes
	b = append(b, "ATSP\x01\x00\x00"...)
	return b
}

// WorkedExample returns the 275-byte version 2 file
// span_opaque_v2_reference.hex, rebuilt from
// docs/remote/change-format/container.md#worked-example-a-version-2-file:
// the header, a one-entry table holding the zero hash, a HEADER section
// holding [WorkedExampleHeader], a DEPS section holding the empty list 00,
// one CONTENT chunk holding [WorkedExampleContent], and the trailer. Every
// payload is a [zstdframe.Stored] frame, as in Atomic's file. Each call
// returns a new slice, which the caller may modify.
func WorkedExample() []byte {
	var z zstdframe.Stored
	le := binary.LittleEndian

	b := []byte("ATOM")
	b = le.AppendUint32(b, 2)          // version
	b = le.AppendUint32(b, 0)          // flags
	b = le.AppendUint32(b, 1)          // hash_table_entries
	b = le.AppendUint32(b, 0)          // graph_section_count
	b = le.AppendUint32(b, 0)          // semantic_section_count
	b = le.AppendUint32(b, 1)          // contents_chunks
	b = le.AppendUint64(b, 0)          // total_uncompressed
	b = append(b, make([]byte, 28)...) // reserved
	hashFrom := len(b)                 // the hash covers the table onwards
	b = append(b, make([]byte, 32)...) // table slot 0: the zero hash

	b = appendSection(b, 0x01, z.Compress(nil, WorkedExampleHeader()))
	b = appendSection(b, 0x02, z.Compress(nil, []byte{0}))

	content := []byte(WorkedExampleContent)
	chunk := ident.Sum(content)
	compressed := z.Compress(nil, content)
	b = append(b, 0x20)
	b = le.AppendUint32(b, 0) // chunk_index
	b = append(b, chunk[:]...)
	b = le.AppendUint32(b, uint32(len(content)))
	b = le.AppendUint32(b, uint32(len(compressed)))
	b = append(b, compressed...)

	trailer := ident.Sum(b[hashFrom:])
	return append(b, trailer[:]...)
}

// appendSection appends a 5-byte frame of type t and then payload to b.
func appendSection(b []byte, t byte, payload []byte) []byte {
	b = append(b, t)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(payload)))
	return append(b, payload...)
}
