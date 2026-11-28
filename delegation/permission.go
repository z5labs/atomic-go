package delegation

import (
	"fmt"
	"strings"
)

// Permission is something a delegation can allow its delegate to do. Its
// wire form is snake_case, and only the eight constants below are valid
// (docs/remote/auth.md#delegationpermission).
type Permission string

const (
	// PermissionRead is permission to read repository content.
	PermissionRead Permission = "read"

	// PermissionRecord is permission to record (commit) changes.
	PermissionRecord Permission = "record"

	// PermissionPush is permission to push changes to remotes.
	PermissionPush Permission = "push"

	// PermissionPull is permission to pull changes from remotes.
	PermissionPull Permission = "pull"

	// PermissionManageViews is permission to create and delete views.
	PermissionManageViews Permission = "manage_views"

	// PermissionManageTags is permission to create and delete tags.
	PermissionManageTags Permission = "manage_tags"

	// PermissionAdmin is permission to manage repository settings.
	PermissionAdmin Permission = "admin"

	// PermissionFull is full access: it implies every other permission.
	PermissionFull Permission = "full"
)

// known reports whether p is one of the eight permissions.
func (p Permission) known() bool {
	switch p {
	case PermissionRead, PermissionRecord, PermissionPush, PermissionPull,
		PermissionManageViews, PermissionManageTags, PermissionAdmin, PermissionFull:
		return true
	}
	return false
}

// Implies reports whether holding p grants q (docs/remote/auth.md#scope).
// [PermissionFull] implies every permission. [PermissionAdmin] implies
// [PermissionRead], [PermissionManageViews], [PermissionManageTags] and
// itself, but not record, push or pull. Every other permission implies only
// itself.
//
// A Permission that is not one of the eight constants implies nothing and
// is implied by nothing, not even itself.
func (p Permission) Implies(q Permission) bool {
	if !p.known() || !q.known() {
		return false
	}
	switch p {
	case PermissionFull:
		return true
	case PermissionAdmin:
		switch q {
		case PermissionRead, PermissionManageViews, PermissionManageTags, PermissionAdmin:
			return true
		}
		return false
	}
	return p == q
}

// ParsePermission parses a permission the way Atomic's command line does,
// which is more forgiving than the wire: surrounding white space is
// trimmed, case is ignored, hyphens may stand for underscores, and the old
// name manage_stacks means [PermissionManageViews]. Anything else is an
// error wrapping [ErrUnknownPermission].
//
// Use [Permission.UnmarshalText], which accepts only the wire values, for
// text read from a document.
func ParsePermission(s string) (Permission, error) {
	norm := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "-", "_")
	if norm == "manage_stacks" {
		return PermissionManageViews, nil
	}
	if p := Permission(norm); p.known() {
		return p, nil
	}
	return "", fmt.Errorf("%w %q", ErrUnknownPermission, s)
}

// MarshalText returns p's wire form. It implements [encoding.TextMarshaler],
// and refuses, with an error wrapping [ErrUnknownPermission], anything but
// the eight constants, so a Permission that marshals always unmarshals
// again.
func (p Permission) MarshalText() ([]byte, error) {
	if !p.known() {
		return nil, fmt.Errorf("%w %q", ErrUnknownPermission, string(p))
	}
	return []byte(p), nil
}

// UnmarshalText sets p from one of the eight wire values, exactly as
// written, and refuses anything else, including other cases, hyphens and
// manage_stacks, with an error wrapping [ErrUnknownPermission]. It
// implements [encoding.TextUnmarshaler]. On failure p is unchanged.
func (p *Permission) UnmarshalText(text []byte) error {
	v := Permission(text)
	if !v.known() {
		return fmt.Errorf("%w %q", ErrUnknownPermission, text)
	}
	*p = v
	return nil
}
