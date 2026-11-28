package storage_test

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/storage"
	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

// registrationAt is the timestamp docs/remote/auth.md#registration gives as
// its example.
var registrationAt = time.Date(2026, 11, 24, 9, 30, 0, 0, time.UTC)

// signedRegistration returns alice's registration, signed by the RFC 8032
// TEST 1 key at registrationAt, with email as her email.
func signedRegistration(t *testing.T, email string) (identity.Registration, identity.PublicKey) {
	t.Helper()
	seed, err := hex.DecodeString(seed1)
	require.NoError(t, err)
	s, err := identity.SignerFromSeed(seed)
	require.NoError(t, err)
	id := identity.Identity{
		Name:      "alice",
		Type:      identity.TypeUser,
		Email:     email,
		PublicKey: s.PublicKey(),
	}
	reg, err := identity.SignRegistration(t.Context(), id, s, registrationAt)
	require.NoError(t, err)
	return reg, s.PublicKey()
}

// registered is the request a fake registration server saw.
type registered struct {
	calls         int
	method        string
	host          string
	requestURI    string
	header        http.Header
	contentLength int64
	body          []byte
}

// registrar returns a Doer whose requests are answered in memory by h, and
// the record of the last request it saw. The request is the one that went
// over the wire, written by http.Request.Write and read back.
func registrar(t *testing.T, h http.HandlerFunc) (*http.Client, *registered) {
	t.Helper()
	got := &registered{}
	return remotetest.Client(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		*got = registered{
			calls:         got.calls + 1,
			method:        r.Method,
			host:          r.Host,
			requestURI:    r.RequestURI,
			header:        r.Header.Clone(),
			contentLength: r.ContentLength,
			body:          b,
		}
		h(w, r)
	})), got
}

// countingDoer counts the requests it is asked to send, and sends none.
type countingDoer struct{ calls int }

func (d *countingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return nil, errors.New("countingDoer sends nothing")
}

// brokenBody yields its data and then fails with err.
type brokenBody struct {
	r   io.Reader
	err error
}

func (b *brokenBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, b.err
	}
	return n, err
}

func (b *brokenBody) Close() error { return nil }

// asRemoteError returns err as a *remote.Error.
func asRemoteError(t *testing.T, err error) *remote.Error {
	t.Helper()
	require.Error(t, err)
	var rerr *remote.Error
	require.ErrorAs(t, err, &rerr)
	return rerr
}

func TestRegisterSendsTheSignedRegistration(t *testing.T) {
	for name, tc := range map[string]struct {
		email     string
		wantEmail string
	}{
		"with email":    {email: "alice@example.com", wantEmail: `"alice@example.com"`},
		"without email": {email: "", wantEmail: `null`},
	} {
		t.Run(name, func(t *testing.T) {
			reg, key := signedRegistration(t, tc.email)
			d, got := registrar(t, raw(http.StatusCreated, `{}`))

			_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
			require.NoError(t, err)

			require.Equal(t, 1, got.calls)
			require.Equal(t, http.MethodPost, got.method)
			require.Equal(t, "atomic.storage", got.host)
			require.Equal(t, "/register", got.requestURI)
			require.JSONEq(t, `{
				"username": "alice",
				"email": `+tc.wantEmail+`,
				"public_key": "`+key.String()+`",
				"timestamp": "2026-11-24T09:30:00Z",
				"signature": "`+reg.Signature+`"
			}`, string(got.body))

			// The signature on the wire covers the documented four lines.
			sig, err := ident.DecodeBase32Strict(reg.Signature, identity.SignatureSize)
			require.NoError(t, err)
			require.True(t, key.Verify(identity.RegistrationMessage("alice", key.String(), "2026-11-24T09:30:00Z"), sig))
		})
	}
}

func TestRegisterSendsOnlyContentType(t *testing.T) {
	reg, _ := signedRegistration(t, "alice@example.com")
	d, got := registrar(t, raw(http.StatusOK, `{}`))

	_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
	require.NoError(t, err)

	// Content-Length frames the body and Host is the request line's; every
	// other header line on the wire is the request's own.
	require.Equal(t, int64(len(got.body)), got.contentLength)
	header := got.header.Clone()
	header.Del("Content-Length")
	require.Equal(t, http.Header{"Content-Type": {"application/json"}}, header)
}

func TestRegisterTrimsTrailingSlashes(t *testing.T) {
	for server, want := range map[string]string{
		"https://h":         "/register",
		"https://h/":        "/register",
		"https://h//":       "/register",
		"https://h/base/":   "/base/register",
		"http://h:8080/a/b": "/a/b/register",
	} {
		t.Run(server, func(t *testing.T) {
			reg, _ := signedRegistration(t, "")
			d, got := registrar(t, raw(http.StatusOK, `{}`))
			_, err := storage.Register(t.Context(), d, mustURL(t, server), reg)
			require.NoError(t, err)
			require.Equal(t, want, got.requestURI)
		})
	}
}

