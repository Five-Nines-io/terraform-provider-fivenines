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
	Templates []workflowTemplateModel `tfsdk:"templates"`
}

type workflowTemplateModel struct {
	Slug        types.String `tfsdk:"slug"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Category    types.String `tfsdk:"category"`
	Icon        types.String `tfsdk:"icon"`
	TriggerType types.String `tfsdk:"trigger_type"`
}

func NewWorkflowTemplatesDataSource() datasource.DataSource {
	return &workflowTemplatesDataSource{}
}

func (d *workflowTemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow_templates"
}

func (d *workflowTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the catalog of ready-made workflow templates.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.ListNestedAttribute{
				Description: "List of workflow templates.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"slug": schema.StringAttribute{
							Description: "Template identifier.",
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
							Description: "Catalog category.",
							Computed:    true,
						},
						"icon": schema.StringAttribute{
							Description: "Icon name used by the dashboard gallery.",
							Computed:    true,
						},
						"trigger_type": schema.StringAttribute{
							Description: "The trigger the template builds around. A template for a subsystem your fleet does not run never fires.",
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
		resp.Diagnostics.AddError("Unexpected DataSource Configure Type",
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
	for _, t := range templates {
		state.Templates = append(state.Templates, workflowTemplateModel{
			Slug:        types.StringValue(t.Slug),
			Name:        types.StringValue(t.Name),
			Description: types.StringValue(t.Description),
			Category:    types.StringValue(t.Category),
			Icon:        optionalString(t.Icon),
			TriggerType: types.StringValue(t.TriggerType),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
