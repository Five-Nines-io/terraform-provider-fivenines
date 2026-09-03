package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &workflowTemplatesDataSource{}

type workflowTemplatesDataSource struct {
	client *client.Client
}

type workflowTemplatesModel struct {
	Templates []workflowTemplateItem `tfsdk:"templates"`
}

type workflowTemplateItem struct {
	Slug        types.String `tfsdk:"slug"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Category    types.String `tfsdk:"category"`
	TriggerType types.String `tfsdk:"trigger_type"`
	JSON        types.String `tfsdk:"json"`
}

func NewWorkflowTemplatesDataSource() datasource.DataSource {
	return &workflowTemplatesDataSource{}
}

func (d *workflowTemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow_templates"
}

func (d *workflowTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the FiveNines workflow templates that can be instantiated with the `template_slug` argument of `fivenines_workflow`.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.ListNestedAttribute{
				Description: "List of workflow templates.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"slug": schema.StringAttribute{
							Description: "Template slug, to pass as `template_slug` on a workflow.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Template name.",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "What the template does.",
							Computed:    true,
						},
						"category": schema.StringAttribute{
							Description: "Template category.",
							Computed:    true,
						},
						"trigger_type": schema.StringAttribute{
							Description: "Trigger type of the template's graph.",
							Computed:    true,
						},
						"json": schema.StringAttribute{
							Description: "The raw template object as returned by the API, for fields this provider does not model yet. Use jsondecode() to read it.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *workflowTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type",
			"Expected *client.Client, got unexpected type.")
		return
	}
	d.client = c
}

func (d *workflowTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	templates, err := d.client.ListWorkflowTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing workflow templates", err.Error())
		return
	}

	var state workflowTemplatesModel
	state.Templates = make([]workflowTemplateItem, len(templates))
	for i, t := range templates {
		state.Templates[i] = workflowTemplateItem{
			Slug:        types.StringValue(t.Slug),
			Name:        types.StringValue(t.Name),
			Description: types.StringValue(t.Description),
			Category:    types.StringValue(t.Category),
			TriggerType: types.StringValue(t.TriggerType),
			JSON:        types.StringValue(string(t.Raw)),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
