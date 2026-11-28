package syncflow_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/changefile"
	"github.com/z5labs/atomic-go/ident"
	"github.com/z5labs/atomic-go/internal/changefixture"
	"github.com/z5labs/atomic-go/remote/syncflow"
	"github.com/z5labs/atomic-go/remote/syncproto"
	"github.com/z5labs/atomic-go/remote/tags"
	"github.com/z5labs/atomic-go/remote/views"
)

func TestChangeKeyWorkedExample(t *testing.T) {
	h, err := changefile.Verify(changefixture.WorkedExample())
	require.NoError(t, err)
	require.Equal(t, changefixture.WorkedExampleHash, syncflow.ChangeKey(h))
}

func TestVerifyKeyChange(t *testing.T) {
	obj := syncproto.ObjectRecord{
		Family: syncproto.Change,
		Key:    changefixture.WorkedExampleHash,
		Bytes:  changefixture.WorkedExample(),
	}

	t.Run("the worked example passes", func(t *testing.T) {
		require.NoError(t, syncflow.VerifyKey(obj))
	})

	t.Run("a lower-case key passes", func(t *testing.T) {
		o := obj
		o.Key = strings.ToLower(o.Key)
		require.NoError(t, syncflow.VerifyKey(o))
	})

	t.Run("a flipped byte fails", func(t *testing.T) {
		o := obj
		o.Bytes = changefixture.WorkedExample()
		o.Bytes[len(o.Bytes)-ident.Size-1] ^= 0x01
		err := syncflow.VerifyKey(o)
		require.ErrorIs(t, err, changefile.ErrHashMismatch)
	})

	t.Run("trailing bytes fail unless allowed", func(t *testing.T) {
		o := obj
		o.Bytes = append(changefixture.WorkedExample(), 0)
		require.ErrorIs(t, syncflow.VerifyKey(o), changefile.ErrTrailingBytes)
		require.NoError(t, syncflow.VerifyKey(o, changefile.AllowTrailingBytes()))
	})

	t.Run("another change's key fails", func(t *testing.T) {
		o := obj
		o.Key = ident.Sum([]byte("another change")).String()
		require.ErrorIs(t, syncflow.VerifyKey(o), syncflow.ErrKeyMismatch)
	})

	t.Run("a key that does not parse fails", func(t *testing.T) {
		o := obj
		o.Key = "not-a-hash"
		err := syncflow.VerifyKey(o)
		require.ErrorIs(t, err, syncflow.ErrKeyMismatch)
		require.ErrorIs(t, err, ident.ErrSyntax)
	})
}

func TestVerifyKeySidecars(t *testing.T) {
	b := []byte("ATTS\x01\x02\x03")
	key := syncflow.SidecarKey(b)
	require.Equal(t, ident.Sum(b).String(), key)

	for _, family := range []syncproto.Family{syncproto.Attest, syncproto.Provenance} {
		t.Run(family.String(), func(t *testing.T) {
			require.NoError(t, syncflow.VerifyKey(syncproto.ObjectRecord{Family: family, Key: key, Bytes: b}))

			flipped := []byte("ATTS\x01\x02\x04")
			err := syncflow.VerifyKey(syncproto.ObjectRecord{Family: family, Key: key, Bytes: flipped})
			require.ErrorIs(t, err, syncflow.ErrKeyMismatch)

			// A sidecar is keyed in base32, not in hex.
			err = syncflow.VerifyKey(syncproto.ObjectRecord{Family: family, Key: ident.Sum(b).Hex(), Bytes: b})
			require.ErrorIs(t, err, syncflow.ErrKeyMismatch)
		})
	}
}

func TestVerifyKeyView(t *testing.T) {
	s := views.FromManifest(views.Manifest{Name: "dev", Scope: ident.ScopeShared, Changes: changes("c1")}, "")
	obj := syncproto.ObjectRecord{Family: syncproto.View, Key: s.Key(), Bytes: s.MarshalCanonical()}

	t.Run("the snapshot key passes", func(t *testing.T) {
		require.NoError(t, syncflow.VerifyKey(obj))
	})

	t.Run("an upper-case hex key passes", func(t *testing.T) {
		o := obj
		o.Key = strings.ToUpper(o.Key)
		require.NotEqual(t, obj.Key, o.Key)
		require.NoError(t, syncflow.VerifyKey(o))
	})

	t.Run("a base32 key fails", func(t *testing.T) {
		o := obj
		o.Key = ident.Sum(obj.Bytes).String()
		require.ErrorIs(t, syncflow.VerifyKey(o), syncflow.ErrKeyMismatch)
	})

	t.Run("other bytes fail", func(t *testing.T) {
		o := obj
		o.Bytes = []byte(strings.Replace(string(obj.Bytes), "shared", "draft", 1))
		require.ErrorIs(t, syncflow.VerifyKey(o), syncflow.ErrKeyMismatch)
	})
}

func TestVerifyKeyTag(t *testing.T) {
	r := tags.Record{
		Name:       "v1",
		View:       "dev",
		Sequence:   3,
		ChangeHash: ident.Sum([]byte("c3")),
		Timestamp:  time.Unix(1700000000, 0).UTC(),
		Kind:       tags.Release,
	}
	b, err := r.MarshalPostcard()
	require.NoError(t, err)
	obj := syncproto.ObjectRecord{Family: syncproto.Tag, Key: r.Key(), Bytes: b}

	t.Run("the record's key passes", func(t *testing.T) {
		require.NoError(t, syncflow.VerifyKey(obj))
	})

	t.Run("the digest of the bytes fails", func(t *testing.T) {
		o := obj
		o.Key = syncflow.SidecarKey(b)
		require.ErrorIs(t, syncflow.VerifyKey(o), syncflow.ErrKeyMismatch)
	})

	t.Run("bytes that do not decode fail", func(t *testing.T) {
		o := obj
		o.Bytes = b[:5]
		require.ErrorIs(t, syncflow.VerifyKey(o), tags.ErrMalformed)
	})
}

func TestVerifyKeyUnverifiable(t *testing.T) {
	for _, family := range []syncproto.Family{syncproto.Conflict, syncproto.Family(9)} {
		t.Run(family.String(), func(t *testing.T) {
			b := []byte("conflict state")
			err := syncflow.VerifyKey(syncproto.ObjectRecord{Family: family, Key: syncflow.SidecarKey(b), Bytes: b})
			require.ErrorIs(t, err, syncflow.ErrUnverifiable)
		})
	}
}

// changes returns a change log of the hashes of names.
func changes(names ...string) []ident.Hash {
	log := make([]ident.Hash, len(names))
	for i, n := range names {
		log[i] = ident.Sum([]byte(n))
	}
	return log
}
