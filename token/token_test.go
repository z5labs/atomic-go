package token_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/token"
)

// RFC 8032 §7.1 TEST 1, the seed the golden token is signed with.
const test1Seed = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

// goldenToken is the user token TEST 1's key signs for exampleParams. It
// was computed independently, with crypto/ed25519 and hand-written JSON,
// not with this package.
const goldenToken = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCIsImtpZCI6IjI1TkpRQU1DV0VGTFBWS0w3M0o0U1pBSEhJSE9DNFhUM0tUQ0dKTlBBSU5HUjVZSEtFTkEifQ" +
	".eyJzdWIiOiIyNU5KUUFNQ1dFRkxQVktMNzNKNFNaQUhISUhPQzRYVDNLVENHSk5QQUlOR1I1WUhLRU5BIiwiaWF0IjoxNzk1NTEyNjAwLCJleHAiOjE3OTU1MTI5MDAsImp0aSI6IjBiNmYzYzllLTFmN2EtNGMyZS05YTUxLTNkMmI4ZTRmNmExMCIsImF1ZCI6Imh0dHBzOi8vYXRvbWljLnN0b3JhZ2UifQ" +
	".248Fhfj68YrYoQVIHJVCKnl12NunPAD-RQWyzJtd8sWsqYPWHejuGyoyHibHduRGnYynhlf4Queuox1lWfE1DA"

// test1Key is TEST 1's public key as strict base32.
const test1Key = "25NJQAMCWEFLPVKL73J4SZAHHIHOC4XT3KTCGJNPAINGR5YHKENA"

// exampleTime is the instant of the examples in
// docs/remote/auth.md#token-format.
var exampleTime = time.Unix(1795512600, 0)

var exampleID = uuid.MustParse("0b6f3c9e-1f7a-4c2e-9a51-3d2b8e4f6a10")

const exampleDelegation = "urn:atomic:delegation:9d61b19d"

func exampleParams() token.Params {
	return token.Params{
		Audience: "https://atomic.storage",
		IssuedAt: exampleTime,
		ID:       exampleID,
	}
}

func signerFromHex(t *testing.T, seed string) identity.Signer {
	t.Helper()
	b, err := hex.DecodeString(seed)
	require.NoError(t, err)
	s, err := identity.SignerFromSeed(b)
	require.NoError(t, err)
	return s
}

// signerFromByte returns a Signer whose seed is 32 copies of b.
func signerFromByte(t *testing.T, b byte) identity.Signer {
	t.Helper()
	s, err := identity.SignerFromSeed(bytes.Repeat([]byte{b}, identity.SeedSize))
	require.NoError(t, err)
	return s
}

// segments returns the decoded header and claims JSON of a compact token.
func segments(t *testing.T, tok string) (header, claims string) {
	t.Helper()
	parts := strings.Split(tok, ".")
	require.Len(t, parts, 3)
	h, err := base64.RawURLEncoding.DecodeString(parts[0])
	require.NoError(t, err)
	c, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	return string(h), string(c)
}

func TestConstants(t *testing.T) {
	require.Equal(t, "EdDSA", token.Algorithm)
	require.Equal(t, "JWT", token.Type)
	require.Equal(t, 5*time.Minute, token.Lifetime)
}

func TestHeaderBytes(t *testing.T) {
	s := signerFromHex(t, test1Seed)
	require.Equal(t, test1Key, s.PublicKey().String())

	tok, err := token.Sign(context.Background(), s, exampleParams())
	require.NoError(t, err)

	header, _ := segments(t, tok.String())
	require.Equal(t, `{"alg":"EdDSA","typ":"JWT","kid":"`+test1Key+`"}`, header)
	require.Equal(t, token.Header{Alg: "EdDSA", Typ: "JWT", Kid: test1Key}, tok.Header())
}

