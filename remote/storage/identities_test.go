package storage_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/remote"
	"github.com/z5labs/atomic-go/remote/remotetest"
	"github.com/z5labs/atomic-go/remote/serverurl"
	"github.com/z5labs/atomic-go/remote/storage"
)

// seed1 is the private key seed of RFC 8032 section 7.1 TEST 1.
const seed1 = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

// keyFromSeed returns the public key of the Ed25519 seed written in hex.
func keyFromSeed(t *testing.T, seedHex string) identity.PublicKey {
	t.Helper()
	seed, err := hex.DecodeString(seedHex)
	require.NoError(t, err)
	k, err := identity.PublicKeyFromBytes(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	require.NoError(t, err)
	return k
}

// identityBody is the IdentityInfo of the docs' envelope example
// (docs/remote/endpoints/storage.md#response-envelope) with its public key
// member spelled member and holding key.
func identityBody(member, key string) string {
	return `{"id": "00000000-0000-0000-0000-000000000001", "name": "remote signer", "status": "active", "createdAt": "2026-10-15T00:00:00Z", "` + member + `": "` + key + `"}`
}

func wantIdentity(key *string) storage.IdentityInfo {
	return storage.IdentityInfo{
		ID:        uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Name:      "remote signer",
		Status:    "active",
		CreatedAt: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
		PublicKey: key,
	}
}

func TestIdentityInfoReadsTheDocsReply(t *testing.T) {
	for _, member := range []string{"publicKey", "public_key"} {
		t.Run(member, func(t *testing.T) {
			c, _ := serve(t, "GET /identities/by-key/{key}", raw(http.StatusOK, envelope(identityBody(member, "..."))))
			info, err := c.Identities().ByPublicKey(t.Context(), "KEY")
			require.NoError(t, err)
			require.Equal(t, wantIdentity(ptr("...")), info)
		})
	}
}

func TestIdentityInfoWithoutAPublicKey(t *testing.T) {
	for name, body := range map[string]string{
		"absent": `{"id": "00000000-0000-0000-0000-000000000001", "name": "remote signer", "status": "active", "createdAt": "2026-10-15T00:00:00Z"}`,
		"null":   `{"id": "00000000-0000-0000-0000-000000000001", "name": "remote signer", "status": "active", "createdAt": "2026-10-15T00:00:00Z", "publicKey": null}`,
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, "GET /identities/by-key/{key}", raw(http.StatusOK, envelope(body)))
			info, err := c.Identities().ByPublicKey(t.Context(), "KEY")
			require.NoError(t, err)
			require.Equal(t, wantIdentity(nil), info)
		})
	}
}

