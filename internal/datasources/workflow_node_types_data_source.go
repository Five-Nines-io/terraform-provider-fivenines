package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &workflowNodeTypesDataSource{}

type workflowNodeTypesDataSource struct {
	client *client.Client
}

type workflowNodeTypesModel struct {
	NodeTypes []workflowNodeTypeItem `tfsdk:"node_types"`
}

type workflowNodeTypeItem struct {
	Type        types.String `tfsdk:"type"`
	Name        types.String `tfsdk:"name"`
	Category    types.String `tfsdk:"category"`
	Description types.String `tfsdk:"description"`
	JSON        types.String `tfsdk:"json"`
}

func NewWorkflowNodeTypesDataSource() datasource.DataSource {
	return &workflowNodeTypesDataSource{}
}

func (d *workflowNodeTypesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workflow_node_types"
}

func (d *workflowNodeTypesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the node types available to workflow execution graphs. Useful to validate an `execution_graph_json` against what the API actually supports before applying.",
		Attributes: map[string]schema.Attribute{
			"node_types": schema.ListNestedAttribute{
				Description: "List of node types.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Description: "Node type identifier, as used in an execution graph.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Human-readable node name.",
							Computed:    true,
						},
						"category": schema.StringAttribute{
							Description: "Node category (trigger, condition, action, ...).",
							Computed:    true,
						},
						"description": schema.StringAttribute{
							Description: "What the node does.",
							Computed:    true,
						},
						"json": schema.StringAttribute{
							Description: "The raw node type object as returned by the API, including its configuration schema. Use jsondecode() to read it.",
							Computed:    true,
						},
					},
				},
			},
		},
	}
}

func (d *workflowNodeTypesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *workflowNodeTypesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	nodeTypes, err := d.client.ListNodeTypes(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing node types", err.Error())
		return
	}

	var state workflowNodeTypesModel
	state.NodeTypes = make([]workflowNodeTypeItem, len(nodeTypes))
	for i, n := range nodeTypes {
		state.NodeTypes[i] = workflowNodeTypeItem{
			Type:        types.StringValue(n.Type),
			Name:        types.StringValue(n.Name),
			Category:    types.StringValue(n.Category),
			Description: types.StringValue(n.Description),
			JSON:        types.StringValue(string(n.Raw)),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