func TestRegisterReadsTheReply(t *testing.T) {
	const reply = `{"tenant_id": "t-1", "slug": "alice", "base_url": "https://alice.atomic.storage", "mode": "single", "role": "owner"}`
	want := storage.RegisterResponse{
		TenantID: ptr("t-1"),
		Slug:     ptr("alice"),
		BaseURL:  ptr("https://alice.atomic.storage"),
		Mode:     ptr("single"),
		Role:     ptr("owner"),
	}
	for name, body := range map[string]string{
		"enveloped": envelope(reply),
		"bare":      reply,
	} {
		t.Run(name, func(t *testing.T) {
			reg, _ := signedRegistration(t, "alice@example.com")
			d, _ := registrar(t, raw(http.StatusCreated, body))
			got, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.True(t, got.SingleTenant())
		})
	}
}

func TestRegisterKeepsOnlyStrings(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want storage.RegisterResponse
	}{
		"slug 5":                {`{"slug": 5}`, storage.RegisterResponse{}},
		"enveloped slug 5":      {envelope(`{"slug": 5, "mode": "multi"}`), storage.RegisterResponse{Mode: ptr("multi")}},
		"null, bool and object": {`{"tenant_id": null, "slug": true, "base_url": {"u": "x"}, "role": ["owner"]}`, storage.RegisterResponse{}},
		"empty string kept":     {`{"slug": ""}`, storage.RegisterResponse{Slug: ptr("")}},
		"unknown members":       {`{"slug": "alice", "extra": 1}`, storage.RegisterResponse{Slug: ptr("alice")}},
		"last duplicate wins":   {`{"slug": "first", "slug": "second"}`, storage.RegisterResponse{Slug: ptr("second")}},
		// data is read whatever it holds, so a data that is not an
		// object leaves every field nil, even beside top-level members.
		"null data":       {`{"success": true, "data": null, "slug": "alice"}`, storage.RegisterResponse{}},
		"string data":     {`{"data": "alice", "slug": "alice"}`, storage.RegisterResponse{}},
		"array reply":     {`["alice"]`, storage.RegisterResponse{}},
		"string reply":    {`"alice"`, storage.RegisterResponse{}},
		"escaped strings": {`{"slug": "alice"}`, storage.RegisterResponse{Slug: ptr("alice")}},
	} {
		t.Run(name, func(t *testing.T) {
			reg, _ := signedRegistration(t, "")
			d, _ := registrar(t, raw(http.StatusOK, tc.body))
			got, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRegisterResponseSingleTenant(t *testing.T) {
	for name, tc := range map[string]struct {
		mode *string
		want bool
	}{
		"single":  {ptr("single"), true},
		"multi":   {ptr("multi"), false},
		"no mode": {nil, false},
		"Single":  {ptr("Single"), false},
		"empty":   {ptr(""), false},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, storage.RegisterResponse{Mode: tc.mode}.SingleTenant())
		})
	}
}

func TestRegisterErrorMessages(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"top-level message":       {http.StatusBadRequest, `{"code": "BAD_SIGNATURE", "message": "signature does not verify"}`, "signature does not verify"},
		"error.message":           {http.StatusBadRequest, `{"success": false, "error": {"code": "BAD_SIGNATURE", "message": "stale timestamp"}}`, "stale timestamp"},
		"top-level message first": {http.StatusBadRequest, `{"message": "top", "error": {"message": "nested"}}`, "top"},
		"non-string message":      {http.StatusBadRequest, `{"message": 5, "error": {"message": "nested"}}`, "nested"},
		"neither":                 {http.StatusBadGateway, `{"error": "nope"}`, "Server returned 502 Bad Gateway"},
		"plain text":              {http.StatusInternalServerError, `internal error`, "Server returned 500 Internal Server Error"},
		"empty body":              {http.StatusServiceUnavailable, ``, "Server returned 503 Service Unavailable"},
		"unknown status":          {599, `{}`, "Server returned 599 <unknown status code>"},
	} {
		t.Run(name, func(t *testing.T) {
			reg, _ := signedRegistration(t, "")
			d, _ := registrar(t, raw(tc.status, tc.body))
			_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage/"), reg)
			rerr := asRemoteError(t, err)
			require.Equal(t, remote.KindHTTP, rerr.Kind)
			require.Equal(t, "register", rerr.Op)
			require.Equal(t, http.MethodPost, rerr.Method)
			require.Equal(t, "https://atomic.storage/register", rerr.URL)
			require.Equal(t, tc.status, rerr.StatusCode)
			require.Equal(t, tc.want, rerr.Message)
			require.Equal(t, tc.body, rerr.Body)
			require.Equal(t, "HTTP error "+strconv.Itoa(tc.status)+": "+tc.want, rerr.Error())
		})
	}
}

func TestRegisterAuthStatusIsHTTP(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			reg, _ := signedRegistration(t, "")
			d, _ := registrar(t, raw(status, `{"message": "no"}`))
			_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
			require.Equal(t, remote.KindHTTP, asRemoteError(t, err).Kind)
		})
	}
}

