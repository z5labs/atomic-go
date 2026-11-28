package legacy_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/project/legacy"
)

func TestParseState(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want legacy.State
	}{
		{name: "three tokens", body: "42 ABC123 DEF456", want: legacy.State{Position: 42, Merkle: "ABC123", TagMerkle: "DEF456"}},
		{name: "with a SetId", body: "42 ABC DEF SID32", want: legacy.State{Position: 42, Merkle: "ABC", TagMerkle: "DEF", SetID: "SID32"}},
		{name: "fifth token ignored", body: "42 A B S X", want: legacy.State{Position: 42, Merkle: "A", TagMerkle: "B", SetID: "S"}},
		{name: "empty view", body: "-", want: legacy.State{Empty: true}},
		{name: "empty view padded", body: " -\n", want: legacy.State{Empty: true}},
		{name: "whole body, newlines included", body: "\n7\nM\r\n\tT\n", want: legacy.State{Position: 7, Merkle: "M", TagMerkle: "T"}},
		{name: "a leading plus, as Rust's u64 parse", body: "+3 M T", want: legacy.State{Position: 3, Merkle: "M", TagMerkle: "T"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := legacy.ParseState(tc.body)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseStateRefuses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		msg      string
		sentinel error
	}{
		{name: "empty", body: "", msg: "empty state response", sentinel: legacy.ErrEmpty},
		{name: "blank", body: " \n\t", msg: "empty state response", sentinel: legacy.ErrEmpty},
		{name: "two tokens", body: " 42 ABC\n", msg: "invalid state format: '42 ABC'", sentinel: legacy.ErrInvalidFormat},
		{name: "bad position", body: "x ABC DEF", msg: "invalid position: 'x'", sentinel: legacy.ErrInvalidNumber},
		{name: "negative position", body: "-1 ABC DEF", msg: "invalid position: '-1'", sentinel: legacy.ErrInvalidNumber},
		{name: "position past u64", body: "18446744073709551616 A B", msg: "invalid position: '18446744073709551616'", sentinel: legacy.ErrInvalidNumber},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := legacy.ParseState(tc.body)
			require.Equal(t, legacy.State{}, got)
			require.EqualError(t, err, tc.msg)
			require.ErrorIs(t, err, tc.sentinel)
		})
	}
}

func TestParseChangelistEntry(t *testing.T) {
	for _, tc := range []struct {
		line string
		want legacy.ChangelistEntry
	}{
		{line: "0.ABC.DEF", want: legacy.ChangelistEntry{Sequence: 0, Hash: "ABC", Merkle: "DEF"}},
		{line: "1.GHI.JKL.", want: legacy.ChangelistEntry{Sequence: 1, Hash: "GHI", Merkle: "JKL", Tagged: true}},
		{line: "1.A.B..", want: legacy.ChangelistEntry{Sequence: 1, Hash: "A", Merkle: "B", Tagged: true}},
		{line: "0.A.B.C", want: legacy.ChangelistEntry{Sequence: 0, Hash: "A", Merkle: "B"}},
		{line: "  42.ABC123.DEF456.\r", want: legacy.ChangelistEntry{Sequence: 42, Hash: "ABC123", Merkle: "DEF456", Tagged: true}},
		{line: "2.A.B.C.", want: legacy.ChangelistEntry{Sequence: 2, Hash: "A", Merkle: "B", Tagged: true}},
	} {
		t.Run(tc.line, func(t *testing.T) {
			got, err := legacy.ParseChangelistEntry(tc.line)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseChangelistEntryRefuses(t *testing.T) {
	for _, tc := range []struct {
		line     string
		msg      string
		sentinel error
	}{
		{line: "", msg: "empty changelist line", sentinel: legacy.ErrEmpty},
		{line: " \t", msg: "empty changelist line", sentinel: legacy.ErrEmpty},
		// Fewer than two dots are left once the trailing one goes, so the
		// original line is split, and its Merkle state is empty.
		{line: "0.ABC.", msg: "invalid changelist format: '0.ABC.'", sentinel: legacy.ErrInvalidFormat},
		{line: "0.ABC", msg: "invalid changelist format: '0.ABC'", sentinel: legacy.ErrInvalidFormat},
		{line: "0..DEF", msg: "invalid changelist format: '0..DEF'", sentinel: legacy.ErrInvalidFormat},
		{line: "x.ABC.DEF", msg: "invalid sequence number: 'x'", sentinel: legacy.ErrInvalidNumber},
		{line: ".ABC.DEF", msg: "invalid sequence number: ''", sentinel: legacy.ErrInvalidNumber},
	} {
		t.Run(tc.line, func(t *testing.T) {
			got, err := legacy.ParseChangelistEntry(tc.line)
			require.Equal(t, legacy.ChangelistEntry{}, got)
			require.EqualError(t, err, tc.msg)
			require.ErrorIs(t, err, tc.sentinel)
		})
	}
}
