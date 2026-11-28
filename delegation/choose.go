package delegation

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/z5labs/atomic-go/identity"
)

// Candidates are the certificates an agent could present to a server, as
// [Choose] reads them (docs/remote/auth.md#choosing-a-certificate).
type Candidates struct {
	// Agent is the agent's identity id. Only a certificate whose delegate
	// is Agent's did:atomic is ever chosen.
	Agent identity.ID

	// Server is the URL of the server the certificate will be presented
	// to, such as "https://atomic.storage". A stored certificate is chosen
	// only if it allows [PermissionRead] on it. "" leaves the server
	// unchecked, so any stored certificate that grants read will do.
	Server string

	// Explicit is an Atomic-Delegation header value handed to the agent out
	// of band, as Atomic's ATOMIC_DELEGATION environment variable carries
	// one. When it is not blank after trimming white space, it is the only
	// certificate considered.
	Explicit string

	// Stored are the certificates held for the agent, in any order. Those
	// naming another delegate, and those that do not verify, are ignored.
	Stored []Certificate
}

// SelectionReason says why [Choose] found no certificate to present.
type SelectionReason int

const (
	// ExplicitUnusable is an explicit certificate that does not decode,
	// does not verify, names another delegate or has expired.
	ExplicitUnusable SelectionReason = iota + 1

	// NoCertificates is an agent with no stored certificate that names it
	// and verifies.
	NoCertificates

	// NewestRevoked is a newest stored certificate that is revoked.
	NewestRevoked

	// NewestExpired is a newest stored certificate that has expired and is
	// not revoked.
	NewestExpired

	// NotValidForServer is a newest stored certificate that is neither
	// revoked nor expired but does not allow [PermissionRead] on the server:
	// the server is not in its scope, or nothing it grants implies read.
	NotValidForServer
)

// String returns the reason's name, such as "NewestExpired".
func (r SelectionReason) String() string {
	switch r {
	case ExplicitUnusable:
		return "ExplicitUnusable"
	case NoCertificates:
		return "NoCertificates"
	case NewestRevoked:
		return "NewestRevoked"
	case NewestExpired:
		return "NewestExpired"
	case NotValidForServer:
		return "NotValidForServer"
	}
	return fmt.Sprintf("SelectionReason(%d)", int(r))
}

// SelectionError is the error [Choose] returns when no certificate can be
// presented. It wraps [ErrNoUsableCertificate] and Err.
type SelectionError struct {
	// Reason is why no certificate was chosen. When stored certificates
	// exist but none is usable, it is judged from the newest of them alone.
	Reason SelectionReason

	// Server is [Candidates.Server].
	Server string

	// Servers is the newest stored certificate's server scope when Reason
	// is [NotValidForServer], and nil otherwise. It is empty when that
	// certificate is not restricted to any server, which means it grants
	// nothing that implies read.
	Servers []string

	// Err is the underlying failure, or nil. For [ExplicitUnusable] it is
	// the decode or verify error, or an error wrapping [ErrWrongDelegate] or
	// [ErrExpired]; for [NewestExpired] it is [ErrExpired].
	Err error
}

// Error describes the failure. It never quotes the explicit header value.
func (e *SelectionError) Error() string {
	var b strings.Builder
	b.WriteString(ErrNoUsableCertificate.Error())
	b.WriteString(": ")
	switch e.Reason {
	case ExplicitUnusable:
		b.WriteString("the explicit certificate is unusable")
	case NoCertificates:
		b.WriteString("no stored certificate names this agent")
	case NewestRevoked:
		b.WriteString("the newest certificate has been revoked")
	case NewestExpired:
		b.WriteString("the newest certificate has expired")
	case NotValidForServer:
		b.WriteString("the newest certificate does not allow read")
		if e.Server != "" {
			fmt.Fprintf(&b, " on %s", e.Server)
		}
		if e.scopedToServer() {
			b.WriteString("; it grants nothing that implies read")
		} else {
			fmt.Fprintf(&b, "; it is scoped to %s", strings.Join(e.Servers, ", "))
		}
	default:
		b.WriteString(e.Reason.String())
	}
	// The bare ErrExpired adds nothing to "has expired"; anything else,
	// including an ErrExpired wrapped with the time, is worth saying.
	if e.Err != nil && e.Err != ErrExpired {
		fmt.Fprintf(&b, ": %v", e.Err)
	}
	return b.String()
}

