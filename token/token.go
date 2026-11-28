// Package token mints and verifies the bearer tokens Atomic sends in an
// Authorization header: self-signed EdDSA JWTs, valid for [Lifetime]
// (docs/remote/auth.md#tokens).
//
// The signature covers the exact bytes of the header and the claims, so
// both are written by this package rather than by a JOSE library: the
// header is always {"alg":"EdDSA","typ":"JWT","kid":…} and the claims are
// always written in the order sub, iat, exp, jti, aud, act, dlg, compact and
// without HTML escaping, with act and dlg left out of a user's token.
//
// Nothing here reads the host. [Sign] takes the issue time and the token id
// as arguments, and a [Minter] takes the clock and the random source its
// ids come from in its constructor, so a test can mint a token with known
// bytes and verify it at a known instant.
package token

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/z5labs/atomic-go/identity"
)

const (
	// Algorithm is the JOSE algorithm of every token, the header's alg.
	Algorithm = "EdDSA"

	// Type is the header's typ.
	Type = "JWT"

	// Lifetime is how long a token is valid: its exp is its iat plus
	// Lifetime, in whole seconds.
	Lifetime = 5 * time.Minute
)

// lifetimeSeconds is [Lifetime] in the whole seconds that iat and exp are
// written in.
const lifetimeSeconds = int64(Lifetime / time.Second)

var (
	// ErrMalformed is wrapped by every error [Parse] returns for text that
	// is not a well-formed compact JWT. Parse does not check alg or any
	// other claim's value; that is [Token.Verify]'s job.
	ErrMalformed = errors.New("token: malformed token")

	// ErrRejected is wrapped by every error [Token.Verify] returns for a
	// token that fails one of the checks it makes.
	ErrRejected = errors.New("token: token rejected")

	// ErrInvalidArgument is wrapped by every error [Sign], [NewMinter] and
	// [NewID] return for an argument they cannot use.
	ErrInvalidArgument = errors.New("token: invalid argument")
)

// encoding is the unpadded base64url of all three segments. Strict refuses
// a final character with unused bits set, so a segment has one spelling.
var encoding = base64.RawURLEncoding.Strict()

// Header is a token's JOSE header. Its fields are written in this order.
type Header struct {
	// Alg is always [Algorithm].
	Alg string `json:"alg"`
	// Typ is always [Type].
	Typ string `json:"typ"`
	// Kid is the signer's public key as strict base32.
	Kid string `json:"kid"`
}

// Claims is a token's claims. Its fields are written in this order, and
// Act and Dlg are left out entirely, never written as null, when they are
// unset.
type Claims struct {
	// Sub is the effective subject's public key as strict base32: the
	// signer's own key, or for a delegated token the delegator's.
	Sub string `json:"sub"`
	// Iat is the issue time in Unix seconds.
	Iat int64 `json:"iat"`
	// Exp is the expiry time in Unix seconds, Iat plus [Lifetime].
	Exp int64 `json:"exp"`
	// Jti is the token's id, a UUID v4 in lowercase hyphenated form.
	Jti string `json:"jti"`
	// Aud is the normalized server URL the token is for; see [Audience].
	Aud string `json:"aud"`
	// Act names the agent acting for Sub. It is set only on a delegated
	// token.
	Act *Actor `json:"act,omitempty"`
	// Dlg is the URN of the delegation certificate that authorizes the
	// agent. It is set exactly when Act is.
	Dlg string `json:"dlg,omitempty"`
}

// Actor is the value of a delegated token's act claim.
type Actor struct {
	// Sub is the agent's public key as strict base32, the same key as the
	// header's kid.
	Sub string `json:"sub"`
}

