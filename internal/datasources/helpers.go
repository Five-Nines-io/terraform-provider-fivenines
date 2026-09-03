package datasources

import (
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func optionalInt64(i *int64) types.Int64 {
	if i == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*i)
}

func optionalFloat64(f *float64) types.Float64 {
	if f == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*f)
}

// jsonString renders an arbitrary API object as a JSON string. Terraform has no
// dynamic object type to map these onto, so they are surfaced for jsondecode().
func jsonString(v map[string]interface{}) types.String {
	if v == nil {
		return types.StringNull()
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return types.StringNull()
	}
	return types.StringValue(string(encoded))
}

// The filter* helpers turn configured arguments into client filter values. An
// unset argument must stay out of the query entirely: the API rejects unknown
// and malformed query parameters with a 400 rather than ignoring them.

func filterString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

func filterInt64(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := v.ValueInt64()
	return &i
}

func filterBool(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}
