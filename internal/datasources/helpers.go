package datasources

import (
	"encoding/json"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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

func optionalBool(b *bool) types.Bool {
	if b == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*b)
}

// stringList always returns a non-nil slice, so an empty API array reads as an
// empty list rather than as null. The distinction matters wherever a null and
// an empty collection are different answers.
func stringList(values []string) []types.String {
	out := make([]types.String, len(values))
	for i, v := range values {
		out[i] = types.StringValue(v)
	}
	return out
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

// addSecurityError reports an error from one of the plan-gated security
// endpoints. The 403 gets its own diagnostic: it means the plan does not
// include `security_details`, and reading it as "no findings" is exactly the
// mistake these endpoints refuse an empty list to prevent.
func addSecurityError(diags *diag.Diagnostics, summary string, err error) {
	if client.IsForbidden(err) {
		diags.AddError(
			"Security data requires a plan with security_details",
			"FiveNines refused the request with 403 Forbidden. Vulnerability and container-image "+
				"details (CVEs, scores, fix versions) require the Pro plan or above.\n\n"+
				"This is deliberately an error rather than an empty result: an empty list here would "+
				"read as an all-clear.\n\nAPI response: "+err.Error(),
		)
		return
	}
	diags.AddError(summary, err.Error())
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

// configureClient is the Configure body every data source in this package
// shares: nil provider data is the pre-configuration call the framework always
// makes and must stay a quiet no-op, and anything that is not a *client.Client
// is a provider bug.
//
// It returns the client rather than assigning it, because each data source owns
// a differently-typed receiver. Callers must keep the assignment -- dropping it
// still compiles and still raises no diagnostic, and the nil only surfaces as a
// panic on the first Read.
func configureClient(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type",
			"Expected *client.Client, got unexpected type.")
		return nil
	}
	return c
}
