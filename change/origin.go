package change

import (
	"errors"
	"fmt"
)

// ErrInvalidOrigin is returned by [Origin.Validate], wrapped, for an
// origin that breaks one of its rules. Test for it with [errors.Is].
var ErrInvalidOrigin = errors.New("invalid change origin")

// OriginKind says where a change came from. The values are the variant
// indices the HEADER payload writes
// (docs/remote/change-format/sections.md#wirechangeoriginv2).
type OriginKind uint8

const (
	// Native is a change authored natively by Atomic. It has no Git
	// fields.
	Native OriginKind = iota

	// GitSynthesized is a change synthesized from one Git commit.
	GitSynthesized

	// GitResolution is a change synthesized to resolve a Git merge
	// commit.
	GitResolution
)

// String returns the kind's name as Atomic spells it, or OriginKind(n) for
// a value with no name.
func (k OriginKind) String() string {
	switch k {
	case Native:
		return "Native"
	case GitSynthesized:
		return "GitSynthesized"
	case GitResolution:
		return "GitResolution"
	default:
		return fmt.Sprintf("OriginKind(%d)", uint8(k))
	}
}

// GitDerivation says how a [GitSynthesized] change was derived from its
// commit, and so how many parents the commit may have. The values are the
// variant indices the HEADER payload writes
// (docs/remote/change-format/sections.md#wiregitderivationv2).
type GitDerivation uint8

const (
	// DerivationRoot is a root commit, with exactly 0 parents.
	DerivationRoot GitDerivation = iota

	// DerivationFirstParent is a commit with exactly 1 parent.
	DerivationFirstParent

	// DerivationMultiParent is a commit with 2 or more parents.
	DerivationMultiParent

	// DerivationSquash is a squash, with any number of parents.
	DerivationSquash

	// DerivationEmptyCommit is a commit that changes nothing, with any
	// number of parents.
	DerivationEmptyCommit

	// DerivationRewriteCandidate is a commit that may be rewritten, with
	// any number of parents.
	DerivationRewriteCandidate
)

// String returns the derivation's name as Atomic spells it, or
// GitDerivation(n) for a value with no name.
func (d GitDerivation) String() string {
	switch d {
	case DerivationRoot:
		return "Root"
	case DerivationFirstParent:
		return "FirstParent"
	case DerivationMultiParent:
		return "MultiParent"
	case DerivationSquash:
		return "Squash"
	case DerivationEmptyCommit:
		return "EmptyCommit"
	case DerivationRewriteCandidate:
		return "RewriteCandidate"
	default:
		return fmt.Sprintf("GitDerivation(%d)", uint8(d))
	}
}

// Sizes of a [GitObjectID]'s Bytes for each algorithm.
const (
	SHA1Size   = 20
	SHA256Size = 32
)

// GitObjectID is a Git object id: SHA-1, of [SHA1Size] bytes, or SHA-256,
// of [SHA256Size] bytes
// (docs/remote/change-format/sections.md#wiregitobjectidv2).
type GitObjectID struct {
	// SHA256 is true for a SHA-256 id and false for a SHA-1 id.
	SHA256 bool

	// Bytes is the id's raw bytes, not its hex text.
	Bytes []byte
}

func (id GitObjectID) algorithm() string {
	if id.SHA256 {
		return "SHA-256"
	}
	return "SHA-1"
}

func (id GitObjectID) size() int {
	if id.SHA256 {
		return SHA256Size
	}
	return SHA1Size
}

func (id GitObjectID) isZero() bool {
	return !id.SHA256 && len(id.Bytes) == 0
}

// Origin says where a change came from: [Native], or the Git commit it was
// synthesized from (docs/remote/change-format/sections.md#wirechangeoriginv2).
// The zero Origin is Native.
type Origin struct {
	// Kind is where the change came from.
	Kind OriginKind

	// Commit is the Git commit a GitSynthesized change was made from, or
	// the merge commit a GitResolution change resolves. It is zero for a
	// Native change.
	Commit GitObjectID

	// Parents is the commit's full parent list, in Git's order. It is
	// empty for a Native change.
	Parents []GitObjectID

	// Derivation says how a GitSynthesized change was derived. A
	// GitResolution or Native origin has none, and leaves it zero, which
	// is the same value as DerivationRoot.
	Derivation GitDerivation
}

