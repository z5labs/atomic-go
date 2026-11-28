package auth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/auth"
	"github.com/z5labs/atomic-go/remote/remotetest"
)

const (
	testToken = "Bearer test-token"
	testCert  = "test-certificate"
)

// fakeCreds sets Authorization and Atomic-Delegation, or fails with err.
type fakeCreds struct {
	err error
}

func (c fakeCreds) Attach(req *http.Request) error {
	if c.err != nil {
		return c.err
	}
	req.Header.Set("Authorization", testToken)
	req.Header.Set("Atomic-Delegation", testCert)
	return nil
}

// sent is what the recording base transport saw of one request.
type sent struct {
	url    string
	header http.Header
}

// recorder is a base transport that records every request it is given and
// answers each with respond, or with 200 and an empty body.
type recorder struct {
	mu      sync.Mutex
	sent    []sent
	respond func(*http.Request) *http.Response
}

func (r *recorder) transport() http.RoundTripper {
	return remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		r.mu.Lock()
		r.sent = append(r.sent, sent{url: req.URL.String(), header: req.Header.Clone()})
		r.mu.Unlock()
		if req.Body != nil {
			_ = req.Body.Close()
		}
		if r.respond != nil {
			return r.respond(req), nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
	})
}

func (r *recorder) requests() []sent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sent(nil), r.sent...)
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func newTransport(t *testing.T, base http.RoundTripper, creds auth.Credentials, origin string) *auth.Transport {
	t.Helper()
	tr, err := auth.NewTransport(base, creds, mustURL(t, origin))
	require.NoError(t, err)
	return tr
}

func roundTrip(t *testing.T, tr http.RoundTripper, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "atomic-go-test")
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return req
}

func TestTransportAttachesOnlyOnItsOrigin(t *testing.T) {
	testCases := []struct {
		name     string
		origin   string
		url      string
		attached bool
	}{
		{name: "same origin", origin: "https://atomic.example", url: "https://atomic.example/p/proj", attached: true},
		{name: "implicit and explicit default port", origin: "https://atomic.example", url: "https://atomic.example:443/p", attached: true},
		{name: "explicit and implicit default port", origin: "https://atomic.example:443", url: "https://atomic.example/p", attached: true},
		{name: "host case is ignored", origin: "https://Atomic.Example/some/path", url: "https://ATOMIC.example/p", attached: true},
		{name: "scheme case is ignored", origin: "HTTPS://atomic.example", url: "https://atomic.example/p", attached: true},
		{name: "explicit http port", origin: "http://localhost:8080", url: "http://localhost:8080/p", attached: true},
		{name: "another host", origin: "https://atomic.example", url: "https://evil.example/p", attached: false},
		{name: "a subdomain", origin: "https://atomic.example", url: "https://org.atomic.example/p", attached: false},
		{name: "another port", origin: "https://atomic.example", url: "https://atomic.example:8443/p", attached: false},
		{name: "another scheme", origin: "https://atomic.example", url: "http://atomic.example/p", attached: false},
		{name: "http default port is not https's", origin: "http://atomic.example", url: "http://atomic.example:443/p", attached: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			tr := newTransport(t, rec.transport(), fakeCreds{}, tc.origin)

			req := roundTrip(t, tr, tc.url)

			got := rec.requests()
			require.Len(t, got, 1)
			require.Equal(t, tc.url, got[0].url)
			require.Equal(t, "atomic-go-test", got[0].header.Get("User-Agent"))
			if tc.attached {
				require.Equal(t, testToken, got[0].header.Get("Authorization"))
				require.Equal(t, testCert, got[0].header.Get("Atomic-Delegation"))
			} else {
				require.NotContains(t, got[0].header, "Authorization")
				require.NotContains(t, got[0].header, "Atomic-Delegation")
			}

			// The caller's request is never mutated.
			require.Equal(t, http.Header{"User-Agent": {"atomic-go-test"}}, req.Header)
			require.Equal(t, tc.url, req.URL.String())
		})
	}
}

func TestTransportPassesOtherOriginsThroughUntouched(t *testing.T) {
	var got *http.Request
	base := remotetest.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
	})
	tr := newTransport(t, base, fakeCreds{}, "https://atomic.example")

	req := roundTrip(t, tr, "https://other.example/p")
	require.Same(t, req, got)
}

func TestTransportDoesNotFollowARedirectWithCredentials(t *testing.T) {
	rec := &recorder{}
	rec.respond = func(req *http.Request) *http.Response {
		if req.URL.Host == "atomic.example" {
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     http.Header{"Location": {"https://elsewhere.example/landing"}},
				Body:       http.NoBody,
				Request:    req,
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: req}
	}
	tr := newTransport(t, rec.transport(), fakeCreds{}, "https://atomic.example")
	client := &http.Client{Transport: tr}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://atomic.example/p/proj", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	got := rec.requests()
	require.Len(t, got, 2)
	require.Equal(t, "https://atomic.example/p/proj", got[0].url)
	require.Equal(t, testToken, got[0].header.Get("Authorization"))
	require.Equal(t, testCert, got[0].header.Get("Atomic-Delegation"))

	require.Equal(t, "https://elsewhere.example/landing", got[1].url)
	require.NotContains(t, got[1].header, "Authorization")
	require.NotContains(t, got[1].header, "Atomic-Delegation")
}

