package projectidentity_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"lukechampine.com/blake3"

	"github.com/z5labs/atomic-go/canonical"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/identity"
	"github.com/z5labs/atomic-go/projectidentity"
)

// RFC 8032 §7.1 TEST 1 and TEST 2 seeds.
const (
	seed1 = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
	seed2 = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
)

// boundExample is the identity member of the bound response of
// docs/remote/endpoints/project.md#fetch_project_identity, with the
// project id written out as its 52 A's and the response's white space.
var boundExample = `{"@context": {"atomic": "https://atomic.dev/ns/workflow#", "dcterms": "http://purl.org/dc/terms/"}, "@type": "atomic:ProjectIdentity", "atomic:projectId": "` +
	strings.Repeat("A", ident.Base32Len) +
	`", "atomic:schemaVersion": 1, "dcterms:created": "2026-10-26T00:00:00Z"}`

// boundExampleJCS is boundExample's RFC 8785 canonical form, written out by
// hand: members sorted, white space removed.
var boundExampleJCS = `{"@context":{"atomic":"https://atomic.dev/ns/workflow#","dcterms":"http://purl.org/dc/terms/"},"@type":"atomic:ProjectIdentity","atomic:projectId":"` +
	strings.Repeat("A", ident.Base32Len) +
	`","atomic:schemaVersion":1,"dcterms:created":"2026-10-26T00:00:00Z"}`

// boundExampleHash is boundExample's hash, frozen.
const boundExampleHash = "aebe4782d680b2e9f6160b5e9e169c04a6f2572a65bd5929dec4909f50709c85"

func signerFromHex(t *testing.T, seed string) identity.Signer {
	t.Helper()
	b, err := hex.DecodeString(seed)
	require.NoError(t, err)
	s, err := identity.SignerFromSeed(b)
	require.NoError(t, err)
	return s
}

func parse(t *testing.T, s string) projectidentity.Document {
	t.Helper()
	d, err := projectidentity.Parse([]byte(s))
	require.NoError(t, err)
	return d
}

func sign(t *testing.T, d projectidentity.Document, s identity.Signer) projectidentity.Document {
	t.Helper()
	signed, err := projectidentity.Sign(context.Background(), d, s)
	require.NoError(t, err)
	return signed
}

func TestBoundExample(t *testing.T) {
	d := parse(t, boundExample)

	t.Run("its project id is the zero id", func(t *testing.T) {
		id, err := d.ProjectID()
		require.NoError(t, err)
		require.True(t, id.IsZero())
	})

	t.Run("its hash is frozen", func(t *testing.T) {
		h, err := d.Hash()
		require.NoError(t, err)
		require.Equal(t, boundExampleHash, h.Hex())
	})

	t.Run("its hash is BLAKE3 of the domain, a zero byte and the JCS", func(t *testing.T) {
		in := append([]byte(projectidentity.HashDomain+"\x00"), boundExampleJCS...)
		want := blake3.Sum256(in)

		h, err := d.Hash()
		require.NoError(t, err)
		require.Equal(t, want[:], h[:])
	})

	t.Run("it has no issuer", func(t *testing.T) {
		_, ok, err := d.Issuer()
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("it has no proof", func(t *testing.T) {
		err := d.VerifyProof(signerFromHex(t, seed1).PublicKey())
		require.ErrorIs(t, err, canonical.ErrProof)
	})

	t.Run("its @type is DocumentType", func(t *testing.T) {
		var m map[string]any
		require.NoError(t, json.Unmarshal(d.Bytes(), &m))
		require.Equal(t, projectidentity.DocumentType, m["@type"])
	})
}

func TestParse(t *testing.T) {
	t.Run("keeps the bytes, white space included", func(t *testing.T) {
		in := "\n  " + boundExample + "\t\n"
		d := parse(t, in)
		require.Equal(t, in, string(d.Bytes()))
	})

	t.Run("copies its input", func(t *testing.T) {
		in := []byte(boundExample)
		d, err := projectidentity.Parse(in)
		require.NoError(t, err)
		in[0] = 'X'
		require.Equal(t, boundExample, string(d.Bytes()))
	})

	t.Run("Bytes returns a copy", func(t *testing.T) {
		d := parse(t, boundExample)
		b := d.Bytes()
		b[0] = 'X'
		require.Equal(t, boundExample, string(d.Bytes()))
	})

	t.Run("does not check the members", func(t *testing.T) {
		d := parse(t, `{"@type": "something else"}`)
		_, err := d.ProjectID()
		require.ErrorIs(t, err, projectidentity.ErrInvalid)
	})

	for name, in := range map[string]string{
		"an array":          `[` + boundExample + `]`,
		"a string":          `"atomic:ProjectIdentity"`,
		"null":              `null`,
		"empty input":       ``,
		"two objects":       boundExample + boundExample,
		"a duplicate name":  `{"@type": "a", "@type": "b"}`,
		"a fraction":        `{"atomic:schemaVersion": 1.5}`,
		"truncated":         boundExample[:len(boundExample)-1],
		"invalid UTF-8":     "{\"a\": \"\xff\"}",
		"trailing non-JSON": boundExample + " x",
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			_, err := projectidentity.Parse([]byte(in))
			require.ErrorIs(t, err, projectidentity.ErrInvalid)
			require.ErrorIs(t, err, canonical.ErrNotAdmitted)
		})
	}
}

