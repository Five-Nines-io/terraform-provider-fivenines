package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
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

func TestStringOrKeep(t *testing.T) {
	current := types.StringValue("UTC")
	if got := stringOrKeep(nil, current); got.ValueString() != "UTC" {
		t.Errorf("expected the planned default to survive a null, got %v", got)
	}
	if got := stringOrKeep(ptr("Europe/Paris"), current); got.ValueString() != "Europe/Paris" {
		t.Errorf("expected the API value to win, got %v", got)
	}
}

func TestClearString(t *testing.T) {
	if got := clearString(types.StringUnknown()); got != nil {
		t.Error("expected an unknown plan value to omit the field")
	}
	if got := clearString(types.StringNull()); got == nil || !got.IsNull() {
		t.Error("expected a null plan value to send an explicit null")
	}
	got := clearString(types.StringValue("healthy"))
	if v, ok := got.Get(); !ok || v != "healthy" {
		t.Errorf("got (%v, %v), want (healthy, true)", v, ok)
	}
}

func TestPreserveString(t *testing.T) {
	if got := preserveString(types.StringNull()); got != nil {
		t.Error("expected a null plan value to omit the field")
	}
	if got := preserveString(types.StringUnknown()); got != nil {
		t.Error("expected an unknown plan value to omit the field")
	}
	if got := preserveString(types.StringValue("x")); got == nil || *got != "x" {
		t.Errorf("got %v, want x", got)
	}
}

func TestClearStringList(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	if got := clearStringList(ctx, types.ListNull(types.StringType), &diags); got == nil || !got.IsNull() {
		t.Error("expected an absent list to send an explicit null")
	}

	empty, d := types.ListValueFrom(ctx, types.StringType, []string{})
	diags.Append(d...)
	got := clearStringList(ctx, empty, &diags)
	values, ok := got.Get()
	if !ok || len(values) != 0 || values == nil {
		t.Errorf("expected an explicitly empty list to send [], got (%v, %v)", values, ok)
	}
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestListOrKeepEmpty(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	// The API normalises an emptied list to null. A plan that said [] has to
	// keep [] or Terraform rejects the apply as an inconsistent result.
	planned, d := types.ListValueFrom(ctx, types.StringType, []string{})
	diags.Append(d...)
	got := listOrKeepEmpty(ctx, types.StringType, []string(nil), planned, &diags)
	if got.IsNull() || len(got.Elements()) != 0 {
		t.Errorf("expected the explicit empty list to survive, got %v", got)
	}

	// With nothing planned, a null API value stays null.
	got = listOrKeepEmpty(ctx, types.StringType, []string(nil), types.ListNull(types.StringType), &diags)
	if !got.IsNull() {
		t.Errorf("expected null, got %v", got)
	}

	// A non-empty API value always wins.
	got = listOrKeepEmpty(ctx, types.StringType, []string{"1.2.3.4"}, planned, &diags)
	if len(got.Elements()) != 1 {
		t.Errorf("expected the API value, got %v", got)
	}
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestMapOrKeepEmpty(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics

	planned, d := types.MapValueFrom(ctx, types.StringType, map[string]string{})
	diags.Append(d...)
	got := mapOrKeepEmpty(ctx, nil, planned, &diags)
	if got.IsNull() {
		t.Error("expected the explicit empty map to survive a null from the API")
	}

	got = mapOrKeepEmpty(ctx, nil, types.MapNull(types.StringType), &diags)
	if !got.IsNull() {
		t.Errorf("expected null, got %v", got)
	}
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}