// Audience returns the aud claim for a token minted for server: server with
// surrounding whitespace and every trailing slash removed, then lowercased
// in full, path included, with only the ASCII letters changed. So
// " https://Atomic.Storage/ " gives "https://atomic.storage".
//
// It is one pass, exactly as the reference client normalizes, so it is not
// idempotent for whitespace among the trailing slashes: "https://x.test/ /"
// gives "https://x.test/ ". [Sign], [NewMinter] and [Token.Verify] each
// normalize the server URL they are given exactly once, so give them all
// the same URL.
func Audience(server string) string {
	s := strings.TrimRight(strings.TrimSpace(server), "/")
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// OnBehalfOf makes a token delegated: its sub is Delegator, the signer's
// key moves to act.sub, and dlg is Delegation.
type OnBehalfOf struct {
	// Delegator is the key of the identity the agent acts for, as read from
	// the certificate's delegatorKey. It must not be the zero key.
	Delegator identity.PublicKey
	// Delegation is the certificate's id as a URN, such as
	// "urn:atomic:delegation:…". It must not be empty.
	Delegation string
}

// check refuses an o that cannot make a delegated token, naming it as what.
func (o OnBehalfOf) check(what string) error {
	var errs []error
	if o.Delegator == (identity.PublicKey{}) {
		errs = append(errs, fmt.Errorf("%w: %s has a zero Delegator", ErrInvalidArgument, what))
	}
	if o.Delegation == "" {
		errs = append(errs, fmt.Errorf("%w: %s has no Delegation", ErrInvalidArgument, what))
	}
	return errors.Join(errs...)
}

// Params is everything [Sign] writes into a token besides the signer's key.
type Params struct {
	// Audience is the server URL the token is for. Sign writes [Audience]
	// of it, which must not be empty.
	Audience string
	// IssuedAt is the issue time. Its fraction of a second is dropped. It
	// must not be the zero time.
	IssuedAt time.Time
	// ID is the token's jti, such as one from [NewID]. It must not be the
	// zero UUID.
	ID uuid.UUID
	// OnBehalfOf, when set, makes the token delegated.
	OnBehalfOf *OnBehalfOf
}

// Token is a signed token, as made by [Sign] or read by [Parse]. It keeps
// the exact segments it was made from, so [Token.String] returns them
// unchanged and [Token.Verify] checks the signature over them. The zero
// Token is not a valid token.
type Token struct {
	header    Header
	claims    Claims
	signature []byte
	// signed is "<header segment>.<claims segment>", the bytes the
	// signature covers.
	signed string
	// text is the whole compact token.
	text string
}

// Header returns the token's header.
func (t Token) Header() Header {
	return t.header
}

// Claims returns the token's claims. Its Act, when set, is a copy.
func (t Token) Claims() Claims {
	c := t.claims
	if c.Act != nil {
		act := *c.Act
		c.Act = &act
	}
	return c
}

// Signature returns a copy of the token's [identity.SignatureSize]-byte
// Ed25519 signature.
func (t Token) Signature() []byte {
	return bytes.Clone(t.signature)
}

// String returns the compact token, the value an Authorization: Bearer
// header carries.
func (t Token) String() string {
	return t.text
}

// Sign builds the token that p describes and signs it with s. It reads no
// clock and no random source, so the same s and p always give the same
// bytes (docs/remote/auth.md#minting-a-token):
//
//   - kid is s's key. sub is that key too, or with p.OnBehalfOf set, the
//     delegator's key, with s's key in act.sub and the delegation in dlg.
//   - iat is p.IssuedAt in whole Unix seconds and exp is iat plus
//     [Lifetime].
//   - jti is p.ID and aud is [Audience] of p.Audience.
//
// Sign returns ctx.Err() when ctx is done before it signs. A nil s, an
// empty audience, a zero IssuedAt or ID, or an OnBehalfOf with a zero
// Delegator or no Delegation is an error wrapping [ErrInvalidArgument]. An
// error from s is returned wrapped, and so is a signature that is not
// [identity.SignatureSize] bytes or does not verify against s's key.
func Sign(ctx context.Context, s identity.Signer, p Params) (Token, error) {
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	if s == nil {
		return Token{}, fmt.Errorf("%w: nil Signer", ErrInvalidArgument)
	}
	aud := Audience(p.Audience)
	if aud == "" {
		return Token{}, fmt.Errorf("%w: empty audience", ErrInvalidArgument)
	}
	if p.IssuedAt.IsZero() {
		return Token{}, fmt.Errorf("%w: zero issue time", ErrInvalidArgument)
	}
	if p.ID == (uuid.UUID{}) {
		return Token{}, fmt.Errorf("%w: zero token id", ErrInvalidArgument)
	}
	if p.OnBehalfOf != nil {
		if err := p.OnBehalfOf.check("OnBehalfOf"); err != nil {
			return Token{}, err
		}
	}

	key := s.PublicKey()
	kid := key.String()
	h := Header{Alg: Algorithm, Typ: Type, Kid: kid}
	iat := p.IssuedAt.Unix()
	c := Claims{
		Sub: kid,
		Iat: iat,
		Exp: iat + lifetimeSeconds,
		Jti: p.ID.String(),
		Aud: aud,
	}
	if p.OnBehalfOf != nil {
		c.Sub = p.OnBehalfOf.Delegator.String()
		c.Act = &Actor{Sub: kid}
		c.Dlg = p.OnBehalfOf.Delegation
	}

	hb, err := json.Marshal(h)
	if err != nil {
		return Token{}, fmt.Errorf("token: sign: header: %w", err)
	}
	cb, err := json.Marshal(c)
	if err != nil {
		return Token{}, fmt.Errorf("token: sign: claims: %w", err)
	}
	signed := encoding.EncodeToString(hb) + "." + encoding.EncodeToString(cb)

	sig, err := s.Sign(ctx, []byte(signed))
	if err != nil {
		return Token{}, fmt.Errorf("token: sign: %w", err)
	}
	if len(sig) != identity.SignatureSize {
		return Token{}, fmt.Errorf("token: sign: signature is %d bytes, want %d", len(sig), identity.SignatureSize)
	}
	if !key.Verify([]byte(signed), sig) {
		return Token{}, fmt.Errorf("token: sign: signature does not verify against %s", key)
	}
	return Token{
		header:    h,
		claims:    c,
		signature: bytes.Clone(sig),
		signed:    signed,
		text:      signed + "." + encoding.EncodeToString(sig),
	}, nil
}

// Parse reads a compact token: three segments of strict unpadded base64url
// joined by ".", the first two each a JSON object and the third an
// [identity.SignatureSize]-byte signature. Members Header and Claims do not
// name are ignored; a duplicate member, a member of the wrong JSON type and
// invalid UTF-8 are not. Anything else is an error wrapping [ErrMalformed].
//
// Parse checks only the token's shape. Whether it is valid for a key, an
// audience and an instant is [Token.Verify]'s to decide.
func Parse(s string) (Token, error) {
	if strings.ContainsAny(s, "\r\n") {
		// The base64 decoder skips CR and LF, so refuse them here.
		return Token{}, fmt.Errorf("%w: contains a line break", ErrMalformed)
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Token{}, fmt.Errorf("%w: %d segments, want 3", ErrMalformed, len(parts))
	}

	var t Token
	if err := decodeSegment("header", parts[0], &t.header); err != nil {
		return Token{}, err
	}
	if err := decodeSegment("claims", parts[1], &t.claims); err != nil {
		return Token{}, err
	}
	sig, err := encoding.DecodeString(parts[2])
	if err != nil {
		return Token{}, fmt.Errorf("%w: signature: %w", ErrMalformed, err)
	}
	if len(sig) != identity.SignatureSize {
		return Token{}, fmt.Errorf("%w: signature is %d bytes, want %d", ErrMalformed, len(sig), identity.SignatureSize)
	}
	t.signature = sig
	t.signed = parts[0] + "." + parts[1]
	t.text = s
	return t, nil
}

func decodeSegment(name, seg string, v any) error {
	b, err := encoding.DecodeString(seg)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMalformed, name, err)
	}
	// Unmarshalling null into a struct succeeds and leaves it unchanged,
	// so require an object explicitly.
	if trimmed := bytes.TrimLeft(b, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("%w: %s is not a JSON object", ErrMalformed, name)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMalformed, name, err)
	}
	return nil
}

