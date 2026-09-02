package resources

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- API response → Terraform state ---
//
// The rule is that an API null becomes a Terraform null. Flattening null to ""
// or 0 makes an unset optional attribute drift on every plan, and Terraform
// rejects the apply outright ("was null, but now cty.StringVal(\"\")") for
// Optional-only attributes.

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

// --- Terraform plan → API update input ---
//
// One shape for every attribute: a pointer when the plan holds a value, nil
// otherwise. Whether nil means "leave it alone" or "clear it" is a property of
// the field, not of the call, so it lives in the struct tag on the update input
// (see UpdateUptimeMonitorInput for the convention):
//
//   - `json:"field,omitempty"` — nil omits the key, so the server keeps what it
//     stores. For Optional+Computed attributes and write-only secrets.
//   - `json:"field"` — nil marshals as an explicit null, so the server clears
//     the value. For Optional-only attributes the provider owns end to end.
//   - `*[]T` / `*map[K]V` with omitempty — nil omits the key, but a pointer to
//     an empty value marshals as an explicit [] / {} and clears. For list and
//     map attributes where `x = []` is itself a legal config, which a plain
//     slice with omitempty cannot express.

func stringPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func intPtr(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

// optionalNonEmptyStringOrKeep is optionalNonEmptyString for attributes whose
// prior value distinguishes "" from null. The API uses the two interchangeably,
// so a body configured as "" comes back as null and vice versa; reporting the
// other form makes the attribute drift on every plan forever. Whichever of the
// two empties the practitioner wrote is kept; a value replaced by a real string,
// or a real string emptied out of band, still reports the server.
func optionalNonEmptyStringOrKeep(s *string, current types.String) types.String {
	if s != nil && *s != "" {
		return types.StringValue(*s)
	}
	if !current.IsUnknown() && (current.IsNull() || current.ValueString() == "") {
		return current
	}
	return types.StringNull()
}