// scopedToServer reports whether the server scope in e lets Server through:
// it is empty, Server is "", or Server is listed. A certificate scoped to
// the server that still does not allow read grants nothing that implies
// read.
func (e *SelectionError) scopedToServer() bool {
	if len(e.Servers) == 0 || e.Server == "" {
		return true
	}
	want := NormalizeServer(e.Server)
	return slices.ContainsFunc(e.Servers, func(s string) bool {
		return NormalizeServer(s) == want
	})
}

// Unwrap returns [ErrNoUsableCertificate] and, when it is set, Err.
func (e *SelectionError) Unwrap() []error {
	if e.Err == nil {
		return []error{ErrNoUsableCertificate}
	}
	return []error{ErrNoUsableCertificate, e.Err}
}

// Choose picks the certificate an agent presents to a server, as Atomic's
// active_for does (docs/remote/auth.md#choosing-a-certificate). revoked
// reports whether a certificate is revoked as far as this machine knows. It
// is required: a nil revoked is an error, never "nothing is revoked".
//
// When c.Explicit is not blank after trimming white space, it must hold a
// certificate that decodes ([DecodeHeader]), verifies against its own
// delegatorKey ([Certificate.VerifySelfContained]), names c.Agent as its
// delegate and has not expired at now. If any of those fails, Choose fails
// with [ExplicitUnusable]; it never falls back to c.Stored, since the
// caller asked for that certificate. For parity with Atomic, an explicit
// certificate is not checked against revoked, nor against its scope: it is
// chosen even when it is scoped to another server or grants nothing that
// implies read. Whether that should change is an open question
// (docs/remote/auth.md#open-questions).
//
// Otherwise Choose keeps the stored certificates that name c.Agent and
// verify, newest Issued first (certificates issued at the same time keep
// their order in c.Stored), and returns the first that is not revoked, has
// not expired at now, and allows [PermissionRead] on c.Server. When none
// qualifies, the newest decides the reason: [NewestRevoked],
// [NewestExpired], or [NotValidForServer], in that order of precedence.
// With none at all the reason is [NoCertificates].
//
// Apart from a nil revoked, which is a programming error and wraps no
// sentinel, every failure is a *[SelectionError] wrapping
// [ErrNoUsableCertificate].
// A certificate Choose returns is worth sending, not certain to be
// accepted: the server re-checks it, and knows of revocations this machine
// does not.
func Choose(c Candidates, now time.Time, revoked func(ID) bool) (Certificate, error) {
	if revoked == nil {
		return Certificate{}, errors.New("delegation: Choose needs a revoked func")
	}
	agent := c.Agent.DID()
	if explicit := strings.TrimSpace(c.Explicit); explicit != "" {
		cert, err := explicitCertificate(explicit, agent, now)
		if err != nil {
			return Certificate{}, &SelectionError{Reason: ExplicitUnusable, Server: c.Server, Err: err}
		}
		return cert, nil
	}

	var mine []Certificate
	for _, cert := range c.Stored {
		if cert.view.Delegate == agent && cert.VerifySelfContained() == nil {
			mine = append(mine, cert)
		}
	}
	if len(mine) == 0 {
		return Certificate{}, &SelectionError{Reason: NoCertificates, Server: c.Server}
	}
	slices.SortStableFunc(mine, func(a, b Certificate) int {
		return b.view.Issued.Compare(a.view.Issued)
	})

	read := ResourceRef{Server: c.Server}
	for _, cert := range mine {
		if !revoked(cert.ID()) && cert.Allows(PermissionRead, read, now) {
			return cert, nil
		}
	}

	newest := mine[0]
	switch {
	case revoked(newest.ID()):
		return Certificate{}, &SelectionError{Reason: NewestRevoked, Server: c.Server}
	case newest.Expired(now):
		return Certificate{}, &SelectionError{Reason: NewestExpired, Server: c.Server, Err: ErrExpired}
	}
	return Certificate{}, &SelectionError{
		Reason:  NotValidForServer,
		Server:  c.Server,
		Servers: slices.Clone(newest.view.Scope.Servers),
	}
}