func TestDocumentJSON(t *testing.T) {
	t.Run("MarshalJSON keeps white space", func(t *testing.T) {
		in := "{ \"atomic:projectId\" :\n  \"" + strings.Repeat("A", ident.Base32Len) + "\" }"
		d := parse(t, in)
		b, err := d.MarshalJSON()
		require.NoError(t, err)
		require.Equal(t, in, string(b))
	})

	t.Run("MarshalJSON refuses the zero Document", func(t *testing.T) {
		_, err := projectidentity.Document{}.MarshalJSON()
		require.ErrorIs(t, err, projectidentity.ErrInvalid)
	})

	t.Run("UnmarshalJSON keeps the member's bytes", func(t *testing.T) {
		body := `{"bound": true, "hash": "ab", "identity": ` + boundExample + `}`
		var resp struct {
			Identity projectidentity.Document `json:"identity"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &resp))
		require.Equal(t, boundExample, string(resp.Identity.Bytes()))
	})

	t.Run("UnmarshalJSON refuses an array and leaves the Document unchanged", func(t *testing.T) {
		d := parse(t, boundExample)
		err := d.UnmarshalJSON([]byte(`[1]`))
		require.ErrorIs(t, err, projectidentity.ErrInvalid)
		require.Equal(t, boundExample, string(d.Bytes()))
	})

	t.Run("UnmarshalJSON refuses null", func(t *testing.T) {
		var resp struct {
			Identity projectidentity.Document `json:"identity"`
		}
		err := json.Unmarshal([]byte(`{"identity": null}`), &resp)
		require.ErrorIs(t, err, projectidentity.ErrInvalid)
	})
}

func TestZeroDocument(t *testing.T) {
	var d projectidentity.Document
	require.Nil(t, d.Bytes())

	_, err := d.ProjectID()
	require.ErrorIs(t, err, projectidentity.ErrInvalid)

	_, err = d.Hash()
	require.ErrorIs(t, err, projectidentity.ErrInvalid)

	_, ok, err := d.Issuer()
	require.ErrorIs(t, err, projectidentity.ErrInvalid)
	require.False(t, ok)

	err = d.VerifyProof(signerFromHex(t, seed1).PublicKey())
	require.ErrorIs(t, err, projectidentity.ErrInvalid)
	require.NotErrorIs(t, err, canonical.ErrProof)

	_, err = projectidentity.Sign(context.Background(), d, signerFromHex(t, seed1))
	require.ErrorIs(t, err, projectidentity.ErrInvalid)
}

func TestDocumentProjectID(t *testing.T) {
	var id projectidentity.ProjectID
	for i := range id {
		id[i] = byte(255 - i)
	}

	t.Run("reads the member", func(t *testing.T) {
		got, err := parse(t, `{"atomic:projectId": "`+id.String()+`"}`).ProjectID()
		require.NoError(t, err)
		require.Equal(t, id, got)
	})

	for name, in := range map[string]string{
		"missing":      `{}`,
		"not a string": `{"atomic:projectId": 1}`,
		"null":         `{"atomic:projectId": null}`,
		"lower case":   `{"atomic:projectId": "` + strings.ToLower(id.String()) + `"}`,
		"too short":    `{"atomic:projectId": "AAAA"}`,
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			_, err := parse(t, in).ProjectID()
			require.ErrorIs(t, err, projectidentity.ErrInvalid)
		})
	}
}

func TestDocumentIssuer(t *testing.T) {
	key := signerFromHex(t, seed1).PublicKey()

	t.Run("reads the @id of the object", func(t *testing.T) {
		did, ok, err := parse(t, `{"atomic:issuer": {"@id": "`+key.AtomicDID().String()+`"}}`).Issuer()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, key.AtomicDID(), did)
	})

	t.Run("accepts a did:key", func(t *testing.T) {
		did, ok, err := parse(t, `{"atomic:issuer": {"@id": "`+key.KeyDID().String()+`"}}`).Issuer()
		require.NoError(t, err)
		require.True(t, ok)
		require.True(t, did.Matches(key))
	})

	t.Run("null is no issuer", func(t *testing.T) {
		_, ok, err := parse(t, `{"atomic:issuer": null}`).Issuer()
		require.NoError(t, err)
		require.False(t, ok)
	})

	for name, in := range map[string]string{
		"a string":         `{"atomic:issuer": "` + key.AtomicDID().String() + `"}`,
		"an array":         `{"atomic:issuer": [{"@id": "` + key.AtomicDID().String() + `"}]}`,
		"no @id":           `{"atomic:issuer": {}}`,
		"a non-string @id": `{"atomic:issuer": {"@id": 1}}`,
		"a non-DID @id":    `{"atomic:issuer": {"@id": "https://example.com"}}`,
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			_, ok, err := parse(t, in).Issuer()
			require.ErrorIs(t, err, projectidentity.ErrInvalid)
			require.False(t, ok)
		})
	}

	t.Run("a non-DID @id wraps ErrInvalidDID", func(t *testing.T) {
		_, _, err := parse(t, `{"atomic:issuer": {"@id": "did:atomic:nope"}}`).Issuer()
		require.ErrorIs(t, err, identity.ErrInvalidDID)
	})
}

func TestSign(t *testing.T) {
	signer1 := signerFromHex(t, seed1)
	signer2 := signerFromHex(t, seed2)
	d := parse(t, boundExample)
	signed := sign(t, d, signer1)

	t.Run("VerifyProof passes with the TEST 1 key", func(t *testing.T) {
		require.NoError(t, signed.VerifyProof(signer1.PublicKey()))
	})

	t.Run("VerifyProof fails with the TEST 2 key", func(t *testing.T) {
		err := signed.VerifyProof(signer2.PublicKey())
		require.ErrorIs(t, err, canonical.ErrProof)
	})

	t.Run("VerifyProof fails after an edit", func(t *testing.T) {
		edited := bytes.Replace(signed.Bytes(), []byte("2026-10-26T00:00:00Z"), []byte("2026-10-27T00:00:00Z"), 1)
		require.NotEqual(t, signed.Bytes(), edited)
		err := parse(t, string(edited)).VerifyProof(signer1.PublicKey())
		require.ErrorIs(t, err, canonical.ErrProof)
	})

	t.Run("VerifyProof fails after the issuer is edited", func(t *testing.T) {
		edited := bytes.Replace(signed.Bytes(),
			[]byte(signer1.PublicKey().AtomicDID().String()),
			[]byte(signer2.PublicKey().AtomicDID().String()), 1)
		require.NotEqual(t, signed.Bytes(), edited)
		err := parse(t, string(edited)).VerifyProof(signer1.PublicKey())
		require.ErrorIs(t, err, canonical.ErrProof)
	})

	t.Run("Issuer is the TEST 1 did:atomic", func(t *testing.T) {
		did, ok, err := signed.Issuer()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, signer1.PublicKey().AtomicDID(), did)
		require.Equal(t, identity.DIDMethod("atomic"), did.Method())
	})

	t.Run("writes the issuer as an object", func(t *testing.T) {
		var m map[string]any
		require.NoError(t, json.Unmarshal(signed.Bytes(), &m))
		require.Equal(t, map[string]any{"@id": signer1.PublicKey().AtomicDID().String()}, m["atomic:issuer"])
	})

	t.Run("Bytes are the JCS form of the result", func(t *testing.T) {
		doc, err := canonical.Admit(signed.Bytes())
		require.NoError(t, err)
		want, err := doc.Canonical()
		require.NoError(t, err)
		require.Equal(t, string(want), string(signed.Bytes()))
		require.True(t, doc.Has("proof"))
	})

	t.Run("adds a bare proof", func(t *testing.T) {
		doc, err := canonical.Admit(signed.Bytes())
		require.NoError(t, err)
		require.False(t, doc.Has("contentHash"))
		require.False(t, doc.Has("attributedTo"))

		p, err := canonical.ProofOf(doc)
		require.NoError(t, err)
		require.Equal(t, canonical.ProofType, p.Type)
		require.Equal(t, signer1.PublicKey().AtomicDID().VerificationMethod(), p.VerificationMethod)
	})

	t.Run("keeps the project id", func(t *testing.T) {
		id, err := signed.ProjectID()
		require.NoError(t, err)
		require.True(t, id.IsZero())
	})

	t.Run("the proof does not affect Hash", func(t *testing.T) {
		doc, err := canonical.Admit(signed.Bytes())
		require.NoError(t, err)
		unsigned, err := doc.Without("proof").Canonical()
		require.NoError(t, err)

		want, err := parse(t, string(unsigned)).Hash()
		require.NoError(t, err)
		got, err := signed.Hash()
		require.NoError(t, err)
		require.Equal(t, want, got)

		resigned := sign(t, parse(t, string(unsigned)), signer1)
		got, err = resigned.Hash()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("the issuer does affect Hash", func(t *testing.T) {
		before, err := d.Hash()
		require.NoError(t, err)
		after, err := signed.Hash()
		require.NoError(t, err)
		require.NotEqual(t, before, after)
	})

	t.Run("re-signing replaces the issuer and the proof", func(t *testing.T) {
		resigned := sign(t, signed, signer2)
		require.NoError(t, resigned.VerifyProof(signer2.PublicKey()))
		require.ErrorIs(t, resigned.VerifyProof(signer1.PublicKey()), canonical.ErrProof)

		did, ok, err := resigned.Issuer()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, signer2.PublicKey().AtomicDID(), did)
	})

	t.Run("leaves its input unchanged", func(t *testing.T) {
		require.Equal(t, boundExample, string(d.Bytes()))
		_, ok, err := d.Issuer()
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("is deterministic", func(t *testing.T) {
		require.Equal(t, signed.Bytes(), sign(t, d, signer1).Bytes())
	})

	t.Run("refuses a nil signer", func(t *testing.T) {
		_, err := projectidentity.Sign(context.Background(), d, nil)
		require.ErrorIs(t, err, projectidentity.ErrInvalid)
	})

	t.Run("returns the signer's error", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := projectidentity.Sign(context.Background(), d, failingSigner{key: signer1.PublicKey(), err: boom})
		require.ErrorIs(t, err, boom)
	})

	t.Run("refuses a signer whose signatures are not its key's", func(t *testing.T) {
		_, err := projectidentity.Sign(context.Background(), d, mismatchedSigner{key: signer2.PublicKey(), signer: signer1})
		require.ErrorIs(t, err, canonical.ErrProof)
	})
}

// failingSigner fails every signature.
type failingSigner struct {
	key identity.PublicKey
	err error
}

func (s failingSigner) PublicKey() identity.PublicKey { return s.key }

func (s failingSigner) Sign(context.Context, []byte) ([]byte, error) { return nil, s.err }

// mismatchedSigner claims key and signs with signer.
type mismatchedSigner struct {
	key    identity.PublicKey
	signer identity.Signer
}

func (s mismatchedSigner) PublicKey() identity.PublicKey { return s.key }

func (s mismatchedSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	return s.signer.Sign(ctx, msg)
}
