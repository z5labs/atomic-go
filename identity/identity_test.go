package identity_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/internal/textenc"
)

// RFC 8032 §7.1 TEST 1 and TEST 2, with the base32 and DID forms derived by
// the rules of docs/remote/auth.md#did-forms.
var vectors = []struct {
	name, seed, key, base32, atomicDID, keyDID string
}{
	{
		name:      "RFC 8032 TEST 1",
		seed:      "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60",
		key:       "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
		base32:    "25NJQAMCWEFLPVKL73J4SZAHHIHOC4XT3KTCGJNPAINGR5YHKENA",
		atomicDID: "did:atomic:NQYQIETI6RYWBHDZ6XZNXTBY4SSKWL2NIFQQTJHAT7HVB7IPABRA",
		keyDID:    "did:key:z6MktwupdmLXVVqTzCw4i46r4uGyosGXRnR3XjN4Zq7oMMsw",
	},
	{
		name:      "RFC 8032 TEST 2",
		seed:      "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb",
		key:       "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c",
		base32:    "HVABPQ7IIOEVVEVXBKTU2G36XSOJQLGPF3CJNDGAZVK7CKXUMYGA",
		atomicDID: "did:atomic:CAT6ANNSNNQF3RWUW6GQPXBJMYH4YNEYWWMKFZL4JZVRWZZ2D2KQ",
		keyDID:    "did:key:z6MkiaMbhXHNA4eJVCCj8dbzKzTgYDKf6crKgHVHid1F1WCT",
	},
}

// The author key of docs/remote/change-format/sections.md#changeheader-example
// and the signer_did #changesignature gives for it.
const (
	docsKey       = "TKX4DF2DBALZPZRTUORRAZ74CX5BTFCLMBIRPF27PWYD3TDFUNYQ"
	docsAtomicDID = "did:atomic:45LZQUIZNX5VFO7UXZHBOU4LMUB6U62NZEUN6QP2VPQ7ANLLTC2Q"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return b
}

