package provider_test

import (
	"context"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Every resource and data source must expose a schema the framework accepts and
// a type name unique within the provider. Both are wiring mistakes that
// otherwise only surface when Terraform loads the plugin — after release.

func TestProvider_ResourceSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := provider.New()

	var metaResp fwprovider.MetadataResponse
	p.Metadata(ctx, fwprovider.MetadataRequest{}, &metaResp)

	seen := map[string]bool{}
	for _, newResource := range p.Resources(ctx) {
		r := newResource()

		var nameResp resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: metaResp.TypeName}, &nameResp)
		if seen[nameResp.TypeName] {
			t.Errorf("duplicate resource type name %q", nameResp.TypeName)
		}
		seen[nameResp.TypeName] = true

		var schemaResp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Fatalf("%s: schema errors: %v", nameResp.TypeName, schemaResp.Diagnostics)
		}
		if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: invalid schema: %v", nameResp.TypeName, diags)
		}
		if _, ok := schemaResp.Schema.Attributes["id"]; !ok {
			t.Errorf("%s: no id attribute", nameResp.TypeName)
		}
	}
}

func TestProvider_DataSourceSchemasAreValid(t *testing.T) {
	ctx := context.Background()
	p := provider.New()

	var metaResp fwprovider.MetadataResponse
	p.Metadata(ctx, fwprovider.MetadataRequest{}, &metaResp)

	seen := map[string]bool{}
	for _, newDataSource := range p.DataSources(ctx) {
		d := newDataSource()

		var nameResp datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: metaResp.TypeName}, &nameResp)
		if seen[nameResp.TypeName] {
			t.Errorf("duplicate data source type name %q", nameResp.TypeName)
		}
		seen[nameResp.TypeName] = true

		var schemaResp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
		if schemaResp.Diagnostics.HasError() {
			t.Fatalf("%s: schema errors: %v", nameResp.TypeName, schemaResp.Diagnostics)
		}
		if diags := schemaResp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: invalid schema: %v", nameResp.TypeName, diags)
		}
	}
}

func TestProvider_RegistersOrganizationTypes(t *testing.T) {
	ctx := context.Background()
	p := provider.New()

	resourceNames := map[string]bool{}
	for _, newResource := range p.Resources(ctx) {
		var nameResp resource.MetadataResponse
		newResource().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "fivenines"}, &nameResp)
		resourceNames[nameResp.TypeName] = true
	}
	for _, want := range []string{
		"fivenines_organization",
		"fivenines_organization_member",
		"fivenines_organization_invitation",
	} {
		if !resourceNames[want] {
			t.Errorf("resource %s is not registered", want)
		}
	}

	dataSourceNames := map[string]bool{}
	for _, newDataSource := range p.DataSources(ctx) {
		var nameResp datasource.MetadataResponse
		newDataSource().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "fivenines"}, &nameResp)
		dataSourceNames[nameResp.TypeName] = true
	}
	for _, want := range []string{
		"fivenines_organization",
		"fivenines_organization_members",
		"fivenines_organization_security",
		"fivenines_organization_saml",
	} {
		if !dataSourceNames[want] {
			t.Errorf("data source %s is not registered", want)
		}
	}
}

// --- Configure ---

// configureProvider builds a configuration from the provider's own schema and
// runs Configure against it, returning the client handed to resources.
func configureProvider(t *testing.T, attrs map[string]tftypes.Value) *client.Client {
	t.Helper()
	ctx := context.Background()
	p := provider.New()

	var schemaResp fwprovider.SchemaResponse
	p.Schema(ctx, fwprovider.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("provider schema errors: %v", schemaResp.Diagnostics)
	}

	objType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attrType := range objType.AttributeTypes {
		if v, ok := attrs[name]; ok {
			values[name] = v
			continue
		}
		values[name] = tftypes.NewValue(attrType, nil)
	}

	var resp fwprovider.ConfigureResponse
	p.Configure(ctx, fwprovider.ConfigureRequest{
		Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objType, values)},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("configure errors: %v", resp.Diagnostics)
	}

	c, ok := resp.ResourceData.(*client.Client)
	if !ok {
		t.Fatalf("expected *client.Client, got %T", resp.ResourceData)
	}
	return c
}

// The dry-run pre-flight on organization members is on unless asked otherwise:
// it is the guard against a half-applied offboarding.
func TestProvider_Configure_PlanValidationDefaultsOn(t *testing.T) {
	t.Setenv("FIVENINES_API_KEY", "")
	t.Setenv("FIVENINES_SKIP_PLAN_VALIDATION", "")

	c := configureProvider(t, map[string]tftypes.Value{
		"api_key": tftypes.NewValue(tftypes.String, "fn_test"),
	})

	if c.SkipPlanValidation {
		t.Error("expected plan validation to be enabled by default")
	}
	if c.BaseURL != "https://fivenines.io" {
		t.Errorf("expected the default base URL, got %q", c.BaseURL)
	}
}

func TestProvider_Configure_SkipPlanValidationFromConfig(t *testing.T) {
	t.Setenv("FIVENINES_SKIP_PLAN_VALIDATION", "")

	c := configureProvider(t, map[string]tftypes.Value{
		"api_key":              tftypes.NewValue(tftypes.String, "fn_test"),
		"skip_plan_validation": tftypes.NewValue(tftypes.Bool, true),
	})

	if !c.SkipPlanValidation {
		t.Error("expected skip_plan_validation to be honoured")
	}
}

func TestProvider_Configure_SkipPlanValidationFromEnv(t *testing.T) {
	for _, value := range []string{"true", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("FIVENINES_SKIP_PLAN_VALIDATION", value)

			c := configureProvider(t, map[string]tftypes.Value{
				"api_key": tftypes.NewValue(tftypes.String, "fn_test"),
			})

			if !c.SkipPlanValidation {
				t.Errorf("expected FIVENINES_SKIP_PLAN_VALIDATION=%s to be honoured", value)
			}
		})
	}
}

// An explicit false in the configuration wins over the environment.
func TestProvider_Configure_SkipPlanValidationConfigWinsOverEnv(t *testing.T) {
	t.Setenv("FIVENINES_SKIP_PLAN_VALIDATION", "true")

	c := configureProvider(t, map[string]tftypes.Value{
		"api_key":              tftypes.NewValue(tftypes.String, "fn_test"),
		"skip_plan_validation": tftypes.NewValue(tftypes.Bool, false),
	})

	if c.SkipPlanValidation {
		t.Error("expected the explicit false in configuration to win over the environment")
	}
}
