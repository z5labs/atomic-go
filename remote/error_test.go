package remote_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote"
)

func TestKindString(t *testing.T) {
	for _, tc := range []struct {
		kind remote.Kind
		want string
	}{
		{remote.KindConnectionFailed, "ConnectionFailed"},
		{remote.KindAuthenticationFailed, "AuthenticationFailed"},
		{remote.KindRepositoryNotFound, "RepositoryNotFound"},
		{remote.KindViewNotFound, "ViewNotFound"},
		{remote.KindChangeNotFound, "ChangeNotFound"},
		{remote.KindTagNotFound, "TagNotFound"},
		{remote.KindStateMismatch, "StateMismatch"},
		{remote.KindMissingDependencies, "MissingDependencies"},
		{remote.KindProtocol, "ProtocolError"},
		{remote.KindHTTP, "HttpError"},
		{remote.KindVersionMismatch, "VersionMismatch"},
		{remote.KindOther, "Other"},
		{remote.Kind(0), "Kind(0)"},
		{remote.Kind(99), "Kind(99)"},
		{remote.Kind(-1), "Kind(-1)"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			require.Equal(t, tc.want, tc.kind.String())
		})
	}

	t.Run("KindConnectionFailed is 1", func(t *testing.T) {
		require.Equal(t, remote.Kind(1), remote.KindConnectionFailed)
	})
}

func TestConstants(t *testing.T) {
	require.Equal(t, "X-Atomic-Min-Version", remote.MinVersionHeader)
	require.Equal(t, 1<<20, remote.MaxErrorBody)
}

func TestErrorText(t *testing.T) {
	const u = "https://example.test/acme/widgets/.atomic?state=main"
	cause := errors.New("dial tcp: connection refused")

	for _, tc := range []struct {
		name string
		err  *remote.Error
		want string
	}{
		{
			"ConnectionFailed",
			&remote.Error{Kind: remote.KindConnectionFailed, URL: u, Err: cause},
			"Failed to connect to remote: " + u,
		},
		{
			"AuthenticationFailed",
			&remote.Error{Kind: remote.KindAuthenticationFailed, URL: u, StatusCode: 401, Message: "bad token"},
			"Authentication failed for " + u + ": bad token",
		},
		{
			"RepositoryNotFound",
			&remote.Error{Kind: remote.KindRepositoryNotFound, URL: u, StatusCode: 404},
			"Repository not found: " + u,
		},
		{
			"ViewNotFound",
			&remote.Error{Kind: remote.KindViewNotFound, Subject: "feature-x", StatusCode: 404},
			"View not found: feature-x",
		},
		{
			"ChangeNotFound",
			&remote.Error{Kind: remote.KindChangeNotFound, Subject: "ABCDEF", StatusCode: 404},
			"Change not found: ABCDEF",
		},
		{
			"TagNotFound",
			&remote.Error{Kind: remote.KindTagNotFound, Subject: "STATE1", StatusCode: 404},
			"Tag not found for state: STATE1",
		},
		{
			"StateMismatch",
			&remote.Error{Kind: remote.KindStateMismatch, Subject: "STATE2", StatusCode: 400},
			"State mismatch: remote is at unknown, cannot tag STATE2",
		},
		{
			"MissingDependencies with two",
			&remote.Error{Kind: remote.KindMissingDependencies, MissingHashes: []string{"ABC", "DEF"}},
			"Missing 2 dependencies on remote: ABC, DEF",
		},
		{
			"MissingDependencies with five",
			&remote.Error{Kind: remote.KindMissingDependencies, MissingHashes: []string{"A", "B", "C", "D", "E"}},
			"Missing 5 dependencies on remote: A, B, C, ... and 2 more",
		},
		{
			"MissingDependencies with three",
			&remote.Error{Kind: remote.KindMissingDependencies, MissingHashes: []string{"A", "B", "C"}},
			"Missing 3 dependencies on remote: A, B, C",
		},
		{
			"MissingDependencies with four",
			&remote.Error{Kind: remote.KindMissingDependencies, MissingHashes: []string{"A", "B", "C", "D"}},
			"Missing 4 dependencies on remote: A, B, C, ... and 1 more",
		},
		{
			"MissingDependencies with none",
			&remote.Error{Kind: remote.KindMissingDependencies},
			"Missing 0 dependencies on remote: (none)",
		},
		{
			"ProtocolError",
			&remote.Error{Kind: remote.KindProtocol, Message: "invalid state line", Err: cause},
			"Protocol error: invalid state line",
		},
		{
			"HttpError",
			&remote.Error{Kind: remote.KindHTTP, StatusCode: 500, Message: "Internal Server Error"},
			"HTTP error 500: Internal Server Error",
		},
		{
			"VersionMismatch with the server's min-version",
			&remote.Error{Kind: remote.KindVersionMismatch, ClientVersion: "0.17.0", ServerMinVersion: "0.16.2", Err: cause},
			"Failed to decode data from remote — the client and server may be running incompatible versions.\n" +
				"  Client version: 0.17.0\n" +
				"  Server min-version: 0.16.2\n" +
				"  Hint: run 'atomic update' to upgrade, or ask the storage owner for their server version.",
		},
		{
			"VersionMismatch without the server's min-version",
			&remote.Error{Kind: remote.KindVersionMismatch, ClientVersion: "0.17.0", Err: cause},
			"Failed to decode data from remote — the client and server may be running incompatible versions.\n" +
				"  Client version: 0.17.0\n" +
				"  Hint: run 'atomic update' to upgrade, or ask the storage owner for their server version.",
		},
		{
			"Other",
			&remote.Error{Kind: remote.KindOther, Message: "failed to open file", Err: cause},
			"failed to open file",
		},
		{
			"unknown kind with a message",
			&remote.Error{Kind: remote.Kind(0), Message: "something broke"},
			"something broke",
		},
		{
			"unknown kind without a message",
			&remote.Error{Kind: remote.Kind(42)},
			"remote: Kind(42)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.err.Error())
			if tc.err.Err != nil {
				require.NotContains(t, tc.err.Error(), tc.err.Err.Error())
			}
		})
	}
}

