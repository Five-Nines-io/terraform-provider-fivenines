package resources

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// jsonEqual reports whether two JSON documents are semantically identical,
// ignoring key ordering and whitespace. Invalid JSON is never equal.
func jsonEqual(a, b string) bool {
	var av, bv interface{}
	if json.Unmarshal([]byte(a), &av) != nil {
		return false
	}
	if json.Unmarshal([]byte(b), &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// preserveJSONIfEqual keeps the value already in state when it says the same
// thing as the one just read from the API, so that reformatting a JSON
// attribute does not show up as drift. An empty fetched value means the API has
// nothing to report, which makes the attribute null.
func preserveJSONIfEqual(prior types.String, fetched string) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && jsonEqual(prior.ValueString(), fetched) {
		return prior
	}
	return stringOrNull(fetched)
}

func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// jsonSemanticEquality suppresses the diff on a JSON string attribute when the
// configured document only differs from the stored one by key ordering or
// whitespace.
type jsonSemanticEquality struct{}

func (m jsonSemanticEquality) Description(_ context.Context) string {
	return "Ignores JSON formatting differences (key order, whitespace) when comparing to state."
}

func (m jsonSemanticEquality) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m jsonSemanticEquality) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if jsonEqual(req.StateValue.ValueString(), req.PlanValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}
