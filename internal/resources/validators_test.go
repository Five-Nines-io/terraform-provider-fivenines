package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	fwschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// resourceSchema returns the schema a resource advertises, so the validator
// tests run against the real attribute paths instead of a stand-in.
func resourceSchema(t *testing.T, r resource.Resource) fwschema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.(interface {
		Schema(context.Context, resource.SchemaRequest, *resource.SchemaResponse)
	}).Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// config builds a configuration where every attribute is null except the ones
// named in set — the shape of a .tf file that only writes a few attributes.
func config(t *testing.T, schema fwschema.Schema, set map[string]tftypes.Value) tfsdk.Config {
	t.Helper()
	objectType, ok := schema.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatal("expected the schema to be an object type")
	}
	attributes := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		if value, ok := set[name]; ok {
			attributes[name] = value
			continue
		}
		attributes[name] = tftypes.NewValue(attributeType, nil)
	}
	return tfsdk.Config{Schema: schema, Raw: tftypes.NewValue(objectType, attributes)}
}

func validate(t *testing.T, r resource.Resource, set map[string]tftypes.Value) []string {
	t.Helper()
	ctx := context.Background()
	cfg := config(t, resourceSchema(t, r), set)

	var messages []string
	for _, validator := range r.(resource.ResourceWithConfigValidators).ConfigValidators(ctx) {
		var resp resource.ValidateConfigResponse
		validator.ValidateResource(ctx, resource.ValidateConfigRequest{Config: cfg}, &resp)
		for _, d := range resp.Diagnostics.Errors() {
			messages = append(messages, d.Summary()+": "+d.Detail())
		}
	}
	return messages
}

func str(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
func num(n int64) tftypes.Value  { return tftypes.NewValue(tftypes.Number, n) }

func TestUptimeMonitorConfigValidators(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]tftypes.Value
		wantError bool
	}{
		{
			name:      "https without a url",
			config:    map[string]tftypes.Value{"protocol": str("https")},
			wantError: true,
		},
		{
			name:   "https with a url",
			config: map[string]tftypes.Value{"protocol": str("https"), "url": str("https://example.com")},
		},
		{
			name:      "tcp without a port",
			config:    map[string]tftypes.Value{"protocol": str("tcp"), "hostname": str("example.com")},
			wantError: true,
		},
		{
			name:      "tcp without a hostname",
			config:    map[string]tftypes.Value{"protocol": str("tcp"), "port": num(5432)},
			wantError: true,
		},
		{
			name:   "tcp with both",
			config: map[string]tftypes.Value{"protocol": str("tcp"), "hostname": str("example.com"), "port": num(5432)},
		},
		{
			name:      "dns without a record type",
			config:    map[string]tftypes.Value{"protocol": str("dns"), "hostname": str("example.com")},
			wantError: true,
		},
		{
			name:   "dns with a record type",
			config: map[string]tftypes.Value{"protocol": str("dns"), "hostname": str("example.com"), "dns_record_type": str("A")},
		},
		{
			name:   "icmp needs none of them",
			config: map[string]tftypes.Value{"protocol": str("icmp"), "hostname": str("example.com")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validate(t, NewUptimeMonitorResource(), tt.config)
			if tt.wantError && len(errs) == 0 {
				t.Error("expected a validation error, got none")
			}
			if !tt.wantError && len(errs) > 0 {
				t.Errorf("expected no validation error, got %v", errs)
			}
		})
	}
}
