package storagetest_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/z5labs/atomic-go/remote/storage/storagetest"
)

func TestData(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, storagetest.Data(rec, http.StatusCreated, map[string]any{"name": "<w>", "n": 1}))
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"success":true,"data":{"name":"<w>","n":1}}`, rec.Body.String())
	require.Contains(t, rec.Body.String(), "<w>", "no HTML escaping")
}

func TestDataKeepsEmptyAndNullData(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, storagetest.Data(rec, http.StatusOK, []string{}))
	require.JSONEq(t, `{"success":true,"data":[]}`, rec.Body.String())

	rec = httptest.NewRecorder()
	require.NoError(t, storagetest.Data(rec, http.StatusOK, nil))
	require.JSONEq(t, `{"success":true,"data":null}`, rec.Body.String())
}

func TestDataThatDoesNotMarshal(t *testing.T) {
	rec := httptest.NewRecorder()
	require.Error(t, storagetest.Data(rec, http.StatusOK, make(chan int)))
	require.Zero(t, rec.Body.Len())
	require.False(t, rec.Flushed)
}

func TestFail(t *testing.T) {
	rec := httptest.NewRecorder()
	require.NoError(t, storagetest.Fail(rec, http.StatusNotFound, "NOT_FOUND", "workspace not found"))
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.JSONEq(t, `{"success":false,"error":{"code":"NOT_FOUND","message":"workspace not found"}}`, rec.Body.String())
}

func TestRaw(t *testing.T) {
	rec := httptest.NewRecorder()
	storagetest.Raw(rec, http.StatusBadGateway, "Bad Gateway")
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "Bad Gateway", rec.Body.String())
}
