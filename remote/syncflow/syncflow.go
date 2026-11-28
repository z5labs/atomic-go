// Package syncflow holds the pure building blocks every push, pull and
// clone starts from: reading what a remote advertises, resolving a view's
// chain of ancestors out of a pulled pack, and checking that each object's
// key matches its bytes.
//
// [ParseAdvertisement] reads the answer to a refs-only pull, the ref
// advertisement a push plans against, into an [Advertisement]: each view's
// current target and, when the pack carries its snapshot, its verified
// manifest. [ResolveChain] turns a full pull or clone into the [ChainView]s
// of a view and its ancestors, root first, the order they are applied in.
// [VerifyKey] checks one [syncproto.ObjectRecord] against its key by the
// rule its family is keyed by, so an object is never stored under a name it
// does not have; [ChangeKey] and [SidecarKey] compute two of those keys.
//
// A push is planned, packed and checked by three more. [PlanPush] compares
// a chain of [LocalView]s with an advertisement and decides, per view, what
// to store and whether to declare, or reports a [Conflict]. [BuildPack]
// turns that [Plan] into the pack Atomic's push sends, in its order,
// reading change files, sidecars and tags from a [PackSource] the caller
// implements over its own repository. [VerifyPush] checks the advertisement
// re-read after the push against the plan, returning a [VerifyError].
//
// Nothing here touches the network. A remote is assumed to send every
// ancestor's ref and snapshot on a full pull, as Atomic's test peer does;
// one it leaves out is an error, not a guess.
//
// The flows are documented in docs/remote/flows.md, under Request 1: ref
// advertisement, Planning what to send, Building the pack, Request 4:
// verify and Reading the chain and saving changes, and the per-family keys
// in docs/remote/endpoints/project.md, under ObjectRecord.
package syncflow

import (
	"errors"
	"fmt"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/tags"
)

var (
	// ErrKeyMismatch reports an object whose key is not the key its bytes
	// have, including a key that does not parse as its family's key.
	//
	// Bytes that cannot be read at all are reported by their own error
	// instead: a Change whose file [changefile.Verify] refuses, such as one
	// whose trailer disagrees with its contents
	// ([changefile.ErrHashMismatch]), or a Tag that does not decode
	// ([tags.ErrMalformed]). Every non-nil [VerifyKey] error means the
	// object must not be stored under its key.
	ErrKeyMismatch = errors.New("syncflow: object key does not match its bytes")

	// ErrUnverifiable reports an object whose family has no known key
	// rule: a [syncproto.Conflict] object, or one of a family that is not
	// [syncproto.Family.Known].
	ErrUnverifiable = errors.New("syncflow: object key cannot be verified")

	// ErrViewNotInPack reports a view whose ref, or whose ref's snapshot,
	// the pack does not carry.
	ErrViewNotInPack = errors.New("syncflow: view not in pack")

	// ErrViewCycle reports a view chain that reaches a view it has already
	// passed through.
	ErrViewCycle = errors.New("syncflow: view chain has a cycle")
)

// ChangeKey returns the key a Change object is stored under: the base32
// text of its change hash h, the BLAKE3 hash [changefile.Verify] returns.
func ChangeKey(h ident.Hash) string {
	return h.String()
}

// SidecarKey returns the key an Attest or Provenance object with bytes b is
// stored under: the base32 text of the BLAKE3 hash of b.
func SidecarKey(b []byte) string {
	return ident.Sum(b).String()
}

// VerifyKey checks that obj's Key is the key its Bytes have, by the rule of
// its family:
//
//   - [syncproto.Change]: [changefile.Verify], with opts, must accept the
//     bytes, and the hash it returns must equal [ident.ParseHash] of the
//     key.
//   - [syncproto.Attest] and [syncproto.Provenance]: [ident.Sum] of the
//     bytes must equal [ident.ParseHash] of the key.
//   - [syncproto.View]: [ident.Sum] of the bytes must equal
//     [ident.ParseHashHex] of the key. Upper-case hex is accepted, although
//     Atomic writes lower case; see Open questions in
//     docs/remote/endpoints/project.md.
//   - [syncproto.Tag]: the bytes must decode as a [tags.Record], and its
//     [tags.Record.ContentHash] must equal [ident.ParseHash] of the key.
//
// opts are read only for a Change. Keys are compared as parsed hashes, so
// base32 in either case passes.
//
// A key that does not parse, or names other bytes, is an error wrapping
// [ErrKeyMismatch]. Bytes that [changefile.Verify] refuses, or that do not
// decode as a tag record, are an error wrapping that failure. A
// [syncproto.Conflict] object, or one whose family is not
// [syncproto.Family.Known], is an error wrapping [ErrUnverifiable].
func VerifyKey(obj syncproto.ObjectRecord, opts ...changefile.ReadOption) error {
	var got ident.Hash
	switch obj.Family {
	case syncproto.Change:
		h, err := changefile.Verify(obj.Bytes, opts...)
		if err != nil {
			return fmt.Errorf("syncflow: change %q: %w", obj.Key, err)
		}
		got = h
	case syncproto.Attest, syncproto.Provenance:
		got = ident.Sum(obj.Bytes)
	case syncproto.View:
		return compareKey(obj, ident.Sum(obj.Bytes), ident.ParseHashHex)
	case syncproto.Tag:
		var r tags.Record
		if err := r.UnmarshalPostcard(obj.Bytes); err != nil {
			return fmt.Errorf("syncflow: tag %q: %w", obj.Key, err)
		}
		got = r.ContentHash()
	default:
		return fmt.Errorf("%w: %v object %q", ErrUnverifiable, obj.Family, obj.Key)
	}
	return compareKey(obj, got, ident.ParseHash)
}

// compareKey parses obj's key with parse and compares it with got, the key
// obj's bytes have.
func compareKey(obj syncproto.ObjectRecord, got ident.Hash, parse func(string) (ident.Hash, error)) error {
	want, err := parse(obj.Key)
	if err != nil {
		return fmt.Errorf("%w: %v object key %q: %w", ErrKeyMismatch, obj.Family, obj.Key, err)
	}
	if want != got {
		return fmt.Errorf("%w: %v object keyed %q has bytes keyed %q", ErrKeyMismatch, obj.Family, obj.Key, format(obj.Family, got))
	}
	return nil
}

// format returns h as the text a key of family f is written in.
func format(f syncproto.Family, h ident.Hash) string {
	if f == syncproto.View {
		return h.Hex()
	}
	return h.String()
}
