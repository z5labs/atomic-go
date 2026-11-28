package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/chrono"
)

// Registration is the signed JSON body that registers a user identity with
// a server, POSTed to {server_url}/register with no Authorization header:
// the signature is the credential (docs/remote/auth.md#registration and
// docs/remote/endpoints/storage.md#execute).
//
// Username, PublicKey and Timestamp are the lines the signature covers,
// exactly as [RegistrationMessage] joins them. Email is not signed.
type Registration struct {
	// Username is the identity's name.
	Username string `json:"username"`
	// Email is the identity's email address. It is always sent, and is
	// null when nil.
	Email *string `json:"email"`
	// PublicKey is the identity's Ed25519 public key, written as strict
	// base32.
	PublicKey PublicKey `json:"public_key"`
	// Timestamp is the signing time in RFC 3339 UTC with whole seconds and
	// a Z suffix, as in 2026-11-24T09:30:00Z.
	Timestamp string `json:"timestamp"`
	// Signature is the unpadded base32 of the Ed25519 signature of
	// [RegistrationMessage] over the three fields above.
	Signature string `json:"signature"`
}

// RegistrationMessage returns the text a registration signs: the
// [RegistrationDomain] line followed by username, publicKey and timestamp,
// joined by \n with no trailing newline. publicKey and timestamp are the
// base32 key and the timestamp exactly as the body carries them.
func RegistrationMessage(username, publicKey, timestamp string) []byte {
	b := make([]byte, 0, len(RegistrationDomain)+len(username)+len(publicKey)+len(timestamp)+3)
	b = append(b, RegistrationDomain...)
	for _, line := range [...]string{username, publicKey, timestamp} {
		b = append(b, '\n')
		b = append(b, line...)
	}
	return b
}

// SignRegistration builds and signs the [Registration] for id at the
// instant at, which is written in UTC with its fraction of a second
// dropped. The caller supplies at, so no clock is read here.
//
// Only a [TypeUser] identity can register, because registering creates a
// tenant; any other type is an error wrapping [ErrNotRegistrable]. s must
// sign with id's key, or the result is an error wrapping [ErrKeyMismatch].
// An error from s is returned wrapped. id.Email of "" becomes a null email.
func SignRegistration(ctx context.Context, id Identity, s Signer, at time.Time) (Registration, error) {
	if id.Type != TypeUser {
		return Registration{}, fmt.Errorf("%w: %q is a %q identity, not %q", ErrNotRegistrable, id.Name, string(id.Type), string(TypeUser))
	}
	if s == nil {
		return Registration{}, fmt.Errorf("identity: sign registration: nil Signer")
	}
	if got := s.PublicKey(); got != id.PublicKey {
		return Registration{}, fmt.Errorf("%w: signer has %s, identity %q has %s", ErrKeyMismatch, got, id.Name, id.PublicKey)
	}

	r := Registration{
		Username:  id.Name,
		PublicKey: id.PublicKey,
		Timestamp: chrono.FormatSeconds(at),
	}
	if id.Email != "" {
		email := id.Email
		r.Email = &email
	}

	sig, err := s.Sign(ctx, RegistrationMessage(r.Username, r.PublicKey.String(), r.Timestamp))
	if err != nil {
		return Registration{}, fmt.Errorf("identity: sign registration: %w", err)
	}
	if len(sig) != SignatureSize {
		return Registration{}, fmt.Errorf("identity: sign registration: signature is %d bytes, want %d", len(sig), SignatureSize)
	}
	r.Signature = ident.EncodeBase32(sig)
	return r, nil
}