func TestErrorIs(t *testing.T) {
	sentinels := []error{remote.ErrUnauthorized, remote.ErrForbidden, remote.ErrNotFound, remote.ErrConflict}

	for _, tc := range []struct {
		name  string
		err   *remote.Error
		match []error
	}{
		{"HTTP 401", &remote.Error{Kind: remote.KindHTTP, StatusCode: 401}, []error{remote.ErrUnauthorized}},
		{"HTTP 403", &remote.Error{Kind: remote.KindHTTP, StatusCode: 403}, []error{remote.ErrForbidden}},
		{"HTTP 404", &remote.Error{Kind: remote.KindHTTP, StatusCode: 404}, []error{remote.ErrNotFound}},
		{"HTTP 409", &remote.Error{Kind: remote.KindHTTP, StatusCode: 409}, []error{remote.ErrConflict}},
		{"HTTP 500", &remote.Error{Kind: remote.KindHTTP, StatusCode: 500}, nil},
		{"AuthenticationFailed 401", &remote.Error{Kind: remote.KindAuthenticationFailed, StatusCode: 401}, []error{remote.ErrUnauthorized}},
		{"AuthenticationFailed 403", &remote.Error{Kind: remote.KindAuthenticationFailed, StatusCode: 403}, []error{remote.ErrForbidden}},
		{"Protocol 409", &remote.Error{Kind: remote.KindProtocol, StatusCode: 409}, []error{remote.ErrConflict}},
		{"Protocol 404", &remote.Error{Kind: remote.KindProtocol, StatusCode: 404}, []error{remote.ErrNotFound}},
		{"RepositoryNotFound", &remote.Error{Kind: remote.KindRepositoryNotFound, StatusCode: 404}, []error{remote.ErrNotFound}},
		{"ViewNotFound without a status", &remote.Error{Kind: remote.KindViewNotFound}, []error{remote.ErrNotFound}},
		{"ChangeNotFound without a status", &remote.Error{Kind: remote.KindChangeNotFound}, []error{remote.ErrNotFound}},
		{"TagNotFound without a status", &remote.Error{Kind: remote.KindTagNotFound}, []error{remote.ErrNotFound}},
		{"StateMismatch 400", &remote.Error{Kind: remote.KindStateMismatch, StatusCode: 400}, nil},
		{"MissingDependencies", &remote.Error{Kind: remote.KindMissingDependencies, StatusCode: 400}, nil},
		{"ConnectionFailed", &remote.Error{Kind: remote.KindConnectionFailed}, nil},
		{"VersionMismatch", &remote.Error{Kind: remote.KindVersionMismatch, StatusCode: 200}, nil},
		{"Other", &remote.Error{Kind: remote.KindOther}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Wrapped once more, as a caller's error would be.
			var err error = fmt.Errorf("push: %w", tc.err)
			for _, s := range sentinels {
				require.Equal(t, slices.Contains(tc.match, s), errors.Is(err, s), "errors.Is(%v, %v)", tc.err, s)
			}
		})
	}

	t.Run("unrelated targets are left to the cause", func(t *testing.T) {
		err := &remote.Error{Kind: remote.KindHTTP, StatusCode: 404}
		require.False(t, errors.Is(err, io.EOF))
		require.False(t, errors.Is(err, context.Canceled))
	})

	t.Run("Unwrap reaches the context's errors", func(t *testing.T) {
		for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
			cause := &url.Error{Op: "Get", URL: "https://example.test", Err: ctxErr}
			var err error = &remote.Error{Kind: remote.KindConnectionFailed, Err: cause, ContextDone: true}
			require.ErrorIs(t, err, ctxErr)
			require.Same(t, cause, errors.Unwrap(err))

			var urlErr *url.Error
			require.ErrorAs(t, err, &urlErr)
		}
	})

	t.Run("errors.As finds the Error", func(t *testing.T) {
		var err error = fmt.Errorf("pull: %w", &remote.Error{Kind: remote.KindViewNotFound, Subject: "main"})
		var re *remote.Error
		require.ErrorAs(t, err, &re)
		require.Equal(t, remote.KindViewNotFound, re.Kind)
	})
}

