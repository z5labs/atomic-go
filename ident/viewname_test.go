package ident_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
)

func TestValidateNewViewName(t *testing.T) {
	// One accepted and one refused name per rule. The parenthesised suffix
	// names the front end the rule comes from; "client" rules are this
	// package's own additions.
	for _, tc := range []struct {
		rule     string
		accepted string
		refused  string
	}{
		{"empty (CLI, daemon)", "a", ""},
		{"daemon maximum of 100 bytes", strings.Repeat("a", 100), strings.Repeat("a", 101)},
		{"CLI maximum of 255 bytes, subsumed by the daemon's 100", strings.Repeat("a", 100), strings.Repeat("a", 256)},
		{"multi-byte length is counted in bytes", strings.Repeat("é", 50), strings.Repeat("é", 51)},
		{"'/' (CLI, daemon)", "feature-x", "feature/x"},
		{"'\\' (CLI, daemon)", "feature-x", `feature\x`},
		{"NUL (CLI)", "feature-x", "feature\x00x"},
		{"':' (CLI)", "feature-x", "feature:x"},
		{"'*' (CLI)", "feature-x", "feature*"},
		{"'?' (CLI)", "feature-x", "feature?"},
		{"'\"' (CLI)", "feature-x", `"feature"`},
		{"'<' (CLI)", "feature-x", "<feature"},
		{"'>' (CLI)", "feature-x", "feature>"},
		{"'|' (CLI)", "feature-x", "feature|x"},
		{"space (CLI)", "feature_x", "feature x"},
		{"tab (client)", "feature_x", "feature\tx"},
		{"CR (client)", "feature_x", "feature\rx"},
		{"LF (client)", "feature_x", "feature\nx"},
		{"'.' alone (CLI)", "a.b", "."},
		{"'..' alone (CLI, daemon)", "a.b", ".."},
		{"leading '.' (CLI)", "a.b", ".hidden"},
		{"trailing '.' (CLI)", "v1.0", "v1."},
		{"'..' anywhere (daemon)", "a.b.c", "a..b"},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			require.NoError(t, ident.ValidateNewViewName(tc.accepted))

			err := ident.ValidateNewViewName(tc.refused)
			require.ErrorIs(t, err, ident.ErrViewName)
		})
	}

	t.Run("accepts what neither front end refuses", func(t *testing.T) {
		for _, name := range []string{"main", "a&b", "a#b", "a%b", "a+b", "a=b", "Feature-X_2", "日本"} {
			require.NoError(t, ident.ValidateNewViewName(name), name)
		}
	})

	t.Run("the error names the view", func(t *testing.T) {
		err := ident.ValidateNewViewName("feature/x")
		require.ErrorContains(t, err, `"feature/x"`)
	})
}