// explicitCertificate decodes and checks an explicit certificate for
// [Choose].
func explicitCertificate(value string, agent identity.DID, now time.Time) (Certificate, error) {
	cert, err := DecodeHeader(value)
	if err != nil {
		return Certificate{}, err
	}
	if err := cert.VerifySelfContained(); err != nil {
		return Certificate{}, err
	}
	if cert.view.Delegate != agent {
		return Certificate{}, fmt.Errorf("%w: it was issued to %s (%q), not to %s",
			ErrWrongDelegate, cert.view.Delegate, cert.view.DelegateName, agent)
	}
	if cert.Expired(now) {
		return Certificate{}, fmt.Errorf("%w at %s", ErrExpired, cert.view.Expires.Format(time.RFC3339))
	}
	return cert, nil
}

// PermissionError is the error [CheckPermission] returns when a certificate
// does not allow an action. It wraps [ErrNotPermitted].
type PermissionError struct {
	// Identity is the agent's name, as given to [CheckPermission].
	Identity string

	// Permission is the permission asked for.
	Permission Permission

	// Resource is the resource it was asked for on.
	Resource ResourceRef

	// Granted are the permissions the certificate's scope lists.
	Granted []Permission

	// Projects is the certificate's project scope when Resource.Project is
	// outside it, and nil otherwise.
	Projects []string

	// Expired reports whether the certificate had expired.
	Expired bool
}

// Error describes what was refused and what the certificate grants.
func (e *PermissionError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: agent %q may not %s", ErrNotPermitted.Error(), e.Identity, e.Permission)
	if e.Resource.Project != "" {
		fmt.Fprintf(&b, " on project %s", e.Resource.Project)
	}
	granted := make([]string, len(e.Granted))
	for i, p := range e.Granted {
		granted[i] = string(p)
	}
	if len(granted) == 0 {
		b.WriteString("; granted: nothing")
	} else {
		fmt.Fprintf(&b, "; granted: %s", strings.Join(granted, ", "))
	}
	if e.Projects != nil {
		fmt.Fprintf(&b, "; scoped to projects: %s", strings.Join(e.Projects, ", "))
	}
	if e.Expired {
		b.WriteString("; the certificate has expired")
	}
	if e.Projects == nil && !e.Expired && slices.ContainsFunc(e.Granted, func(g Permission) bool {
		return g.Implies(e.Permission)
	}) {
		fmt.Fprintf(&b, "; %s is granted, but the certificate's scope excludes this server, workspace or view", e.Permission)
	}
	return b.String()
}

// Unwrap returns [ErrNotPermitted].
func (e *PermissionError) Unwrap() error {
	return ErrNotPermitted
}

// Remedy returns the command that would issue a certificate granting the
// refused permission, such as
// "atomic identity delegate alice+claude --can push", as Atomic's
// check_permission suggests whatever the refusal was. A new certificate
// also replaces an expired one, but when the permission is already granted
// and the resource is outside the scope, the certificate has to be issued
// with a wider scope as well.
func (e *PermissionError) Remedy() string {
	return fmt.Sprintf("atomic identity delegate %s --can %s", e.Identity, e.Permission)
}

// CheckPermission pre-flights an action, as Atomic's check_permission does
// (docs/remote/auth.md#check_permission): it returns nil when c allows p on
// r at now ([Certificate.Allows]), which covers expiry and scope, and
// otherwise a *[PermissionError] for identityName, the agent's name, that
// wraps [ErrNotPermitted].
//
// It is a local check only and sends nothing. Like [Certificate.Allows] it
// checks neither c's signature nor its revocation; pass it a certificate
// [Choose] returned. The server re-checks everything.
func CheckPermission(c Certificate, p Permission, r ResourceRef, now time.Time, identityName string) error {
	if c.Allows(p, r, now) {
		return nil
	}
	scope := c.view.Scope
	e := &PermissionError{
		Identity:   identityName,
		Permission: p,
		Resource:   r,
		Granted:    slices.Clone(scope.Permissions),
		Expired:    c.Expired(now),
	}
	if r.Project != "" && !matchesAny(scope.Projects, r.Project) {
		e.Projects = slices.Clone(scope.Projects)
	}
	return e
}