func TestKeyVectors(t *testing.T) {
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			priv := ed25519.NewKeyFromSeed(mustHex(t, v.seed))
			k, err := identity.PublicKeyFromBytes(priv.Public().(ed25519.PublicKey))
			require.NoError(t, err)
			require.Equal(t, mustHex(t, v.key), k[:])

			t.Run("base32", func(t *testing.T) {
				require.Equal(t, v.base32, k.String())
				parsed, err := identity.ParsePublicKey(v.base32)
				require.NoError(t, err)
				require.Equal(t, k, parsed)

				text, err := k.MarshalText()
				require.NoError(t, err)
				require.Equal(t, v.base32, string(text))
				appended, err := k.AppendText([]byte("x"))
				require.NoError(t, err)
				require.Equal(t, "x"+v.base32, string(appended))

				var u identity.PublicKey
				require.NoError(t, u.UnmarshalText([]byte(v.base32)))
				require.Equal(t, k, u)
			})

			t.Run("ed25519", func(t *testing.T) {
				require.Equal(t, priv.Public(), k.Ed25519())
				msg := []byte("atomic")
				sig := ed25519.Sign(priv, msg)
				require.True(t, k.Verify(msg, sig))
				require.False(t, k.Verify([]byte("atomics"), sig))
				require.False(t, k.Verify(msg, sig[:identity.SignatureSize-1]))

				// The returned key is a copy.
				pub := k.Ed25519()
				pub[0] ^= 0xff
				require.Equal(t, mustHex(t, v.key), k[:])
			})

			t.Run("identity id", func(t *testing.T) {
				id := k.ID()
				require.Equal(t, ident.Sum(k[:]), ident.Hash(id))
				wantID := strings.TrimPrefix(v.atomicDID, "did:atomic:")
				require.Equal(t, wantID, id.String())

				parsed, err := identity.ParseID(wantID)
				require.NoError(t, err)
				require.Equal(t, id, parsed)

				text, err := id.MarshalText()
				require.NoError(t, err)
				require.Equal(t, wantID, string(text))
				var u identity.ID
				require.NoError(t, u.UnmarshalText(text))
				require.Equal(t, id, u)

				require.Equal(t, k.AtomicDID(), id.DID())
			})

			t.Run("did:atomic", func(t *testing.T) {
				d := k.AtomicDID()
				require.Equal(t, v.atomicDID, d.String())
				require.Equal(t, identity.MethodAtomic, d.Method())
				require.Equal(t, k.ID(), d.ID())
				_, ok := d.PublicKey()
				require.False(t, ok)
				require.Equal(t, v.atomicDID+"#key-1", d.VerificationMethod())

				parsed, err := identity.ParseDID(v.atomicDID)
				require.NoError(t, err)
				require.Equal(t, d, parsed)

				vm, err := identity.ParseVerificationMethod(v.atomicDID + "#key-1")
				require.NoError(t, err)
				require.Equal(t, d, vm)
			})

			t.Run("did:key", func(t *testing.T) {
				d := k.KeyDID()
				require.Equal(t, v.keyDID, d.String())
				require.True(t, strings.HasPrefix(d.String(), "did:key:z6Mk"))
				require.Equal(t, identity.MethodKey, d.Method())
				require.Equal(t, k.ID(), d.ID())
				got, ok := d.PublicKey()
				require.True(t, ok)
				require.Equal(t, k, got)
				require.Equal(t, v.keyDID+"#key-1", d.VerificationMethod())

				parsed, err := identity.ParseDID(v.keyDID)
				require.NoError(t, err)
				require.Equal(t, d, parsed)
				got, ok = parsed.PublicKey()
				require.True(t, ok)
				require.Equal(t, k, got)

				vm, err := identity.ParseVerificationMethod(v.keyDID + "#key-1")
				require.NoError(t, err)
				require.Equal(t, d, vm)
			})

			t.Run("json", func(t *testing.T) {
				type doc struct {
					Key    identity.PublicKey `json:"key"`
					ID     identity.ID        `json:"id"`
					Atomic identity.DID       `json:"atomic"`
					Key2   identity.DID       `json:"did_key"`
				}
				in := doc{Key: k, ID: k.ID(), Atomic: k.AtomicDID(), Key2: k.KeyDID()}
				b, err := json.Marshal(in)
				require.NoError(t, err)
				require.JSONEq(t, `{"key":"`+v.base32+`","id":"`+k.ID().String()+`","atomic":"`+v.atomicDID+`","did_key":"`+v.keyDID+`"}`, string(b))

				var out doc
				require.NoError(t, json.Unmarshal(b, &out))
				require.Equal(t, in, out)
			})
		})
	}
}

func TestDocsKey(t *testing.T) {
	k, err := identity.ParsePublicKey(docsKey)
	require.NoError(t, err)
	require.Equal(t, docsAtomicDID, k.AtomicDID().String())

	d, err := identity.ParseDID(docsAtomicDID)
	require.NoError(t, err)
	require.True(t, d.Matches(k))
}

func TestZeroKey(t *testing.T) {
	var k identity.PublicKey
	require.Equal(t, strings.Repeat("A", 52), k.String())
	parsed, err := identity.ParsePublicKey(strings.Repeat("A", 52))
	require.NoError(t, err)
	require.Equal(t, k, parsed)
}

func TestPublicKeyFromBytesLength(t *testing.T) {
	for _, n := range []int{0, 31, 33, 64} {
		_, err := identity.PublicKeyFromBytes(make([]byte, n))
		require.ErrorIs(t, err, identity.ErrInvalidKey)
	}
}

// strictCases are base32 texts that a strict decoder refuses, built from the
// TEST 1 key.
func strictCases() map[string]string {
	good := vectors[0].base32
	return map[string]string{
		"lower case":         strings.ToLower(good),
		"one lower case":     good[:2] + strings.ToLower(good[2:3]) + good[3:],
		"padding":            good + "====",
		"padding in place":   good[:51] + "=",
		"line feed":          good + "\n",
		"inner line feed":    good[:26] + "\n" + good[27:],
		"bad last character": good[:51] + "B",
		"51 characters":      good[:51],
		"53 characters":      good + "A",
		"empty":              "",
		"outside alphabet":   good[:10] + "1" + good[11:],
	}
}

