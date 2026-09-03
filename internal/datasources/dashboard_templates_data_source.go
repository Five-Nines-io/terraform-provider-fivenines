package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &dashboardTemplatesDataSource{}

type dashboardTemplatesDataSource struct {
	client *client.Client
}

type dashboardTemplatesModel struct {
	Templates []dashboardTemplateModel `tfsdk:"templates"`
}

type dashboardTemplateModel struct {
	Slug              types.String `tfsdk:"slug"`
	Name              types.String `tfsdk:"name"`
	Description       types.String `tfsdk:"description"`
	Category          types.String `tfsdk:"category"`
	Icon              types.String `tfsdk:"icon"`
	TargetKinds       []string     `tfsdk:"target_kinds"`
	PanelCount        types.Int64  `tfsdk:"panel_count"`
	SectionCount      types.Int64  `tfsdk:"section_count"`
	Available         types.Bool   `tfsdk:"available"`
	UnavailableReason types.String `tfsdk:"unavailable_reason"`
}

func NewDashboardTemplatesDataSource() datasource.DataSource {
	return &dashboardTemplatesDataSource{}
}

func (d *dashboardTemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dashboard_templates"
}

func (d *dashboardTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the ready-made dashboards the FiveNines gallery offers, with an availability " +
			"verdict for your organization. Feed a `slug` to `fivenines_dashboard`'s `template_slug` to " +
			"build one. A template your organization cannot use is listed with a reason rather than " +
			"hidden, so `available` is a discoverability hint and not a gate.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.ListNestedAttribute{
				Description: "The catalog.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"slug": schema.StringAttribute{
							Description: "The identifier `fivenines_dashboard.template_slug` takes.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Template name.",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "What the template charts.",
							Computed:    true,
						},
						"category": schema.StringAttribute{
							Description: "Gallery category (Databases, Web servers, ...).",
							Computed:    true,
						},
						"icon": schema.StringAttribute{
							Description: "Icon name used by the gallery.",
							Computed:    true,
						},
						"target_kinds": schema.ListAttribute{
							Description: "Which entity kinds this template's panels bind.",
							Computed:    true,
							ElementType: types.StringType,
						},
						"panel_count": schema.Int64Attribute{
							Description: "How many panels the template declares. What actually gets built " +
								"can be fewer — panels the organization cannot feed are dropped.",
							Computed: true,
						},
						"section_count": schema.Int64Attribute{
							Description: "How many sections the template declares.",
							Computed:    true,
						},
						"available": schema.BoolAttribute{
							Description: "Whether your organization reports the software this template charts.",
							Computed:    true,
						},
						"unavailable_reason": schema.StringAttribute{
							Description: "One sentence naming what is missing, or null when available.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *dashboardTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *dashboardTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	templates, err := d.client.ListDashboardTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing dashboard templates", err.Error())
		return
	}

	var state dashboardTemplatesModel
	for _, t := range templates {
		targetKinds := t.TargetKinds
		if targetKinds == nil {
			targetKinds = []string{}
		}
		state.Templates = append(state.Templates, dashboardTemplateModel{
			Slug:              types.StringValue(t.Slug),
			Name:              types.StringValue(t.Name),
			Description:       types.StringValue(t.Description),
			Category:          types.StringValue(t.Category),
			Icon:              types.StringValue(t.Icon),
			TargetKinds:       targetKinds,
			PanelCount:        types.Int64Value(t.PanelCount),
			SectionCount:      types.Int64Value(t.SectionCount),
			Available:         types.BoolValue(t.Available),
			UnavailableReason: optionalString(t.UnavailableReason),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