// Verify checks t the way the reference verifier does
// (docs/remote/auth.md#what-the-server-is-expected-to-check), for the
// registered key, the server audience and the instant now:
//
//   - alg is [Algorithm];
//   - kid is key;
//   - kid is act.sub when act is present, and sub otherwise;
//   - dlg is present exactly when act is;
//   - aud is [Audience] of audience, and neither is empty;
//   - iat ≤ now < exp, with now in Unix seconds and no leeway either way;
//   - the signature is key's Ed25519 signature of the first two segments.
//
// The first check that fails is returned as an error wrapping
// [ErrRejected]. Verify does not check a delegated token's certificate.
func (t Token) Verify(key identity.PublicKey, audience string, now time.Time) error {
	h, c := t.header, t.claims
	if h.Alg != Algorithm {
		return fmt.Errorf("%w: alg is %q, want %q", ErrRejected, h.Alg, Algorithm)
	}
	want := key.String()
	if h.Kid != want {
		return fmt.Errorf("%w: kid is %q, want %q", ErrRejected, h.Kid, want)
	}
	if c.Act != nil {
		if c.Act.Sub != h.Kid {
			return fmt.Errorf("%w: act.sub %q is not the kid %q", ErrRejected, c.Act.Sub, h.Kid)
		}
	} else if c.Sub != h.Kid {
		return fmt.Errorf("%w: sub %q is not the kid %q", ErrRejected, c.Sub, h.Kid)
	}
	if (c.Act != nil) != (c.Dlg != "") {
		return fmt.Errorf("%w: act and dlg must be present together", ErrRejected)
	}
	aud := Audience(audience)
	if aud == "" {
		return fmt.Errorf("%w: empty audience to verify against", ErrRejected)
	}
	if c.Aud != aud {
		return fmt.Errorf("%w: aud is %q, want %q", ErrRejected, c.Aud, aud)
	}
	sec := now.Unix()
	if c.Iat > sec {
		return fmt.Errorf("%w: issued at %d, after %d", ErrRejected, c.Iat, sec)
	}
	if sec >= c.Exp {
		return fmt.Errorf("%w: expired at %d, not after %d", ErrRejected, c.Exp, sec)
	}
	if !key.Verify([]byte(t.signed), t.signature) {
		return fmt.Errorf("%w: signature does not verify against %s", ErrRejected, want)
	}
	return nil
}