func TestStrictParsing(t *testing.T) {
	require.Equal(t, byte('A'), vectors[0].base32[51], "the bad last character case assumes the key ends in A")

	for name, s := range strictCases() {
		t.Run(name, func(t *testing.T) {
			_, err := identity.ParsePublicKey(s)
			require.ErrorIs(t, err, identity.ErrInvalidKey)
			var pe *ident.ParseError
			require.ErrorAs(t, err, &pe)

			_, err = identity.ParseID(s)
			require.ErrorIs(t, err, identity.ErrInvalidID)

			_, err = identity.ParseDID("did:atomic:" + s)
			require.ErrorIs(t, err, identity.ErrInvalidDID)

			var k identity.PublicKey
			require.Error(t, k.UnmarshalText([]byte(s)))
			require.Equal(t, identity.PublicKey{}, k)

			var id identity.ID
			require.Error(t, id.UnmarshalText([]byte(s)))
			require.Equal(t, identity.ID{}, id)
		})
	}
}

func TestMatches(t *testing.T) {
	k1, err := identity.ParsePublicKey(vectors[0].base32)
	require.NoError(t, err)
	k2, err := identity.ParsePublicKey(vectors[1].base32)
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		did  identity.DID
	}{
		{"did:atomic", k1.AtomicDID()},
		{"did:key", k1.KeyDID()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.True(t, tc.did.Matches(k1))
			require.False(t, tc.did.Matches(k2))
		})
	}

	t.Run("the forms differ", func(t *testing.T) {
		require.NotEqual(t, k1.AtomicDID(), k1.KeyDID())
	})

	t.Run("the zero DID matches nothing", func(t *testing.T) {
		var d identity.DID
		require.False(t, d.Matches(k1))
		require.False(t, d.Matches(identity.PublicKey{}))
	})
}

func TestZeroDID(t *testing.T) {
	var d identity.DID
	require.True(t, d.IsZero())
	require.Equal(t, identity.DIDMethod(""), d.Method())
	require.Equal(t, identity.ID{}, d.ID())
	_, ok := d.PublicKey()
	require.False(t, ok)
	require.Equal(t, "", d.String())
	require.Equal(t, "", d.VerificationMethod())

	_, err := d.MarshalText()
	require.ErrorIs(t, err, identity.ErrInvalidDID)
	b, err := d.AppendText([]byte("x"))
	require.ErrorIs(t, err, identity.ErrInvalidDID)
	require.Equal(t, "x", string(b))
	_, err = json.Marshal(d)
	require.ErrorIs(t, err, identity.ErrInvalidDID)

	k, err := identity.ParsePublicKey(vectors[0].base32)
	require.NoError(t, err)
	require.False(t, k.AtomicDID().IsZero())
	require.False(t, k.KeyDID().IsZero())
}

func keyDIDOf(raw []byte) string {
	return "did:key:z" + textenc.EncodeBase58(raw)
}

