package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
)

func TestRegistrationMessagePinned(t *testing.T) {
	// The bytes atomic-cli's register.rs pins in its unit test
	// (docs/remote/auth.md#registration).
	const want = "atomic-storage:register\nalice\nABCDEF1234567890\n2025-03-26T17:00:00Z"
	got := identity.RegistrationMessage("alice", "ABCDEF1234567890", "2025-03-26T17:00:00Z")
	require.Equal(t, want, string(got))
}

// testUser returns a user identity for the first RFC 8032 vector, and a
// Signer for its key.
func testUser(t *testing.T) (identity.Identity, identity.Signer) {
	t.Helper()
	s, err := identity.SignerFromSeed(mustHex(t, signatureVectors[0].seed))
	require.NoError(t, err)
	return identity.Identity{
		Name:      "alice",
		Type:      identity.TypeUser,
		Email:     "alice@example.com",
		PublicKey: s.PublicKey(),
	}, s
}

func TestSignRegistration(t *testing.T) {
	id, s := testUser(t)
	at := time.Date(2026, 11, 24, 9, 30, 0, 750_000_000, time.UTC)

	r, err := identity.SignRegistration(context.Background(), id, s, at)
	require.NoError(t, err)
	require.Equal(t, "alice", r.Username)
	require.Equal(t, id.PublicKey, r.PublicKey)
	require.Equal(t, "2026-11-24T09:30:00Z", r.Timestamp)
	require.NotNil(t, r.Email)
	require.Equal(t, "alice@example.com", *r.Email)

	sig, err := ident.DecodeBase32Strict(r.Signature, identity.SignatureSize)
	require.NoError(t, err)
	msg := identity.RegistrationMessage("alice", id.PublicKey.String(), "2026-11-24T09:30:00Z")
	require.True(t, id.PublicKey.Verify(msg, sig))

	b, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"username": "alice",
		"email": "alice@example.com",
		"public_key": "`+id.PublicKey.String()+`",
		"timestamp": "2026-11-24T09:30:00Z",
		"signature": "`+r.Signature+`"
	}`, string(b))
}

func TestSignRegistrationUTC(t *testing.T) {
	id, s := testUser(t)
	at := time.Date(2026, 11, 24, 11, 30, 0, 0, time.FixedZone("UTC+2", 2*60*60))

	r, err := identity.SignRegistration(context.Background(), id, s, at)
	require.NoError(t, err)
	require.Equal(t, "2026-11-24T09:30:00Z", r.Timestamp)
}

func TestSignRegistrationNullEmail(t *testing.T) {
	id, s := testUser(t)
	id.Email = ""

	r, err := identity.SignRegistration(context.Background(), id, s, time.Date(2025, 3, 26, 17, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Nil(t, r.Email)

	b, err := json.Marshal(r)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &fields))
	require.Contains(t, fields, "email")
	require.Equal(t, "null", string(fields["email"]))
}

func TestSignRegistrationRefusesType(t *testing.T) {
	for _, typ := range []identity.Type{identity.TypeAgent, identity.TypeDelegated, ""} {
		t.Run(string(typ), func(t *testing.T) {
			id, s := testUser(t)
			id.Type = typ
			signer := &countingSigner{signer: s}

			_, err := identity.SignRegistration(context.Background(), id, signer, time.Time{})
			require.ErrorIs(t, err, identity.ErrNotRegistrable)
			require.Zero(t, signer.calls)
		})
	}
}

func TestSignRegistrationKeyMismatch(t *testing.T) {
	id, _ := testUser(t)
	other, err := identity.SignerFromSeed(mustHex(t, signatureVectors[1].seed))
	require.NoError(t, err)

	_, err = identity.SignRegistration(context.Background(), id, other, time.Time{})
	require.ErrorIs(t, err, identity.ErrKeyMismatch)
}

func TestSignRegistrationNilSigner(t *testing.T) {
	id, _ := testUser(t)
	_, err := identity.SignRegistration(context.Background(), id, nil, time.Time{})
	require.Error(t, err)
}

func TestSignRegistrationSignerFails(t *testing.T) {
	id, s := testUser(t)
	errSign := errors.New("token unplugged")

	_, err := identity.SignRegistration(context.Background(), id, failingSigner{pub: s.PublicKey(), err: errSign}, time.Time{})
	require.ErrorIs(t, err, errSign)
}

func TestSignRegistrationShortSignature(t *testing.T) {
	id, s := testUser(t)
	_, err := identity.SignRegistration(context.Background(), id, failingSigner{pub: s.PublicKey(), sig: make([]byte, 10)}, time.Time{})
	require.Error(t, err)
}

func TestSignRegistrationDoneContext(t *testing.T) {
	id, s := testUser(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := identity.SignRegistration(ctx, id, s, time.Time{})
	require.ErrorIs(t, err, context.Canceled)
}

// failingSigner is a Signer with a fixed key that returns a fixed
// signature or error.
type failingSigner struct {
	pub identity.PublicKey
	sig []byte
	err error
}

func (f failingSigner) PublicKey() identity.PublicKey { return f.pub }

func (f failingSigner) Sign(context.Context, []byte) ([]byte, error) { return f.sig, f.err }

// countingSigner counts the calls to Sign of the Signer it wraps.
type countingSigner struct {
	signer identity.Signer
	calls  int
}

func (c *countingSigner) PublicKey() identity.PublicKey { return c.signer.PublicKey() }

func (c *countingSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	c.calls++
	return c.signer.Sign(ctx, msg)
}
