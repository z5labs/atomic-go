package storage

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponseMetadataAliases(t *testing.T) {
	want := responseMetadata{Page: 2, PerPage: 20, Total: 41, TotalPages: 3}
	for _, body := range []string{
		`{"page":2,"perPage":20,"total":41,"totalPages":3}`,
		`{"page":2,"per_page":20,"total":41,"total_pages":3}`,
		`{"page":2,"perPage":20,"total":41,"total_pages":3,"extra":true}`,
	} {
		var got responseMetadata
		require.NoError(t, json.Unmarshal([]byte(body), &got), body)
		require.Equal(t, want, got, body)
	}
}

func TestResponseMetadataRefuses(t *testing.T) {
	for _, body := range []string{
		// A member under both its name and its alias.
		`{"page":2,"perPage":20,"per_page":20,"total":41,"totalPages":3}`,
		// A member written twice.
		`{"page":2,"page":3,"perPage":20,"total":41,"totalPages":3}`,
		// A required member missing, or null.
		`{"page":2,"perPage":20,"total":41}`,
		`{"page":null,"perPage":20,"total":41,"totalPages":3}`,
		// Names match exactly.
		`{"Page":2,"perPage":20,"total":41,"totalPages":3}`,
		`{"page":2,"PerPage":20,"total":41,"totalPages":3}`,
		// Types are checked.
		`{"page":-1,"perPage":20,"total":41,"totalPages":3}`,
		`{"page":"2","perPage":20,"total":41,"totalPages":3}`,
		// Not an object.
		`[]`, `null`, `1`,
	} {
		var got responseMetadata
		require.Error(t, json.Unmarshal([]byte(body), &got), body)
	}
}

func TestAPIError(t *testing.T) {
	var e APIError
	require.NoError(t, json.Unmarshal([]byte(`{"code":"X","message":"m","details":{"field":"name"}}`), &e))
	require.Equal(t, "X", e.Code)
	require.Equal(t, "m", e.Message)
	require.JSONEq(t, `{"field":"name"}`, string(e.Details))

	e = APIError{}
	require.NoError(t, json.Unmarshal([]byte(`{"code":"X","message":"m","details":null}`), &e))
	require.Equal(t, APIError{Code: "X", Message: "m"}, e)

	for _, body := range []string{
		`{"code":"X"}`,
		`{"message":"m"}`,
		`{"code":"X","message":null}`,
		`{"code":1,"message":"m"}`,
		`{"code":"X","message":"m","code":"Y"}`,
	} {
		var e APIError
		require.Error(t, json.Unmarshal([]byte(body), &e), body)
	}
}

func TestAPIErrorMarshals(t *testing.T) {
	b, err := json.Marshal(APIError{Code: "X", Message: "m"})
	require.NoError(t, err)
	require.JSONEq(t, `{"code":"X","message":"m"}`, string(b))

	b, err = json.Marshal(APIError{Code: "X", Message: "m", Details: jsontext.Value(`[1]`)})
	require.NoError(t, err)
	require.JSONEq(t, `{"code":"X","message":"m","details":[1]}`, string(b))
}

func TestAPIResponse(t *testing.T) {
	var env apiResponse[thing]
	require.NoError(t, json.Unmarshal([]byte(`{"success":true,"data":{"name":"x"},"error":null,"metadata":null}`), &env))
	require.True(t, env.Success)
	require.Equal(t, &thing{Name: "x"}, env.Data)
	require.Nil(t, env.Error)
	require.Nil(t, env.Metadata)

	env = apiResponse[thing]{}
	require.NoError(t, json.Unmarshal([]byte(`{"success":true,"metadata":{"page":1,"per_page":2,"total":3,"total_pages":4}}`), &env))
	require.Nil(t, env.Data)
	require.Equal(t, &responseMetadata{Page: 1, PerPage: 2, Total: 3, TotalPages: 4}, env.Metadata)

	for _, body := range []string{
		`{}`,
		`{"success":null}`,
		`{"data":{"name":"x"}}`,
		`{"success":true,"data":{"name":"x"}}x`,
		`{"success":true,"error":{"code":"X"}}`,
	} {
		var env apiResponse[thing]
		require.Error(t, json.Unmarshal([]byte(body), &env), body)
	}
}
