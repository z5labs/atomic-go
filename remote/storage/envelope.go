package storage

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
)

// apiResponse is the envelope around every response body but that of a
// successful DELETE (docs/remote/endpoints/storage.md#apiresponse). success
// is required; data, error and metadata may be absent or null, which leaves
// them nil.
type apiResponse[T any] struct {
	Success  bool              `json:"success"`
	Data     *T                `json:"data"`
	Error    *APIError         `json:"error"`
	Metadata *responseMetadata `json:"metadata"`
}

// UnmarshalJSON decodes an envelope with the rules of [unmarshalAliased]:
// member names match exactly, a member written twice is refused, and
// success must be present and not null.
func (r *apiResponse[T]) UnmarshalJSON(b []byte) error {
	type plain apiResponse[T]
	return unmarshalAliased(b, (*plain)(r), nil, "success")
}

// responseMetadata is an envelope's paging information
// (docs/remote/endpoints/storage.md#responsemetadata). The client never
// reads it, but a malformed one fails the envelope, as it does in Rust.
type responseMetadata struct {
	Page       uint32 `json:"page"`
	PerPage    uint32 `json:"perPage"`
	Total      uint64 `json:"total"`
	TotalPages uint32 `json:"totalPages"`
}

// metadataAlias returns the member a snake_case spelling responseMetadata
// also accepts stands for.
func metadataAlias(name string) (string, bool) {
	switch name {
	case "per_page":
		return "perPage", true
	case "total_pages":
		return "totalPages", true
	}
	return "", false
}

// UnmarshalJSON decodes paging information whose members are all required
// and whose perPage and totalPages may be spelled per_page and total_pages.
func (m *responseMetadata) UnmarshalJSON(b []byte) error {
	type plain responseMetadata
	return unmarshalAliased(b, (*plain)(m), metadataAlias, "page", "perPage", "total", "totalPages")
}

// APIError is the error a management API server reports, inside an
// envelope or as a bare error body
// (docs/remote/endpoints/storage.md#apierror).
type APIError struct {
	// Code is a machine-readable code, such as "NOT_FOUND".
	Code string `json:"code"`

	// Message is a human-readable reason.
	Message string `json:"message"`

	// Details is any further JSON the server sent, or nil when it sent
	// none or null.
	Details jsontext.Value `json:"details,omitzero"`
}

// UnmarshalJSON decodes an error object whose code and message are both
// required strings, as the Rust client's type requires them. Member names
// match exactly and a member written twice is refused.
func (e *APIError) UnmarshalJSON(b []byte) error {
	type plain APIError
	var out plain
	if err := unmarshalAliased(b, &out, nil, "code", "message"); err != nil {
		return err
	}
	if out.Details.Kind() == 'n' {
		out.Details = nil
	}
	*e = APIError(out)
	return nil
}

// unmarshalAliased decodes the JSON object b into v, a pointer to a struct
// whose json tags name its members, the way serde decodes a struct with
// aliases.
//
// Each member for whose name alias reports a member is read as that member;
// a nil alias accepts no other spellings. A member written twice, or under both its name and an alias,
// is refused, as serde refuses a duplicate field. Every name in required,
// which uses the names the tags give, must be present and not null. Names
// match exactly, unknown members are ignored, and anything but exactly one
// object is refused.
//
// A type's UnmarshalJSON calls it with a pointer to a type without that
// method, so that decoding does not recurse:
//
//	func (m *T) UnmarshalJSON(b []byte) error {
//		type plain T
//		return unmarshalAliased(b, (*plain)(m), aliasOfT, "id")
//	}
func unmarshalAliased(b []byte, v any, alias func(string) (string, bool), required ...string) error {
	dec := jsontext.NewDecoder(bytes.NewReader(b))
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '{' {
		return fmt.Errorf("want a JSON object, not %s", tok.Kind())
	}

	var renamed bytes.Buffer
	enc := jsontext.NewEncoder(&renamed)
	if err := enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	present := make(map[string]bool)
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		// A token is voided by the next read, so the name is copied out
		// first.
		written := tok.String()
		val, err := dec.ReadValue()
		if err != nil {
			return err
		}
		name := written
		if alias != nil {
			if canonical, ok := alias(name); ok {
				name = canonical
			}
		}
		// The encoder refuses a name it has already written, which is
		// how a member under both its name and an alias is caught.
		if err := enc.WriteToken(jsontext.String(name)); err != nil {
			return fmt.Errorf("member %q: %w", written, err)
		}
		if err := enc.WriteValue(val); err != nil {
			return err
		}
		if val.Kind() != 'n' {
			present[name] = true
		}
	}
	if _, err := dec.ReadToken(); err != nil {
		return err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		return errors.New("data after the object")
	}
	if err := enc.WriteToken(jsontext.EndObject); err != nil {
		return err
	}

	for _, name := range required {
		if !present[name] {
			return fmt.Errorf("missing member %q", name)
		}
	}
	return json.Unmarshal(renamed.Bytes(), v)
}
