package resources

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// requiresReplaceOnDifferentAddress replaces the resource when the configured
// email names a different person, but not when it only changes case. Addresses
// are matched case-insensitively against the API, and the destroy half of a
// replacement here is destructive — it offboards somebody, or revokes a live
// invitation — so re-casing an address must never trigger one.
func requiresReplaceOnDifferentAddress() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !strings.EqualFold(req.StateValue.ValueString(), req.ConfigValue.ValueString())
		},
		"Replaces the resource when the email address names a different person, ignoring a change of case.",
		"Replaces the resource when the email address names a different person, ignoring a change of case.",
	)
}

// keepEmailCasing returns the address to store in state. The API stores
// addresses normalized, so a configuration that wrote "Lead@Acme.com" must keep
// that spelling: overwriting a Required attribute the configuration owns fails
// Terraform's "inconsistent result after apply" check. A genuinely different
// address is drift, and is reported as the API gave it.
func keepEmailCasing(current types.String, apiEmail string) types.String {
	if strings.EqualFold(current.ValueString(), apiEmail) {
		return current
	}
	return types.StringValue(apiEmail)
}
