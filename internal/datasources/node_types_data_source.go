package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &nodeTypesDataSource{}

type nodeTypesDataSource struct {
	client *client.Client
}

type nodeTypesModel struct {
	NodeTypes []nodeTypeModel `tfsdk:"node_types"`
}

type nodeTypeModel struct {
	Type       types.String `tfsdk:"type"`
	Category   types.String `tfsdk:"category"`
	SchemaJSON types.String `tfsdk:"schema_json"`
	Available  types.Bool   `tfsdk:"available"`
}

func NewNodeTypesDataSource() datasource.DataSource {
	return &nodeTypesDataSource{}
}

func (d *nodeTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_node_types"
}

func (d *nodeTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the workflow node catalog, with each node type's JSON Schema and whether it is available to the authenticated organization.",
		Attributes: map[string]schema.Attribute{
			"node_types": schema.ListNestedAttribute{
				Description: "List of node types.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Description: "Node type identifier, as used in a workflow execution graph.",
							Computed:    true,
						},
						"category": schema.StringAttribute{
							Description: "Node category (trigger, logic, action, notification).",
							Computed:    true,
						},
						"schema_json": schema.StringAttribute{
							Description: "JSON Schema describing the node's required and optional data fields, as a JSON string.",
							Computed:    true,
						},
						"available": schema.BoolAttribute{
							Description: "Whether this node type is available for the organization (based on feature flags).",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *nodeTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *nodeTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	nodeTypes, err := d.client.ListNodeTypes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing node types", err.Error())
		return
	}

	var state nodeTypesModel
	for _, nt := range nodeTypes {
		m := nodeTypeModel{
			Type:      types.StringValue(nt.Type),
			Category:  types.StringValue(nt.Category),
			Available: types.BoolValue(nt.Available),
		}
		if len(nt.Schema) > 0 {
			m.SchemaJSON = types.StringValue(string(nt.Schema))
		} else {
			m.SchemaJSON = types.StringNull()
		}
		state.NodeTypes = append(state.NodeTypes, m)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