func TestGoldenToken(t *testing.T) {
	tok, err := token.Sign(context.Background(), signerFromHex(t, test1Seed), exampleParams())
	require.NoError(t, err)
	require.Equal(t, goldenToken, tok.String())

	parsed, err := token.Parse(goldenToken)
	require.NoError(t, err)
	require.Equal(t, tok.Header(), parsed.Header())
	require.Equal(t, tok.Claims(), parsed.Claims())
	require.Equal(t, tok.Signature(), parsed.Signature())
	require.Equal(t, goldenToken, parsed.String())
	require.NoError(t, parsed.Verify(signerFromHex(t, test1Seed).PublicKey(), "https://atomic.storage", exampleTime))
}

func TestUserExample(t *testing.T) {
	s := signerFromByte(t, 1)
	k := s.PublicKey().String()

	tok, err := token.Sign(context.Background(), s, exampleParams())
	require.NoError(t, err)

	header, claims := segments(t, tok.String())
	require.Equal(t, `{"alg":"EdDSA","typ":"JWT","kid":"`+k+`"}`, header)
	require.Equal(t, `{"sub":"`+k+`","iat":1795512600,"exp":1795512900,"jti":"0b6f3c9e-1f7a-4c2e-9a51-3d2b8e4f6a10","aud":"https://atomic.storage"}`, claims)
	require.NotContains(t, claims, `"act"`)
	require.NotContains(t, claims, `"dlg"`)
	require.NotContains(t, claims, "null")
	require.Equal(t, token.Claims{
		Sub: k,
		Iat: 1795512600,
		Exp: 1795512900,
		Jti: "0b6f3c9e-1f7a-4c2e-9a51-3d2b8e4f6a10",
		Aud: "https://atomic.storage",
	}, tok.Claims())
}

func TestDelegatedExample(t *testing.T) {
	agent := signerFromByte(t, 2)
	human := signerFromByte(t, 3).PublicKey()
	a, h := agent.PublicKey().String(), human.String()

	p := exampleParams()
	p.OnBehalfOf = &token.OnBehalfOf{Delegator: human, Delegation: exampleDelegation}
	tok, err := token.Sign(context.Background(), agent, p)
	require.NoError(t, err)

	header, claims := segments(t, tok.String())
	require.Equal(t, `{"alg":"EdDSA","typ":"JWT","kid":"`+a+`"}`, header)
	require.Equal(t, `{"sub":"`+h+`","iat":1795512600,"exp":1795512900,"jti":"0b6f3c9e-1f7a-4c2e-9a51-3d2b8e4f6a10","aud":"https://atomic.storage","act":{"sub":"`+a+`"},"dlg":"`+exampleDelegation+`"}`, claims)
	require.NoError(t, tok.Verify(agent.PublicKey(), "https://atomic.storage", exampleTime))
}

func TestSignIsDeterministic(t *testing.T) {
	s := signerFromByte(t, 4)
	p := exampleParams()
	p.IssuedAt = exampleTime.Add(999 * time.Millisecond)
	first, err := token.Sign(context.Background(), s, p)
	require.NoError(t, err)
	second, err := token.Sign(context.Background(), s, p)
	require.NoError(t, err)
	require.Equal(t, first.String(), second.String())
	require.Equal(t, int64(1795512600), first.Claims().Iat)
	require.Equal(t, int64(1795512900), first.Claims().Exp)
}

func TestSignNormalizesAudienceWithoutHTMLEscaping(t *testing.T) {
	p := exampleParams()
	p.Audience = " HTTPS://Example.Test/a<b>&c/ "
	tok, err := token.Sign(context.Background(), signerFromByte(t, 5), p)
	require.NoError(t, err)

	_, claims := segments(t, tok.String())
	require.Contains(t, claims, `"aud":"https://example.test/a<b>&c"`)
}

func TestSignRefusesArguments(t *testing.T) {
	s := signerFromByte(t, 6)
	tests := []struct {
		name   string
		signer identity.Signer
		edit   func(*token.Params)
	}{
		{"nil signer", nil, func(*token.Params) {}},
		{"empty audience", s, func(p *token.Params) { p.Audience = " / " }},
		{"zero issue time", s, func(p *token.Params) { p.IssuedAt = time.Time{} }},
		{"zero id", s, func(p *token.Params) { p.ID = uuid.UUID{} }},
		{"no delegation", s, func(p *token.Params) {
			p.OnBehalfOf = &token.OnBehalfOf{Delegator: s.PublicKey()}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := exampleParams()
			tt.edit(&p)
			_, err := token.Sign(context.Background(), tt.signer, p)
			require.ErrorIs(t, err, token.ErrInvalidArgument)
		})
	}
}

func TestSignDoneContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := token.Sign(ctx, signerFromByte(t, 7), exampleParams())
	require.ErrorIs(t, err, context.Canceled)
}

// badSigner returns a signature that does not verify.
type badSigner struct{ identity.Signer }

func (b badSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	return make([]byte, identity.SignatureSize), nil
}

func TestSignChecksSignature(t *testing.T) {
	_, err := token.Sign(context.Background(), badSigner{signerFromByte(t, 8)}, exampleParams())
	require.ErrorContains(t, err, "does not verify")
}

func TestAudience(t *testing.T) {
	tests := []struct{ in, want string }{
		{" https://Atomic.Storage/// ", "https://atomic.storage"},
		{"https://Atomic.Storage/", "https://atomic.storage"},
		{"https://atomic.storage", "https://atomic.storage"},
		{"HTTPS://A.Example/Path/", "https://a.example/path"},
		{"https://Ä.example", "https://Ä.example"},
		{" / ", ""},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, token.Audience(tt.in), "Audience(%q)", tt.in)
		require.Equal(t, tt.want, token.Audience(tt.want), "Audience(%q)", tt.want)
	}
}

func TestNewID(t *testing.T) {
	id, err := token.NewID(bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), id.Version())
	require.Equal(t, uuid.RFC4122, id.Variant())
	require.Equal(t, "ffffffff-ffff-4fff-bfff-ffffffffffff", id.String())

	id, err = token.NewID(bytes.NewReader(make([]byte, 16)))
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), id.Version())
	require.Equal(t, uuid.RFC4122, id.Variant())
	require.Equal(t, "00000000-0000-4000-8000-000000000000", id.String())
}

func TestNewIDErrors(t *testing.T) {
	_, err := token.NewID(nil)
	require.ErrorIs(t, err, token.ErrInvalidArgument)

	_, err = token.NewID(bytes.NewReader(make([]byte, 15)))
	require.Error(t, err)
}

func TestParseMalformed(t *testing.T) {
	enc := base64.RawURLEncoding.EncodeToString
	sig := enc(make([]byte, 64))
	header := enc([]byte(`{"alg":"EdDSA","typ":"JWT","kid":"K"}`))
	claims := enc([]byte(`{"sub":"K","iat":1,"exp":2,"jti":"j","aud":"a"}`))

	tests := map[string]string{
		"empty":                "",
		"two segments":         header + "." + claims,
		"four segments":        header + "." + claims + "." + sig + "." + sig,
		"line break":           header + "." + claims + ".\n" + sig,
		"padded":               header + "." + claims + "." + sig + "==",
		"standard alphabet":    header + "." + claims + "." + strings.Repeat("+", 86),
		"unused bits set":      header + "." + claims + "." + sig[:85] + "B",
		"short signature":      header + "." + claims + "." + enc(make([]byte, 63)),
		"header not json":      enc([]byte("nope")) + "." + claims + "." + sig,
		"header null":          enc([]byte("null")) + "." + claims + "." + sig,
		"header array":         enc([]byte(`["EdDSA"]`)) + "." + claims + "." + sig,
		"claims empty":         header + ".." + sig,
		"iat is a string":      header + "." + enc([]byte(`{"iat":"1"}`)) + "." + sig,
		"iat is a fraction":    header + "." + enc([]byte(`{"iat":1.5}`)) + "." + sig,
		"duplicate member":     header + "." + enc([]byte(`{"sub":"K","sub":"L"}`)) + "." + sig,
		"invalid utf-8":        enc([]byte("{\"alg\":\"\xff\"}")) + "." + claims + "." + sig,
		"act is not an object": header + "." + enc([]byte(`{"act":"K"}`)) + "." + sig,
	}
	for name, s := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := token.Parse(s)
			require.ErrorIs(t, err, token.ErrMalformed)
		})
	}
}

