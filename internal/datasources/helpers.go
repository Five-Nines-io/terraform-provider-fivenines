package datasources

import "github.com/hashicorp/terraform-plugin-framework/types"

func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}
