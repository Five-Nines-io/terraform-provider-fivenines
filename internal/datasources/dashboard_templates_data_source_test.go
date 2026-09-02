package datasources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
)

func TestDashboardTemplatesSchema_ValidateImplementation(t *testing.T) {
	ctx := context.Background()
	resp := &datasource.SchemaResponse{}
	NewDashboardTemplatesDataSource().Schema(ctx, datasource.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("schema errors: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Errorf("invalid schema implementation: %v", diags)
	}
}
