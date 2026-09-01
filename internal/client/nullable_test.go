package client

import (
	"encoding/json"
	"testing"
)

func TestNullable_MarshalStates(t *testing.T) {
	type body struct {
		Field *Nullable[string] `json:"field,omitempty"`
	}

	tests := []struct {
		name  string
		field *Nullable[string]
		want  string
	}{
		{"unset omits the key", nil, `{}`},
		{"null clears the value", Null[string](), `{"field":null}`},
		{"a value is sent as-is", Set("healthy"), `{"field":"healthy"}`},
		{"an empty string is not an unset", Set(""), `{"field":""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(body{Field: tt.field})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestNullable_MarshalCollections(t *testing.T) {
	type body struct {
		Records *Nullable[[]string]          `json:"records,omitempty"`
		Headers *Nullable[map[string]string] `json:"headers,omitempty"`
	}

	// An explicitly empty collection has to reach the wire as [] / {}: with a
	// plain slice and `omitempty` it would vanish, and the API reads a missing
	// key as "keep what you have".
	got, err := json.Marshal(body{
		Records: Set([]string{}),
		Headers: Set(map[string]string{}),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"records":[],"headers":{}}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNullable_RoundTrip(t *testing.T) {
	var n Nullable[int]
	if err := json.Unmarshal([]byte(`42`), &n); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := n.Get(); !ok || v != 42 {
		t.Errorf("got (%v, %v), want (42, true)", v, ok)
	}
	if err := json.Unmarshal([]byte(`null`), &n); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !n.IsNull() {
		t.Error("expected IsNull after unmarshalling null")
	}
}

func TestUpdateStatusPageInput_EmptyItems(t *testing.T) {
	// Emptying a page is only possible with an explicit []; the previous
	// `Items []StatusPageItem ... omitempty` marshalled that to nothing.
	got, err := json.Marshal(UpdateStatusPageInput{Items: Set([]StatusPageItem{})})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"items":[]}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// Leaving items unmanaged must not touch them.
	got, err = json.Marshal(UpdateStatusPageInput{Name: strPtr("Status")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"name":"Status"}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestUpdateUptimeMonitorInput_ClearSemantics(t *testing.T) {
	got, err := json.Marshal(UpdateUptimeMonitorInput{
		Name:               strPtr("API"),
		Keyword:            Null[string](),
		DNSExpectedRecords: Set([]string{}),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"name":"API","keyword":null,"dns_expected_records":[]}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestUpdateNetworkDeviceInput_SecretsAreOmittedWhenUnset(t *testing.T) {
	// Write-only credentials are blank-means-keep server-side, so an unset plan
	// value must omit the key rather than send null and wipe the credential.
	got, err := json.Marshal(UpdateNetworkDeviceInput{Name: strPtr("core-sw")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"name":"core-sw"}`; string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func strPtr(s string) *string { return &s }