// closeRecorder is a request body that records whether it was closed.
type closeRecorder struct {
	io.Reader
	mu     sync.Mutex
	closed int
}

func (c *closeRecorder) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func (c *closeRecorder) closes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func TestTransportAttachErrorFailsWithoutSending(t *testing.T) {
	errSign := errors.New("signing failed")
	called := false
	base := remotetest.RoundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("base must not be called")
	})
	tr := newTransport(t, base, fakeCreds{err: errSign}, "https://atomic.example")

	t.Run("with a body", func(t *testing.T) {
		body := &closeRecorder{Reader: strings.NewReader("payload")}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://atomic.example/p", body)
		require.NoError(t, err)

		resp, err := tr.RoundTrip(req)
		require.ErrorIs(t, err, errSign)
		require.Nil(t, resp)
		require.False(t, called)
		require.Equal(t, 1, body.closes())
		require.Empty(t, req.Header)
	})

	t.Run("without a body", func(t *testing.T) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://atomic.example/p", nil)
		require.NoError(t, err)

		resp, err := tr.RoundTrip(req)
		require.ErrorIs(t, err, errSign)
		require.Nil(t, resp)
		require.False(t, called)
	})
}

// contextCreds records the context Attach was given.
type contextCreds struct {
	got chan context.Context
}

func (c contextCreds) Attach(req *http.Request) error {
	c.got <- req.Context()
	return nil
}

type ctxKey struct{}

func TestTransportAttachSeesTheRequestContext(t *testing.T) {
	rec := &recorder{}
	creds := contextCreds{got: make(chan context.Context, 1)}
	tr := newTransport(t, rec.transport(), creds, "https://atomic.example")

	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://atomic.example/p", nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	got := <-creds.got
	require.Equal(t, "marker", got.Value(ctxKey{}))
}

func TestNoCredentialsAttachesNothing(t *testing.T) {
	rec := &recorder{}
	tr := newTransport(t, rec.transport(), auth.NoCredentials{}, "https://atomic.example")

	roundTrip(t, tr, "https://atomic.example/p")

	got := rec.requests()
	require.Len(t, got, 1)
	require.Equal(t, http.Header{"User-Agent": {"atomic-go-test"}}, got[0].header)
}

func TestNewTransportRequiresEveryArgument(t *testing.T) {
	tr, err := auth.NewTransport(nil, nil, nil)
	require.Nil(t, tr)
	require.ErrorIs(t, err, auth.ErrInvalidArgument)
	require.ErrorContains(t, err, "the base RoundTripper is required")
	require.ErrorContains(t, err, "the Credentials is required")
	require.ErrorContains(t, err, "the origin is required")

	var joined interface{ Unwrap() []error }
	require.ErrorAs(t, err, &joined)
	require.Len(t, joined.Unwrap(), 3)
}

func TestNewTransportRefusesAnOriginThatIsNotOne(t *testing.T) {
	base := (&recorder{}).transport()
	for _, origin := range []string{"", "/relative/path", "ftp://atomic.example", "https://", "mailto:someone@atomic.example"} {
		t.Run(origin, func(t *testing.T) {
			tr, err := auth.NewTransport(base, auth.NoCredentials{}, mustURL(t, origin))
			require.Nil(t, tr)
			require.ErrorIs(t, err, auth.ErrInvalidArgument)
			require.ErrorContains(t, err, "the origin must be")
		})
	}
}

func TestNewTransportDoesNotKeepTheOrigin(t *testing.T) {
	rec := &recorder{}
	origin := mustURL(t, "https://atomic.example")
	tr, err := auth.NewTransport(rec.transport(), fakeCreds{}, origin)
	require.NoError(t, err)

	origin.Host = "evil.example"
	roundTrip(t, tr, "https://evil.example/p")

	got := rec.requests()
	require.Len(t, got, 1)
	require.NotContains(t, got[0].header, "Authorization")
}

func TestTransportIsSafeForConcurrentUse(t *testing.T) {
	const n = 32
	rec := &recorder{}
	tr := newTransport(t, rec.transport(), fakeCreds{}, "https://atomic.example")
	client := &http.Client{Transport: tr}

	type result struct {
		resp *http.Response
		err  error
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			target := "https://atomic.example/p"
			if i%2 == 1 {
				target = "https://other.example/p"
			}
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
			if err != nil {
				results <- result{err: err}
				return
			}
			resp, err := client.Do(req)
			results <- result{resp: resp, err: err}
		})
	}
	wg.Wait()
	close(results)

	for r := range results {
		require.NoError(t, r.err)
		require.Equal(t, http.StatusOK, r.resp.StatusCode)
		require.NoError(t, r.resp.Body.Close())
	}

	got := rec.requests()
	require.Len(t, got, n)
	for _, s := range got {
		if strings.HasPrefix(s.url, "https://atomic.example/") {
			require.Equal(t, testToken, s.header.Get("Authorization"))
			require.Equal(t, testCert, s.header.Get("Atomic-Delegation"))
		} else {
			require.NotContains(t, s.header, "Authorization")
			require.NotContains(t, s.header, "Atomic-Delegation")
		}
	}
}
