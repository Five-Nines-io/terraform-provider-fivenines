package client

import "encoding/json"

// Nullable models the three states an optional field can be in on a PATCH body:
// absent from the request (the server keeps what it stores), explicitly null
// (the server clears it), or set to a value.
//
// Declare it as a pointer with `omitempty` so a nil pointer omits the key:
//
//	Keyword *Nullable[string] `json:"keyword,omitempty"`
//
//	nil            → key absent        → keep the stored value
//	Null[string]() → "keyword": null   → clear the stored value
//	Set("healthy") → "keyword": "healthy"
//
// The distinction is contractual on this API: explicit null clears a field
// while omission preserves it, and some collections (status page items,
// dns_expected_records) can only be emptied by sending an explicit value.
type Nullable[T any] struct {
	value *T
}

// Set returns a Nullable holding v.
func Set[T any](v T) *Nullable[T] {
	return &Nullable[T]{value: &v}
}

// Null returns a Nullable that marshals to JSON null.
func Null[T any]() *Nullable[T] {
	return &Nullable[T]{}
}

// IsNull reports whether n marshals to JSON null.
func (n *Nullable[T]) IsNull() bool {
	return n == nil || n.value == nil
}

// Get returns the wrapped value and whether one is set.
func (n *Nullable[T]) Get() (T, bool) {
	if n == nil || n.value == nil {
		var zero T
		return zero, false
	}
	return *n.value, true
}

func (n Nullable[T]) MarshalJSON() ([]byte, error) {
	if n.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*n.value)
}

func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		n.value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	n.value = &v
	return nil
}