func TestParseDIDRefuses(t *testing.T) {
	k, err := identity.ParsePublicKey(vectors[0].base32)
	require.NoError(t, err)
	good := vectors[0].keyDID

	t.Run("a multicodec other than ed25519-pub", func(t *testing.T) {
		for name, prefix := range map[string][]byte{
			"secp256k1-pub": {0xe7, 0x01},
			"x25519-pub":    {0xec, 0x01},
			"ED 02":         {0xed, 0x02},
			"none":          {},
		} {
			t.Run(name, func(t *testing.T) {
				raw := append(append([]byte{}, prefix...), k[:]...)
				_, err := identity.ParseDID(keyDIDOf(raw))
				require.ErrorIs(t, err, identity.ErrInvalidDID)
				require.ErrorIs(t, err, identity.ErrNotEd25519)
			})
		}
	})

	for name, s := range map[string]string{
		"no multibase z":        "did:key:" + strings.TrimPrefix(good, "did:key:z"),
		"another multibase":     "did:key:f" + strings.TrimPrefix(good, "did:key:z"),
		"empty did:key":         "did:key:",
		"bare z":                "did:key:z",
		"short key":             keyDIDOf(append([]byte{0xed, 0x01}, k[:31]...)),
		"long key":              keyDIDOf(append(append([]byte{0xed, 0x01}, k[:]...), 0)),
		"only the multicodec":   keyDIDOf([]byte{0xed, 0x01}),
		"one byte":              keyDIDOf([]byte{0xed}),
		"bad base58":            good[:20] + "0" + good[21:],
		"enormous":              "did:key:z" + strings.Repeat("2", 10000),
		"upper case method":     strings.Replace(good, "did:key:", "DID:KEY:", 1),
		"unknown method":        "did:web:example.com",
		"no method":             "did:",
		"empty":                 "",
		"bare key":              vectors[0].base32,
		"verification method":   good + "#key-1",
		"upper case did:atomic": strings.Replace(vectors[0].atomicDID, "did:atomic:", "DID:ATOMIC:", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := identity.ParseDID(s)
			require.ErrorIs(t, err, identity.ErrInvalidDID)

			var d identity.DID
			require.Error(t, d.UnmarshalText([]byte(s)))
			require.True(t, d.IsZero())
		})
	}
}

func TestParseVerificationMethodRefuses(t *testing.T) {
	good := vectors[0].atomicDID
	for name, s := range map[string]string{
		"no fragment":    good,
		"other fragment": good + "#key-2",
		"empty fragment": good + "#",
		"only fragment":  "#key-1",
		"bad did":        "did:web:example.com#key-1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := identity.ParseVerificationMethod(s)
			require.ErrorIs(t, err, identity.ErrInvalidDID)
		})
	}
}

func TestType(t *testing.T) {
	for _, tc := range []struct {
		text      string
		want      identity.Type
		delegated bool
	}{
		{"user", identity.TypeUser, false},
		{"agent", identity.TypeAgent, false},
		{"delegated", identity.TypeDelegated, true},
	} {
		t.Run(tc.text, func(t *testing.T) {
			var ty identity.Type
			require.NoError(t, ty.UnmarshalText([]byte(tc.text)))
			require.Equal(t, tc.want, ty)
			require.Equal(t, tc.delegated, ty.IsDelegated())

			b, err := json.Marshal(ty)
			require.NoError(t, err)
			require.Equal(t, `"`+tc.text+`"`, string(b))
		})
	}

	for _, s := range []string{"", "User", "USER", "Delegated", "human", "agent ", "delegate"} {
		t.Run("refuses "+s, func(t *testing.T) {
			ty := identity.TypeAgent
			require.Error(t, ty.UnmarshalText([]byte(s)))
			require.Equal(t, identity.TypeAgent, ty)
		})
	}

	t.Run("json refuses an unknown type", func(t *testing.T) {
		var v struct {
			Type identity.Type `json:"type"`
		}
		require.Error(t, json.Unmarshal([]byte(`{"type":"admin"}`), &v))
	})
}

func TestIdentity(t *testing.T) {
	k, err := identity.ParsePublicKey(vectors[0].base32)
	require.NoError(t, err)
	id := identity.Identity{Name: "alice", Type: identity.TypeUser, Email: "alice@example.com", PublicKey: k}
	require.False(t, id.Type.IsDelegated())
	require.True(t, id.PublicKey.AtomicDID().Matches(k))
}

func TestSizes(t *testing.T) {
	require.Equal(t, 32, identity.PublicKeySize)
	require.Equal(t, 32, identity.SeedSize)
	require.Equal(t, 64, identity.SignatureSize)
	require.Equal(t, "atomic-storage:register", identity.RegistrationDomain)
}
