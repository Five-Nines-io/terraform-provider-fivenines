package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.ConfigValidator = requiredWhenValidator{}

// requiredWhenValidator reports the "this attribute is required when that one
// has this value" rules the API enforces with a 422. The API message is fine;
// the point of restating them here is timing — the user finds out at plan time
// instead of half way through an apply.
type requiredWhenValidator struct {
	trigger  path.Path
	value    string
	required []path.Path
}

// requiredWhen builds a config validator that fails when trigger equals value
// and any of the required attributes is absent from the configuration.
func requiredWhen(trigger path.Path, value string, required ...path.Path) resource.ConfigValidator {
	return requiredWhenValidator{trigger: trigger, value: value, required: required}
}

func (v requiredWhenValidator) Description(_ context.Context) string {
	names := make([]string, len(v.required))
	for i, p := range v.required {
		names[i] = p.String()
	}
	return fmt.Sprintf("%s must be set when %s is %q", strings.Join(names, " and "), v.trigger, v.value)
}

func (v requiredWhenValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v requiredWhenValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var trigger types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, v.trigger, &trigger)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// An unknown trigger only resolves during apply, so there is nothing to
	// check yet; the API still rejects a bad combination.
	if trigger.IsNull() || trigger.IsUnknown() || trigger.ValueString() != v.value {
		return
	}

	for _, required := range v.required {
		var value attr.Value
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, required, &value)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if value.IsNull() {
			resp.Diagnostics.AddAttributeError(
				required,
				"Missing Required Attribute",
				fmt.Sprintf("%s must be set when %s is %q.", required, v.trigger, v.value),
			)
		}
	}
}