// forge signs arbitrary header and claims JSON with priv, so a test can
// build tokens Sign never would.
func forge(t *testing.T, priv ed25519.PrivateKey, header, claims string) token.Token {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	signed := enc([]byte(header)) + "." + enc([]byte(claims))
	tok, err := token.Parse(signed + "." + enc(ed25519.Sign(priv, []byte(signed))))
	require.NoError(t, err)
	return tok
}

func TestVerifyAccepts(t *testing.T) {
	user := signerFromByte(t, 9)
	tok, err := token.Sign(context.Background(), user, exampleParams())
	require.NoError(t, err)

	key := user.PublicKey()
	require.NoError(t, tok.Verify(key, "https://atomic.storage", exampleTime))
	require.NoError(t, tok.Verify(key, "https://Atomic.Storage/", exampleTime))
	require.NoError(t, tok.Verify(key, "https://atomic.storage", exampleTime.Add(token.Lifetime-time.Nanosecond)))
}

func TestVerifyRefusals(t *testing.T) {
	seed := bytes.Repeat([]byte{10}, identity.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	signer, err := identity.SignerFromSeed(seed)
	require.NoError(t, err)
	key := signer.PublicKey()
	k := key.String()
	other := signerFromByte(t, 11)
	o := other.PublicKey().String()

	const aud = "https://atomic.storage"
	userHeader := `{"alg":"EdDSA","typ":"JWT","kid":"` + k + `"}`
	userClaims := func(sub string) string {
		return `{"sub":"` + sub + `","iat":1795512600,"exp":1795512900,"jti":"j","aud":"` + aud + `"}`
	}

	user, err := token.Sign(context.Background(), signer, exampleParams())
	require.NoError(t, err)
	tampered, err := token.Parse(user.String()[:len(user.String())-2] + "AA")
	require.NoError(t, err)
	fromOther, err := token.Sign(context.Background(), other, exampleParams())
	require.NoError(t, err)

	tests := []struct {
		name string
		tok  token.Token
		key  identity.PublicKey
		aud  string
		now  time.Time
		msg  string
	}{
		{
			name: "alg",
			tok:  forge(t, priv, `{"alg":"none","typ":"JWT","kid":"`+k+`"}`, userClaims(k)),
			msg:  "alg",
		},
		{
			name: "zero token",
			tok:  token.Token{},
			msg:  "alg",
		},
		{
			name: "kid is not the key",
			tok:  fromOther,
			msg:  "kid",
		},
		{
			name: "user sub is not the kid",
			tok:  forge(t, priv, userHeader, userClaims(o)),
			msg:  "sub",
		},
		{
			name: "act.sub is not the kid",
			tok: forge(t, priv, userHeader,
				`{"sub":"`+o+`","iat":1795512600,"exp":1795512900,"jti":"j","aud":"`+aud+`","act":{"sub":"`+o+`"},"dlg":"urn:x"}`),
			msg: "act.sub",
		},
		{
			name: "act without dlg",
			tok: forge(t, priv, userHeader,
				`{"sub":"`+o+`","iat":1795512600,"exp":1795512900,"jti":"j","aud":"`+aud+`","act":{"sub":"`+k+`"}}`),
			msg: "act and dlg",
		},
		{
			name: "dlg without act",
			tok: forge(t, priv, userHeader,
				`{"sub":"`+k+`","iat":1795512600,"exp":1795512900,"jti":"j","aud":"`+aud+`","dlg":"urn:x"}`),
			msg: "act and dlg",
		},
		{
			name: "aud",
			tok:  user,
			aud:  "https://other.example",
			msg:  "aud",
		},
		{
			name: "issued in the future",
			tok:  user,
			now:  exampleTime.Add(-time.Nanosecond),
			msg:  "issued",
		},
		{
			name: "expires at now",
			tok:  user,
			now:  exampleTime.Add(token.Lifetime),
			msg:  "expired",
		},
		{
			name: "expired",
			tok:  user,
			now:  exampleTime.Add(time.Hour),
			msg:  "expired",
		},
		{
			name: "signature",
			tok:  tampered,
			msg:  "signature",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.key == (identity.PublicKey{}) {
				tt.key = key
			}
			if tt.aud == "" {
				tt.aud = aud
			}
			if tt.now.IsZero() {
				tt.now = exampleTime
			}
			err := tt.tok.Verify(tt.key, tt.aud, tt.now)
			require.ErrorIs(t, err, token.ErrRejected)
			require.ErrorContains(t, err, tt.msg)
		})
	}
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestNewMinterRequiresArguments(t *testing.T) {
	s := signerFromByte(t, 12)

	_, err := token.NewMinter(s, "https://atomic.storage", nil, nil)
	require.ErrorIs(t, err, token.ErrInvalidArgument)
	require.ErrorContains(t, err, "clock")
	require.ErrorContains(t, err, "random")

	_, err = token.NewMinter(nil, " ", nil, nil, nil, token.WithOnBehalfOf(token.OnBehalfOf{}))
	require.ErrorIs(t, err, token.ErrInvalidArgument)
	for _, want := range []string{"Signer", "audience", "clock", "random", "nil MinterOption", "Delegation"} {
		require.ErrorContains(t, err, want)
	}
}

func TestMinterMintsFresh(t *testing.T) {
	s := signerFromByte(t, 13)
	now := exampleTime
	clock := func() time.Time { return now }
	m, err := token.NewMinter(s, "https://Atomic.Storage/", clock, rand.NewChaCha8([32]byte{1}))
	require.NoError(t, err)

	first, err := m.Mint(context.Background())
	require.NoError(t, err)
	now = now.Add(time.Minute)
	second, err := m.Mint(context.Background())
	require.NoError(t, err)

	require.NotEqual(t, first.Claims().Jti, second.Claims().Jti)
	require.Equal(t, exampleTime.Unix(), first.Claims().Iat)
	require.Equal(t, exampleTime.Add(time.Minute).Unix(), second.Claims().Iat)
	require.Equal(t, "https://atomic.storage", first.Claims().Aud)
	require.NoError(t, first.Verify(s.PublicKey(), "https://atomic.storage", exampleTime))
	require.NoError(t, second.Verify(s.PublicKey(), "https://atomic.storage", now))

	id, err := uuid.Parse(first.Claims().Jti)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), id.Version())
}