// Validate checks a Git origin the way Atomic's ChangeOrigin::validate
// does while it decodes a change: every object id is [SHA1Size] bytes for
// SHA-1 and [SHA256Size] for SHA-256, the commit and every parent use one
// algorithm, and a [GitSynthesized] origin's parent count fits its
// derivation — exactly 0 for [DerivationRoot], exactly 1 for
// [DerivationFirstParent], at least 2 for [DerivationMultiParent], and any
// number for the other three.
//
// A [Native] origin is valid when it has no Git fields. Validate also
// refuses an unknown kind or derivation, and a Derivation other than
// [DerivationRoot] on a Native or GitResolution origin, since the HEADER
// payload could not carry either. DerivationRoot is GitDerivation's zero
// value, so on those origins it reads as "no derivation" and passes.
//
// Validate does not check the rule that a [GitResolution] has at least two
// parents, which depends on the rest of the change and is checked with it.
// A failure wraps [ErrInvalidOrigin]; the HEADER decoder reports it as a
// change-file error of kind changefile.ErrInvalidChangeMetadata.
func (o Origin) Validate() error {
	switch o.Kind {
	case Native:
		switch {
		case !o.Commit.isZero():
			return fmt.Errorf("%w: a Native origin has a commit", ErrInvalidOrigin)
		case len(o.Parents) > 0:
			return fmt.Errorf("%w: a Native origin has %d parents", ErrInvalidOrigin, len(o.Parents))
		case o.Derivation != DerivationRoot:
			return fmt.Errorf("%w: a Native origin has derivation %s", ErrInvalidOrigin, o.Derivation)
		}
		return nil
	case GitSynthesized, GitResolution:
	default:
		return fmt.Errorf("%w: unknown kind %s", ErrInvalidOrigin, o.Kind)
	}

	if err := checkObjectID("commit", o.Commit); err != nil {
		return err
	}
	for i, p := range o.Parents {
		if err := checkObjectID(fmt.Sprintf("parent %d", i), p); err != nil {
			return err
		}
		if p.SHA256 != o.Commit.SHA256 {
			return fmt.Errorf("%w: parent %d is %s but the commit is %s", ErrInvalidOrigin, i, p.algorithm(), o.Commit.algorithm())
		}
	}

	if o.Kind == GitResolution {
		if o.Derivation != DerivationRoot {
			return fmt.Errorf("%w: a GitResolution origin has derivation %s", ErrInvalidOrigin, o.Derivation)
		}
		return nil
	}

	n := len(o.Parents)
	switch o.Derivation {
	case DerivationRoot:
		if n != 0 {
			return fmt.Errorf("%w: a Root commit has %d parents, want 0", ErrInvalidOrigin, n)
		}
	case DerivationFirstParent:
		if n != 1 {
			return fmt.Errorf("%w: a FirstParent commit has %d parents, want 1", ErrInvalidOrigin, n)
		}
	case DerivationMultiParent:
		if n < 2 {
			return fmt.Errorf("%w: a MultiParent commit has %d parents, want at least 2", ErrInvalidOrigin, n)
		}
	case DerivationSquash, DerivationEmptyCommit, DerivationRewriteCandidate:
	default:
		return fmt.Errorf("%w: unknown derivation %s", ErrInvalidOrigin, o.Derivation)
	}
	return nil
}

func checkObjectID(what string, id GitObjectID) error {
	if len(id.Bytes) != id.size() {
		return fmt.Errorf("%w: %s is a %d-byte %s id, want %d bytes", ErrInvalidOrigin, what, len(id.Bytes), id.algorithm(), id.size())
	}
	return nil
}