func TestRegisterConflict(t *testing.T) {
	reg, _ := signedRegistration(t, "alice@example.com")
	d, _ := registrar(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, storagetest.Fail(w, http.StatusConflict, "CONFLICT", "public key already registered"))
	})
	_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
	require.ErrorIs(t, err, remote.ErrConflict)
	rerr := asRemoteError(t, err)
	require.Equal(t, remote.KindHTTP, rerr.Kind)
	require.Equal(t, "public key already registered", rerr.Message)
}

func TestRegisterIgnoresTheMinimumVersion(t *testing.T) {
	for name, status := range map[string]int{"success": http.StatusOK, "failure": http.StatusInternalServerError} {
		t.Run(name, func(t *testing.T) {
			reg, _ := signedRegistration(t, "")
			d, _ := registrar(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(remote.MinVersionHeader, "999.0.0")
				storagetest.Raw(w, status, `{"slug": "alice"}`)
			})
			got, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
			if status == http.StatusOK {
				require.NoError(t, err)
				require.Equal(t, storage.RegisterResponse{Slug: ptr("alice")}, got)
				return
			}
			// The failure is the status's, not a version mismatch.
			require.Equal(t, remote.KindHTTP, asRemoteError(t, err).Kind)
		})
	}
}

func TestRegisterRefusesArgumentsBeforeSending(t *testing.T) {
	reg, _ := signedRegistration(t, "")

	t.Run("nil Doer and server", func(t *testing.T) {
		_, err := storage.Register(t.Context(), nil, nil, reg)
		require.ErrorIs(t, err, storage.ErrInvalidArgument)
		require.ErrorContains(t, err, "the Doer is required")
		require.ErrorContains(t, err, "the server URL is required")
		var joined interface{ Unwrap() []error }
		require.ErrorAs(t, err, &joined)
		require.Len(t, joined.Unwrap(), 2)
	})

	for server, want := range map[string]string{
		"atomic.storage":             "the server URL is not absolute",
		"/register":                  "the server URL is not absolute",
		"ftp://atomic.storage":       `the server URL has scheme "ftp", not http or https`,
		"https://":                   "the server URL has no host",
		"https://u:p@atomic.storage": "the server URL has userinfo",
		"https://atomic.storage?x=1": "the server URL has a query",
		"https://atomic.storage#f":   "the server URL has a fragment",
	} {
		t.Run(server, func(t *testing.T) {
			d := &countingDoer{}
			_, err := storage.Register(t.Context(), d, mustURL(t, server), reg)
			require.ErrorIs(t, err, storage.ErrInvalidArgument)
			require.ErrorContains(t, err, want)
			require.Zero(t, d.calls)
		})
	}

	t.Run("nil Doer and a bad server", func(t *testing.T) {
		_, err := storage.Register(t.Context(), nil, mustURL(t, "ftp://u@h?q"), reg)
		require.ErrorIs(t, err, storage.ErrInvalidArgument)
		for _, want := range []string{"the Doer is required", "scheme", "userinfo", "query"} {
			require.ErrorContains(t, err, want)
		}
	})
}

func TestRegisterBodyFailingMidReadIsConnectionFailed(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusConflict} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			cause := errors.New("connection reset")
			d := &http.Client{Transport: remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{},
					Body:       &brokenBody{r: strings.NewReader(`{"slug": "al`), err: cause},
					Request:    req,
				}, nil
			})}
			reg, _ := signedRegistration(t, "")
			_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
			rerr := asRemoteError(t, err)
			require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
			require.Equal(t, status, rerr.StatusCode)
			require.ErrorIs(t, err, cause)
			require.True(t, rerr.Retryable())
		})
	}
}

func TestRegisterSendFailureIsConnectionFailed(t *testing.T) {
	cause := errors.New("connection refused")
	d := &http.Client{Transport: remotetest.RoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	})}
	reg, _ := signedRegistration(t, "")
	_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage/"), reg)
	rerr := asRemoteError(t, err)
	require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "Failed to connect to remote: https://atomic.storage/register", rerr.Error())
}

func TestRegisterCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	d := &countingDoer{}
	reg, _ := signedRegistration(t, "")
	_, err := storage.Register(ctx, d, mustURL(t, "https://atomic.storage"), reg)
	rerr := asRemoteError(t, err)
	require.Equal(t, remote.KindConnectionFailed, rerr.Kind)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, rerr.Retryable())
	require.Zero(t, d.calls)
}

func TestRegisterUndecodableReplyIsProtocol(t *testing.T) {
	for name, body := range map[string]string{
		"plain text":     `registered`,
		"truncated JSON": `{"slug": "al`,
		"empty":          ``,
		"two values":     `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			reg, _ := signedRegistration(t, "")
			d, _ := registrar(t, raw(http.StatusOK, body))
			_, err := storage.Register(t.Context(), d, mustURL(t, "https://atomic.storage"), reg)
			rerr := asRemoteError(t, err)
			require.Equal(t, remote.KindProtocol, rerr.Kind)
			require.Equal(t, http.StatusOK, rerr.StatusCode)
			require.Contains(t, rerr.Message, "invalid server response")
		})
	}
}