func TestMinterOnBehalfOf(t *testing.T) {
	agent := signerFromByte(t, 14)
	human := signerFromByte(t, 15).PublicKey()
	m, err := token.NewMinter(agent, "https://atomic.storage", fixedClock(exampleTime), rand.NewChaCha8([32]byte{2}),
		token.WithOnBehalfOf(token.OnBehalfOf{Delegator: human, Delegation: exampleDelegation}))
	require.NoError(t, err)

	tok, err := m.Mint(context.Background())
	require.NoError(t, err)
	c := tok.Claims()
	require.Equal(t, human.String(), c.Sub)
	require.Equal(t, &token.Actor{Sub: agent.PublicKey().String()}, c.Act)
	require.Equal(t, exampleDelegation, c.Dlg)
	require.NoError(t, tok.Verify(agent.PublicKey(), "https://atomic.storage", exampleTime))
}

// failingReader fails every read.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestMintErrors(t *testing.T) {
	m, err := token.NewMinter(signerFromByte(t, 16), "https://atomic.storage", fixedClock(exampleTime), failingReader{})
	require.NoError(t, err)
	_, err = m.Mint(context.Background())
	require.ErrorContains(t, err, "no entropy")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = m.Mint(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestMintConcurrent(t *testing.T) {
	const n = 64
	m, err := token.NewMinter(signerFromByte(t, 17), "https://atomic.storage", fixedClock(exampleTime), rand.NewChaCha8([32]byte{3}))
	require.NoError(t, err)

	type result struct {
		tok token.Token
		err error
	}
	results := make(chan result, n)
	for range n {
		go func() {
			tok, err := m.Mint(context.Background())
			results <- result{tok, err}
		}()
	}

	seen := make(map[string]bool, n)
	for range n {
		r := <-results
		require.NoError(t, r.err)
		jti := r.tok.Claims().Jti
		require.False(t, seen[jti], "jti %s minted twice", jti)
		seen[jti] = true
	}
	require.Len(t, seen, n)
}