func TestIdentityInfoRefusesBadBodies(t *testing.T) {
	tests := map[string]string{
		"missing status":     `{"id": "00000000-0000-0000-0000-000000000001", "name": "n", "createdAt": "2026-10-15T00:00:00Z"}`,
		"snake created_at":   `{"id": "00000000-0000-0000-0000-000000000001", "name": "n", "status": "active", "created_at": "2026-10-15T00:00:00Z"}`,
		"both key spellings": `{"id": "00000000-0000-0000-0000-000000000001", "name": "n", "status": "active", "createdAt": "2026-10-15T00:00:00Z", "publicKey": "A", "public_key": "A"}`,
		"id not a uuid":      `{"id": "one", "name": "n", "status": "active", "createdAt": "2026-10-15T00:00:00Z"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			c, _ := serve(t, "GET /identities/by-key/{key}", raw(http.StatusOK, envelope(body)))
			_, err := c.Identities().ByPublicKey(t.Context(), "KEY")
			var rerr *remote.Error
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, remote.KindProtocol, rerr.Kind)
		})
	}
}

func TestIdentitiesRequestURIs(t *testing.T) {
	key := keyFromSeed(t, seed1)
	body := envelope(identityBody("publicKey", key.String()))
	atomicDID := key.AtomicDID()

	tests := map[string]struct {
		send func(storage.Identities) (storage.IdentityInfo, error)
		want string
	}{
		"by email": {
			send: func(ids storage.Identities) (storage.IdentityInfo, error) {
				return ids.ResolveByEmail(t.Context(), "alice@example.com")
			},
			want: "/identities/resolve?email=alice%40example.com",
		},
		"by name": {
			send: func(ids storage.Identities) (storage.IdentityInfo, error) {
				return ids.ResolveByName(t.Context(), "a b")
			},
			want: "/identities/resolve?name=a%20b",
		},
		"by base32 key": {
			send: func(ids storage.Identities) (storage.IdentityInfo, error) {
				return ids.ByPublicKey(t.Context(), key.String())
			},
			want: "/identities/by-key/" + key.String(),
		},
		"by key with a did": {
			send: func(ids storage.Identities) (storage.IdentityInfo, error) {
				return ids.ByPublicKey(t.Context(), "did:atomic:abc")
			},
			want: "/identities/by-key/did:atomic:abc",
		},
		"by did": {
			send: func(ids storage.Identities) (storage.IdentityInfo, error) {
				return ids.ByDID(t.Context(), atomicDID)
			},
			want: "/identities/by-did/did%3Aatomic%3A" + strings.TrimPrefix(atomicDID.String(), "did:atomic:"),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "GET /identities/", raw(http.StatusOK, body))
			_, err := tt.send(c.Identities())
			require.NoError(t, err)
			require.Equal(t, 1, got.calls)
			require.Equal(t, http.MethodGet, got.method)
			require.Equal(t, tt.want, got.requestURI)
			require.Empty(t, got.body)
		})
	}
	require.Len(t, key.String(), 52)
}

func TestIdentitiesByPublicKeyRefusesAnotherPath(t *testing.T) {
	for _, key := range []string{"", "..", "a/b", "a?b", "did:atomic:x#y"} {
		c, got := serve(t, "/", raw(http.StatusOK, envelope(identityBody("publicKey", "K"))))
		_, err := c.Identities().ByPublicKey(t.Context(), key)
		require.ErrorIs(t, err, storage.ErrInvalidArgument, key)
		require.Zero(t, got.calls, key)
	}
}

func TestIdentitiesByDIDAcceptsTheMatchingKey(t *testing.T) {
	key := keyFromSeed(t, seed1)
	for name, did := range map[string]identity.DID{"did:atomic": key.AtomicDID(), "did:key": key.KeyDID()} {
		t.Run(name, func(t *testing.T) {
			c, got := serve(t, "GET /identities/by-did/{did}", raw(http.StatusOK, envelope(identityBody("publicKey", key.String()))))
			info, err := c.Identities().ByDID(t.Context(), did)
			require.NoError(t, err)
			require.Equal(t, wantIdentity(ptr(key.String())), info)
			require.Equal(t, "/identities/by-did/"+strings.ReplaceAll(did.String(), ":", "%3A"), got.requestURI)
		})
	}
}

func TestIdentitiesByDIDRefusesAnUnboundKey(t *testing.T) {
	key := keyFromSeed(t, seed1)
	stranger := keyFromSeed(t, "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb")
	noKey := `{"id": "00000000-0000-0000-0000-000000000001", "name": "remote signer", "status": "active", "createdAt": "2026-10-15T00:00:00Z"}`

	tests := map[string]struct {
		body string
		want error
	}{
		"a stranger's key":  {body: identityBody("publicKey", stranger.String()), want: storage.ErrKeyNotBound},
		"a missing key":     {body: noKey, want: storage.ErrKeyNotBound},
		"an unparsable key": {body: identityBody("publicKey", "not-a-key"), want: identity.ErrInvalidKey},
	}
	for name, tt := range tests {
		for didName, did := range map[string]identity.DID{"did:atomic": key.AtomicDID(), "did:key": key.KeyDID()} {
			t.Run(name+"/"+didName, func(t *testing.T) {
				c, _ := serve(t, "GET /identities/by-did/{did}", raw(http.StatusOK, envelope(tt.body)))
				info, err := c.Identities().ByDID(t.Context(), did)
				require.Zero(t, info)
				var rerr *remote.Error
				require.ErrorAs(t, err, &rerr)
				require.Equal(t, remote.KindProtocol, rerr.Kind)
				require.Equal(t, "resolve_identity_by_did", rerr.Op)
				require.Equal(t, http.StatusOK, rerr.StatusCode)
				require.ErrorIs(t, err, tt.want)
			})
		}
	}
}

func TestIdentitiesByDIDRefusesTheZeroDID(t *testing.T) {
	c, got := serve(t, "/", raw(http.StatusOK, envelope(identityBody("publicKey", "K"))))
	_, err := c.Identities().ByDID(t.Context(), identity.DID{})
	require.ErrorIs(t, err, storage.ErrInvalidArgument)
	require.Zero(t, got.calls)
}

func TestIdentitiesNotFound(t *testing.T) {
	c, _ := serve(t, "/", raw(http.StatusNotFound, `{"success": false, "error": {"code": "NOT_FOUND", "message": "identity not found"}}`))
	_, err := c.Identities().ResolveByEmail(t.Context(), "nobody@example.com")
	require.ErrorIs(t, err, remote.ErrNotFound)
}

// TestSignerDiscoveryRequest sends the lookup signer discovery sends after
// a pull, on a Client built for the project URL's origin
// (docs/remote/flows.md#request-3-signer-discovery).
func TestSignerDiscoveryRequest(t *testing.T) {
	key := keyFromSeed(t, seed1)
	did := key.AtomicDID()
	require.True(t, strings.HasPrefix(did.String(), "did:atomic:NQYQ"), did.String())

	proj, _, err := serverurl.ParseProject("https://alice.atomic.storage/workspaces/w/projects/p/code")
	require.NoError(t, err)

	var gotURL, gotMethod string
	var gotHeader http.Header
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURL = "https://" + r.Host + r.RequestURI
		gotHeader = r.Header.Clone()
		raw(http.StatusOK, envelope(identityBody("publicKey", key.String())))(w, r)
	})
	c, err := storage.New(proj.Origin(), remotetest.Client(h), remotetest.NewLogRecorder().Logger(), "0.22.1")
	require.NoError(t, err)

	info, err := c.Identities().ByPublicKey(t.Context(), did.String())
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "https://alice.atomic.storage/identities/by-key/"+did.String(), gotURL)
	require.Equal(t, []string{"application/json"}, gotHeader.Values("Content-Type"))
	require.Equal(t, []string{"atomic/0.22.1"}, gotHeader.Values("User-Agent"))

	require.NotNil(t, info.PublicKey)
	got, err := identity.ParsePublicKey(*info.PublicKey)
	require.NoError(t, err)
	require.True(t, did.Matches(got))
}