// NewID returns a new random UUID v4 for a token's jti, made from 16 bytes
// read from r. An error from r is returned wrapped.
func NewID(r io.Reader) (uuid.UUID, error) {
	if r == nil {
		return uuid.UUID{}, fmt.Errorf("%w: nil random source", ErrInvalidArgument)
	}
	id, err := uuid.NewRandomFromReader(r)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("token: new id: %w", err)
	}
	return id, nil
}

// MinterOption configures a [Minter].
type MinterOption func(*minterConfig)

type minterConfig struct {
	onBehalfOf *OnBehalfOf
	errs       []error
}

// WithOnBehalfOf makes every token the Minter mints delegated, as
// [Params.OnBehalfOf] describes. An o with a zero Delegator or no
// Delegation makes [NewMinter] fail.
func WithOnBehalfOf(o OnBehalfOf) MinterOption {
	return func(c *minterConfig) {
		if err := o.check("WithOnBehalfOf"); err != nil {
			c.errs = append(c.errs, err)
			return
		}
		c.onBehalfOf = &o
	}
}

// Minter mints a fresh token for one signer and audience on every call to
// [Minter.Mint]. It is safe for concurrent use as long as its signer and
// clock are; it serializes its reads of the random source itself.
type Minter struct {
	signer     identity.Signer
	audience   string
	clock      func() time.Time
	onBehalfOf *OnBehalfOf

	mu     sync.Mutex
	random io.Reader
}

// NewMinter returns a Minter whose tokens are signed by s, for the server
// URL audience, which each token's aud is [Audience] of, issued at the instant clock returns and given ids from random.
//
// Every argument is required, and nothing falls back to a default: NewMinter
// refuses a nil s, an audience that [Audience] makes empty, a nil clock and
// a nil random, and names every one it refused, along with any option it
// refused, in one error made with [errors.Join]. Each of them wraps
// [ErrInvalidArgument].
func NewMinter(s identity.Signer, audience string, clock func() time.Time, random io.Reader, opts ...MinterOption) (*Minter, error) {
	var errs []error
	missing := func(what string) {
		errs = append(errs, fmt.Errorf("%w: %s is required", ErrInvalidArgument, what))
	}
	if s == nil {
		missing("the Signer")
	}
	if Audience(audience) == "" {
		missing("the audience")
	}
	if clock == nil {
		missing("the clock")
	}
	if random == nil {
		missing("the random source")
	}

	var cfg minterConfig
	for _, opt := range opts {
		if opt == nil {
			errs = append(errs, fmt.Errorf("%w: nil MinterOption", ErrInvalidArgument))
			continue
		}
		opt(&cfg)
	}
	errs = append(errs, cfg.errs...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("token: NewMinter: %w", errors.Join(errs...))
	}
	return &Minter{
		signer:     s,
		audience:   audience,
		clock:      clock,
		onBehalfOf: cfg.onBehalfOf,
		random:     random,
	}, nil
}

// Mint returns a new token, issued at the clock's current instant and with
// a new id read from the random source. Every call mints afresh; nothing is
// cached. Errors are those of [NewID] and [Sign].
func (m *Minter) Mint(ctx context.Context) (Token, error) {
	if err := ctx.Err(); err != nil {
		return Token{}, err
	}
	m.mu.Lock()
	id, err := NewID(m.random)
	m.mu.Unlock()
	if err != nil {
		return Token{}, err
	}
	p := Params{
		Audience: m.audience,
		IssuedAt: m.clock(),
		ID:       id,
	}
	if m.onBehalfOf != nil {
		o := *m.onBehalfOf
		p.OnBehalfOf = &o
	}
	return Sign(ctx, m.signer, p)
}
