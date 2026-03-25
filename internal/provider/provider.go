package provider

import (
	"context"
	"os"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/datasources"
	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/resources"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &fiveninesProvider{}

type fiveninesProvider struct{}

type fiveninesProviderModel struct {
	APIKey  types.String `tfsdk:"api_key"`
	BaseURL types.String `tfsdk:"base_url"`
}

func New() provider.Provider {
	return &fiveninesProvider{}
}

func (p *fiveninesProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "fivenines"
}

func (p *fiveninesProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for FiveNines server monitoring and observability platform.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Description: "FiveNines API key (starts with fn_). Can also be set via FIVENINES_API_KEY environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"base_url": schema.StringAttribute{
				Description: "FiveNines API base URL. Defaults to https://fivenines.io. Can also be set via FIVENINES_BASE_URL environment variable.",
				Optional:    true,
			},
		},
	}
}

func (p *fiveninesProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config fiveninesProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve API key: config > env
	apiKey := config.APIKey.ValueString()
	if apiKey == "" {
		apiKey = os.Getenv("FIVENINES_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing API Key",
			"The FiveNines API key must be set in the provider configuration or via the FIVENINES_API_KEY environment variable.",
		)
		return
	}

	// Resolve base URL: config > env > default
	baseURL := config.BaseURL.ValueString()
	if baseURL == "" {
		baseURL = os.Getenv("FIVENINES_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://fivenines.io"
	}

	c := client.NewClient(baseURL, apiKey)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *fiveninesProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewInstanceResource,
		resources.NewTaskResource,
		resources.NewWorkflowResource,
		resources.NewUptimeMonitorResource,
	}
}

func (p *fiveninesProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewProbeRegionsDataSource,
		datasources.NewIntegrationsDataSource,
		datasources.NewWorkflowRunsDataSource,
	}
}
