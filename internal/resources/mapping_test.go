package resources

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestOptionalString_NullRoundTrip(t *testing.T) {
	if got := optionalString(nil); !got.IsNull() {
		t.Errorf("expected null for a nil API value, got %q", got.ValueString())
	}
	// "" is a value the user can legitimately set; it must not become null.
	if got := optionalString(ptr("")); got.IsNull() || got.ValueString() != "" {
		t.Errorf(`expected "", got %v`, got)
	}
}

func TestOptionalNonEmptyString(t *testing.T) {
	// For the fields where the API uses "" and null interchangeably, both are
	// "unset" and must map to null or the attribute drifts on every plan.
	if got := optionalNonEmptyString(ptr("")); !got.IsNull() {
		t.Errorf(`expected "" to map to null, got %v`, got)
	}
	if got := optionalNonEmptyString(nil); !got.IsNull() {
		t.Errorf("expected nil to map to null, got %v", got)
	}
	if got := optionalNonEmptyString(ptr("0 2 * * *")); got.ValueString() != "0 2 * * *" {
		t.Errorf("expected the value through, got %v", got)
	}
}

func TestStringOrKeep(t *testing.T) {
	current := types.StringValue("UTC")
	if got := stringOrKeep(nil, current); got.ValueString() != "UTC" {
		t.Errorf("expected the planned default to survive a null, got %v", got)
	}
	if got := stringOrKeep(ptr("Europe/Paris"), current); got.ValueString() != "Europe/Paris" {
		t.Errorf("expected the API value to win, got %v", got)
	}
}

func TestOptionalInts(t *testing.T) {
	if got := optionalInt64(nil); !got.IsNull() {
		t.Errorf("expected null, got %v", got)
	}
	if got := optionalInt64(ptr(int64(300))); got.ValueInt64() != 300 {
		t.Errorf("expected 300, got %v", got)
	}
}

// The plan→input helpers carry no clear/preserve policy of their own: they hand
// back a pointer when the plan holds a value and nil otherwise, and the struct
// tag on the update input decides whether nil omits the key or clears the value.
func TestPlanPointers(t *testing.T) {
	if got := stringPtr(types.StringNull()); got != nil {
		t.Error("expected nil for a null plan value")
	}
	if got := stringPtr(types.StringUnknown()); got != nil {
		t.Error("expected nil for an unknown plan value")
	}
	if got := stringPtr(types.StringValue("x")); got == nil || *got != "x" {
		t.Errorf("got %v, want x", got)
	}

	if got := boolPtr(types.BoolNull()); got != nil {
		t.Error("expected nil for a null bool")
	}
	// false is a value, not an absence.
	if got := boolPtr(types.BoolValue(false)); got == nil || *got {
		t.Errorf("expected a pointer to false, got %v", got)
	}

	if got := int64Ptr(types.Int64Null()); got != nil {
		t.Error("expected nil for a null int64")
	}
	if got := int64Ptr(types.Int64Value(60)); got == nil || *got != 60 {
		t.Errorf("got %v, want 60", got)
	}

	if got := intPtr(types.Int64Null()); got != nil {
		t.Error("expected nil for a null int")
	}
	if got := intPtr(types.Int64Value(443)); got == nil || *got != 443 {
		t.Errorf("got %v, want 443", got)
	}
}