func TestErrorRetryable(t *testing.T) {
	// An http.Client Timeout error matches context.DeadlineExceeded through
	// errors.Is, yet a timeout is a retryable ConnectionFailed in Rust.
	timeout := &url.Error{Op: "Get", URL: "https://example.test", Err: fmt.Errorf("client timeout: %w", context.DeadlineExceeded)}
	require.ErrorIs(t, timeout, context.DeadlineExceeded)

	t.Run("ConnectionFailed whose context had not ended", func(t *testing.T) {
		err := &remote.Error{Kind: remote.KindConnectionFailed, Err: timeout}
		require.True(t, err.Retryable())
	})

	t.Run("the same error whose context had ended", func(t *testing.T) {
		err := &remote.Error{Kind: remote.KindConnectionFailed, Err: timeout, ContextDone: true}
		require.False(t, err.Retryable())
	})

	t.Run("ConnectionFailed without a cause", func(t *testing.T) {
		err := &remote.Error{Kind: remote.KindConnectionFailed}
		require.True(t, err.Retryable())
	})

	t.Run("no other kind", func(t *testing.T) {
		for k := remote.KindAuthenticationFailed; k <= remote.KindOther; k++ {
			err := &remote.Error{Kind: k, StatusCode: 503, Err: timeout}
			require.False(t, err.Retryable(), k.String())
		}
	})
}

func TestErrorIsAuth(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *remote.Error
		want bool
	}{
		{"AuthenticationFailed", &remote.Error{Kind: remote.KindAuthenticationFailed}, true},
		{"HTTP 401", &remote.Error{Kind: remote.KindHTTP, StatusCode: 401}, true},
		{"HTTP 403", &remote.Error{Kind: remote.KindHTTP, StatusCode: 403}, true},
		{"Other 401", &remote.Error{Kind: remote.KindOther, StatusCode: 401}, true},
		{"HTTP 404", &remote.Error{Kind: remote.KindHTTP, StatusCode: 404}, false},
		{"RepositoryNotFound", &remote.Error{Kind: remote.KindRepositoryNotFound, StatusCode: 404}, false},
		{"ConnectionFailed", &remote.Error{Kind: remote.KindConnectionFailed}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.err.IsAuth())
		})
	}
}

func TestErrorIsNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *remote.Error
		want bool
	}{
		{"RepositoryNotFound", &remote.Error{Kind: remote.KindRepositoryNotFound}, true},
		{"ViewNotFound", &remote.Error{Kind: remote.KindViewNotFound}, true},
		{"ChangeNotFound", &remote.Error{Kind: remote.KindChangeNotFound}, true},
		{"TagNotFound", &remote.Error{Kind: remote.KindTagNotFound}, true},
		{"HTTP 404", &remote.Error{Kind: remote.KindHTTP, StatusCode: 404}, true},
		{"HTTP 410", &remote.Error{Kind: remote.KindHTTP, StatusCode: 410}, false},
		{"AuthenticationFailed 403", &remote.Error{Kind: remote.KindAuthenticationFailed, StatusCode: 403}, false},
		{"StateMismatch", &remote.Error{Kind: remote.KindStateMismatch}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.err.IsNotFound())
			require.Equal(t, tc.want, errors.Is(tc.err, remote.ErrNotFound))
		})
	}
}
