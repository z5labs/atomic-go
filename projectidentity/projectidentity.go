// Package projectidentity reads, hashes, signs and verifies Atomic's
// project identity documents: the JSON-LD [DocumentType] object that names
// a project lineage by a random 32-byte [ProjectID]
// (docs/remote/auth.md#project-identity).
//
// A [Document] holds the exact bytes it was parsed from, so a document read
// from a remote can be handed back to one unchanged: [Document.Bytes] is
// what the project client sends, whatever this package reads out of it.
// [Document.ProjectID], [Document.Issuer] and [Document.Hash] interpret
// the document; [Document.VerifyProof] checks its signature; and [Sign]
// signs one as Atomic does when a repository is created with a default
// identity.
//
// Nothing here reads the host. [NewProjectID] takes its randomness as an
// [io.Reader] and [Sign] takes its key as an [identity.Signer]. Minting a
// whole new document is out of scope, because the @context Atomic writes
// into one is not documented.
//
// The formats are documented in docs/remote/auth.md#project-identity,
// docs/remote/endpoints/project.md#fetch_project_identity and
// docs/remote/endpoints/project.md#bind_project_identity.
package projectidentity

import "errors"

const (
	// DocumentType is the @type of a project identity document.
	DocumentType = "atomic:ProjectIdentity"

	// HashDomain is the domain separator a project identity's hash input
	// begins with, followed by a zero byte
	// (docs/remote/auth.md#project-identity).
	HashDomain = "atomic.project-identity.v1"
)

// ErrInvalid reports a project id, document or argument that is not what
// this package needs: text that is not a project id, bytes that are not one
// JSON object, a member that cannot be read, or a [Sign] or [NewProjectID]
// call missing what it requires.
var ErrInvalid = errors.New("projectidentity: invalid project identity")
