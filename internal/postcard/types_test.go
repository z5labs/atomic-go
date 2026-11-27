package postcard_test

import "github.com/z5labs/atomic-go/internal/postcard"

// The types below mirror the Rust types the goldens come from, so the tests
// can decode and encode them field by field as real callers will.

// compactPosition is atomic-core's CompactPosition { change: u16, pos: u32 }.
type compactPosition struct {
	Change uint16
	Pos    uint32
}

func decodeCompactPosition(d *postcard.Decoder) compactPosition {
	return compactPosition{Change: d.U16(), Pos: d.U32()}
}

func encodeCompactPosition(e *postcard.Encoder, p compactPosition) {
	e.U16(p.Change)
	e.U32(p.Pos)
}

// syncWants is atomic-objects' SyncWants { refs: Vec<String>, haves:
// Vec<String>, refs_only: bool }.
type syncWants struct {
	Refs     []string
	Haves    []string
	RefsOnly bool
}

func decodeSyncWants(d *postcard.Decoder) syncWants {
	return syncWants{
		Refs:     postcard.Seq(d, (*postcard.Decoder).Str),
		Haves:    postcard.Seq(d, (*postcard.Decoder).Str),
		RefsOnly: d.Bool(),
	}
}

func encodeSyncWants(e *postcard.Encoder, w syncWants) {
	postcard.PutSeq(e, w.Refs, (*postcard.Encoder).Str)
	postcard.PutSeq(e, w.Haves, (*postcard.Encoder).Str)
	e.Bool(w.RefsOnly)
}

// objectFamilies is the number of ObjectFamily variants,
// docs/remote/endpoints/project.md#objectfamily.
const objectFamilies = 6

// objectRecord is ObjectRecord { family: ObjectFamily, key: String, bytes:
// Vec<u8> }.
type objectRecord struct {
	Family uint32
	Key    string
	Bytes  []byte
}

// refRecord is RefRecord { name: String, expect_old: Option<String>,
// new_target: String }.
type refRecord struct {
	Name      string
	ExpectOld *string
	NewTarget string
}

// syncPack is SyncPack { objects: Vec<ObjectRecord>, refs: Vec<RefRecord> }.
type syncPack struct {
	Objects []objectRecord
	Refs    []refRecord
}

func decodeObjectRecord(d *postcard.Decoder) objectRecord {
	return objectRecord{Family: d.Variant(objectFamilies), Key: d.Str(), Bytes: d.ByteSeq()}
}

func decodeRefRecord(d *postcard.Decoder) refRecord {
	return refRecord{
		Name:      d.Str(),
		ExpectOld: postcard.Option(d, (*postcard.Decoder).Str),
		NewTarget: d.Str(),
	}
}

func decodeSyncPack(d *postcard.Decoder) syncPack {
	return syncPack{
		Objects: postcard.Seq(d, decodeObjectRecord),
		Refs:    postcard.Seq(d, decodeRefRecord),
	}
}

func encodeSyncPack(e *postcard.Encoder, p syncPack) {
	postcard.PutSeq(e, p.Objects, func(e *postcard.Encoder, o objectRecord) {
		e.Variant(o.Family)
		e.Str(o.Key)
		e.ByteSeq(o.Bytes)
	})
	postcard.PutSeq(e, p.Refs, func(e *postcard.Encoder, r refRecord) {
		e.Str(r.Name)
		postcard.PutOption(e, r.ExpectOld, (*postcard.Encoder).Str)
		e.Str(r.NewTarget)
	})
}

// readSyncPack decodes a SyncPack from a stream, one object at a time, the
// way a large pull is read.
func readSyncPack(r *postcard.Reader) syncPack {
	var p syncPack
	n := r.SeqLen()
	for i := 0; i < n && r.Err() == nil; i++ {
		o := objectRecord{Family: r.Variant(objectFamilies), Key: r.Str(), Bytes: r.ByteSeq()}
		if r.Err() == nil {
			p.Objects = append(p.Objects, o)
		}
	}
	n = r.SeqLen()
	for i := 0; i < n && r.Err() == nil; i++ {
		ref := refRecord{Name: r.Str()}
		if r.OptionTag() {
			s := r.Str()
			ref.ExpectOld = &s
		}
		ref.NewTarget = r.Str()
		if r.Err() == nil {
			p.Refs = append(p.Refs, ref)
		}
	}
	if r.Err() != nil {
		return syncPack{}
	}
	if p.Objects == nil {
		p.Objects = []objectRecord{}
	}
	if p.Refs == nil {
		p.Refs = []refRecord{}
	}
	return p
}

// mixed exercises every Decoder method in one value, for the fuzzer.
type mixed struct {
	A  uint8
	B  int8
	C  uint16
	D  uint32
	E  uint64
	F  int16
	G  int32
	H  int64
	I  bool
	J  float32
	K  float64
	L  []byte
	M  string
	N  []byte
	O  [32]byte
	P  *uint64
	Q  uint32
	R  []int64
	S  []compactPosition
	T  *string
	UU []bool
}

func decodeMixed(d *postcard.Decoder) mixed {
	return mixed{
		A: d.U8(), B: d.I8(), C: d.U16(), D: d.U32(), E: d.U64(),
		F: d.I16(), G: d.I32(), H: d.I64(), I: d.Bool(), J: d.F32(), K: d.F64(),
		L: d.ByteSeq(), M: d.Str(), N: d.Fixed(3), O: d.Array32(),
		P: postcard.Option(d, (*postcard.Decoder).U64), Q: d.Variant(4),
		R:  postcard.Seq(d, (*postcard.Decoder).I64),
		S:  postcard.Seq(d, decodeCompactPosition),
		T:  postcard.Option(d, (*postcard.Decoder).Str),
		UU: postcard.Seq(d, (*postcard.Decoder).Bool),
	}
}

func encodeMixed(e *postcard.Encoder, m mixed) {
	e.U8(m.A)
	e.I8(m.B)
	e.U16(m.C)
	e.U32(m.D)
	e.U64(m.E)
	e.I16(m.F)
	e.I32(m.G)
	e.I64(m.H)
	e.Bool(m.I)
	e.F32(m.J)
	e.F64(m.K)
	e.ByteSeq(m.L)
	e.Str(m.M)
	e.Fixed(m.N)
	e.Array32(m.O)
	postcard.PutOption(e, m.P, (*postcard.Encoder).U64)
	e.Variant(m.Q)
	postcard.PutSeq(e, m.R, (*postcard.Encoder).I64)
	postcard.PutSeq(e, m.S, encodeCompactPosition)
	postcard.PutOption(e, m.T, (*postcard.Encoder).Str)
	postcard.PutSeq(e, m.UU, (*postcard.Encoder).Bool)
}
