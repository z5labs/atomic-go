// Package storagetest provides reply writers for fake handlers of the
// atomic-storage management API, so a test can answer a storage client's
// requests in the shapes a server sends
// (docs/remote/endpoints/storage.md#response-envelope).
//
// [Data] writes a success envelope, [Fail] a failure envelope and [Raw] any
// body at all, for the replies a client must refuse. Serve a handler that
// uses them in memory with
// [github.com/z5labs/atomic-go/remote/remotetest.Client].
//
// The package imports nothing from the storage client, so a reply is
// written from the documented wire shapes rather than from the client's own
// types, and a test of the client checks it against them.
package storagetest

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
)

const contentType = "application/json"

// Data writes status and the success envelope {"success":true,"data":data},
// with data marshalled by encoding/json/v2. It returns the marshal error,
// and writes nothing, when data does not marshal.
func Data(w http.ResponseWriter, status int, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("storagetest: Data: %w", err)
	}
	return write(w, status, envelope{Success: true, Data: b})
}

// Fail writes status and the failure envelope
// {"success":false,"error":{"code":code,"message":message}}.
func Fail(w http.ResponseWriter, status int, code, message string) error {
	return write(w, status, envelope{Error: &apiError{Code: code, Message: message}})
}

// Raw writes status and body exactly as they are, with no Content-Type of
// its own, for a reply that is not an envelope: plain text, malformed JSON,
// or an envelope a test spells out byte for byte.
func Raw(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// envelope is the success or failure envelope Data and Fail write.
type envelope struct {
	Success bool           `json:"success"`
	Data    jsontext.Value `json:"data,omitzero"`
	Error   *apiError      `json:"error,omitzero"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func write(w http.ResponseWriter, status int, env envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("storagetest: marshal envelope: %w", err)
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, err = w.Write(b)
	return err
}
