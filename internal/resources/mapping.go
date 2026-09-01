package resources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- API response → Terraform state ---
//
// The rule is that an API null becomes a Terraform null. Flattening null to ""
// or 0 makes an unset optional attribute drift on every plan, and makes
// Terraform reject the apply ("was null, but now cty.StringVal(\"\")").

// optionalString maps a nullable API string onto a Terraform value.
func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// optionalNonEmptyString is optionalString for the fields where the API uses ""
// and null interchangeably to mean "unset".
func optionalNonEmptyString(s *string) types.String {
	if s == nil || *s == "" {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// stringOrKeep is optionalString for attributes that carry a schema default or
// are Required: a null from the API keeps whatever the plan already holds
// rather than wiping a known value to null, which Terraform rejects as an
// inconsistent result.
func stringOrKeep(s *string, current types.String) types.String {
	if s == nil {
		return current
	}
	return types.StringValue(*s)
}

// optionalInt64 maps a nullable API integer onto a Terraform value.
func optionalInt64(v *int64) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*v)
}

// optionalInt maps a nullable API int onto a Terraform Int64 value.
func optionalInt(v *int) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*v))
}

// listOrKeepEmpty maps an API array onto a Terraform list while preserving the
// difference between "unset" and "explicitly empty". Several array fields are
// normalised to null server-side when emptied, so a plan that says [] would
// otherwise come back null and fail the apply.
func listOrKeepEmpty[T any](ctx context.Context, elemType attr.Type, values []T, current types.List, diags *diag.Diagnostics) types.List {
	if len(values) > 0 {
		list, d := types.ListValueFrom(ctx, elemType, values)
		diags.Append(d...)
		return list
	}
	if !current.IsNull() && !current.IsUnknown() && len(current.Elements()) == 0 {
		return current
	}
	return types.ListNull(elemType)
}

// mapOrKeepEmpty is listOrKeepEmpty for map attributes.
func mapOrKeepEmpty(ctx context.Context, values map[string]string, current types.Map, diags *diag.Diagnostics) types.Map {
	if len(values) > 0 {
		m, d := types.MapValueFrom(ctx, types.StringType, values)
		diags.Append(d...)
		return m
	}
	if !current.IsNull() && !current.IsUnknown() && len(current.Elements()) == 0 {
		return current
	}
	return types.MapNull(types.StringType)
}

// --- Terraform plan → API update input ---
//
// Two shapes, picked per attribute:
//
//   - clearX for Optional-only attributes the provider owns end to end. A null
//     in the plan means the user removed the value, so it is sent as an
//     explicit JSON null and the server clears it.
//   - preserveX for Optional+Computed attributes and write-only secrets, where
//     a null means "whatever the server already has". Those keys are omitted.
//
// Unknown values are always omitted: there is nothing to send yet.

func clearString(v types.String) *client.Nullable[string] {
	if v.IsUnknown() {
		return nil
	}
	if v.IsNull() {
		return client.Null[string]()
	}
	return client.Set(v.ValueString())
}

func clearInt64(v types.Int64) *client.Nullable[int64] {
	if v.IsUnknown() {
		return nil
	}
	if v.IsNull() {
		return client.Null[int64]()
	}
	return client.Set(v.ValueInt64())
}

func clearInt(v types.Int64) *client.Nullable[int] {
	if v.IsUnknown() {
		return nil
	}
	if v.IsNull() {
		return client.Null[int]()
	}
	return client.Set(int(v.ValueInt64()))
}

// clearStringList sends an explicit [] for an empty plan list and an explicit
// null when the attribute is absent, so a pinned list can actually be dropped.
func clearStringList(ctx context.Context, v types.List, diags *diag.Diagnostics) *client.Nullable[[]string] {
	if v.IsUnknown() {
		return nil
	}
	if v.IsNull() {
		return client.Null[[]string]()
	}
	values := make([]string, 0, len(v.Elements()))
	diags.Append(v.ElementsAs(ctx, &values, false)...)
	return client.Set(values)
}

// clearStringMap is clearStringList for map attributes.
func clearStringMap(ctx context.Context, v types.Map, diags *diag.Diagnostics) *client.Nullable[map[string]string] {
	if v.IsUnknown() {
		return nil
	}
	if v.IsNull() {
		return client.Null[map[string]string]()
	}
	values := make(map[string]string, len(v.Elements()))
	diags.Append(v.ElementsAs(ctx, &values, false)...)
	return client.Set(values)
}

func preserveString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func preserveBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func preserveInt64(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func preserveInt(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

// elementsAsInt64 reads a plan list of numbers into a []int64.
func elementsAsInt64(ctx context.Context, v types.List, diags *diag.Diagnostics) []int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := make([]int64, 0, len(v.Elements()))
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}

// elementsAsInt reads a plan list of numbers into an []int.
func elementsAsInt(ctx context.Context, v types.List, diags *diag.Diagnostics) []int {
	values := elementsAsInt64(ctx, v, diags)
	if values == nil {
		return nil
	}
	out := make([]int, len(values))
	for i, value := range values {
		out[i] = int(value)
	}
	return out
}

// elementsAsString reads a plan list of strings into a []string.
func elementsAsString(ctx context.Context, v types.List, diags *diag.Diagnostics) []string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(v.Elements()))
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}

// elementsAsStringMap reads a plan map of strings into a map[string]string.
func elementsAsStringMap(ctx context.Context, v types.Map, diags *diag.Diagnostics) map[string]string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := make(map[string]string, len(v.Elements()))
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}
