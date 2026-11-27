package ident

import (
	"errors"
	"fmt"
	"strings"
)

// MaxViewNameLen is the longest view name, in bytes, that
// [ValidateNewViewName] accepts: the daemon's limit, which is the lower of
// the two front ends' limits.
const MaxViewNameLen = 100

// ErrViewName reports a view name that [ValidateNewViewName] refuses.
var ErrViewName = errors.New("invalid view name")

// viewNameRefused lists the bytes a new view name may not hold. The CLI
// refuses '/', '\\', NUL, ':', '*', '?', '"', '<', '>', '|' and space; the
// daemon refuses '/' and '\\'. Tab, CR and LF are refused as well, though
// neither front end does, because they separate the fields and rows of
// view inventories and manifest headers.
const viewNameRefused = "/\\\x00:*?\"<>| \t\r\n"

// ValidateNewViewName reports whether name may be used for a view this
// program creates. It refuses everything that either of Atomic's front ends,
// the CLI and the daemon, refuses, so a name it accepts is one both accept:
//
//   - the empty name;
//   - a name longer than [MaxViewNameLen] bytes;
//   - a name holding '/', '\\', NUL, ':', '*', '?', '"', '<', '>', '|',
//     space, tab, CR or LF;
//   - "." and "..", a name that starts or ends with '.', and a name that
//     holds ".." anywhere.
//
// It does not refuse '&', '#', '%', '+' or '=', which neither front end
// refuses either; see docs/remote/types.md#view-names.
//
// Use it only on names this program is about to create. A name read from a
// server is never validated, so a remote whose views have names these rules
// would refuse still works.
//
// A refused name is an error wrapping [ErrViewName], so callers can test it
// with [errors.Is].
func ValidateNewViewName(name string) error {
	switch {
	case name == "":
		return viewNameError(name, "is empty")
	case len(name) > MaxViewNameLen:
		return viewNameError(name, fmt.Sprintf("is %d bytes, longer than %d", len(name), MaxViewNameLen))
	case name == "." || name == "..":
		return viewNameError(name, "is a relative path element")
	case strings.HasPrefix(name, "."):
		return viewNameError(name, "starts with '.'")
	case strings.HasSuffix(name, "."):
		return viewNameError(name, "ends with '.'")
	case strings.Contains(name, ".."):
		return viewNameError(name, `contains ".."`)
	}
	if i := strings.IndexAny(name, viewNameRefused); i >= 0 {
		return viewNameError(name, fmt.Sprintf("contains %q at offset %d", name[i], i))
	}
	return nil
}

func viewNameError(name, reason string) error {
	return fmt.Errorf("ident: %w %q: %s", ErrViewName, name, reason)
}
