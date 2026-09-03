package datasources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &instanceCapabilityStatusDataSource{}

type instanceCapabilityStatusDataSource struct {
	client *client.Client
}

type instanceCapabilityStatusModel struct {
	InstanceID   types.String `tfsdk:"instance_id"`
	Capabilities types.Map    `tfsdk:"capabilities"`
	Pending      types.List   `tfsdk:"pending"`
	Reasons      types.Map    `tfsdk:"reasons"`
	Reported     types.Bool   `tfsdk:"reported"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
}

func NewInstanceCapabilityStatusDataSource() datasource.DataSource {
	return &instanceCapabilityStatusDataSource{}
}

func (d *instanceCapabilityStatusDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_capability_status"
}

func (d *instanceCapabilityStatusDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "What the agent last reported it can actually COLLECT on one instance, as opposed to " +
			"what the operator switched on -- the `*_enabled` arguments on `fivenines_instance`. It is what " +
			"tells \"Redis monitoring is enabled\" from \"Redis monitoring is enabled and working\".\n\n" +
			"THE HONESTY RULE: an empty `capabilities` map means \"the agent has not reported\", never " +
			"\"nothing is supported\". Read `reported` rather than testing the map for emptiness, and note " +
			"the rule does NOT key off `updated_at`: the server stamps that on every check-in whether or " +
			"not the agent sent a capability block, so an older agent checking in every 60s presents as an " +
			"empty map with a timestamp seconds old.\n\n" +
			"`pending` is UNGATED here -- it reports what the agent last said, so a feature disabled since " +
			"the last tick lingers in it until the next one. Intersect it with the instance's `*_enabled` " +
			"arguments for the dashboard's gated view.",
		Attributes: map[string]schema.Attribute{
			"instance_id": schema.StringAttribute{
				Description: "UUID of the instance (host) to read.",
				Required:    true,
			},
			"capabilities": schema.MapAttribute{
				Description: "Per-capability verdict from the agent, keyed by capability name. EMPTY MEANS " +
					"\"NOT REPORTED\", not \"nothing is supported\" -- see `reported`.",
				Computed:    true,
				ElementType: types.BoolType,
			},
			"pending": schema.ListAttribute{
				Description: "Capabilities the operator enabled that the agent cannot yet collect. Ungated: " +
					"a capability disabled since the agent's last tick stays here until the next one.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"reasons": schema.MapAttribute{
				Description: "The agent's explanation for a blocked capability, keyed by capability name " +
					"(`zfs` -> `zpool not found in PATH`). Truncated to 500 characters server-side. Only " +
					"blocked capabilities appear, so this map is normally shorter than `capabilities`.",
				Computed:    true,
				ElementType: types.StringType,
			},
			"reported": schema.BoolAttribute{
				Description: "Whether the agent reported any capabilities at all -- `false` is the " +
					"\"not reported\" state that an empty `capabilities` map cannot distinguish from " +
					"\"nothing is supported\". Treat a `false` here as \"we do not know\", never as a " +
					"verdict: an agent too old to speak the capability protocol reads exactly like this " +
					"while checking in perfectly happily.",
				Computed: true,
			},
			"updated_at": schema.StringAttribute{
				Description: "When the host last checked in. Stamped on EVERY check-in, including from " +
					"agents that send no capability block, so a fresh timestamp does not mean the agent " +
					"reports capabilities. Null only for a host that has never posted any payload.",
				Computed: true,
			},
		},
	}
}

func (d *instanceCapabilityStatusDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *instanceCapabilityStatusDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config instanceCapabilityStatusModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	status, err := d.client.GetInstanceCapabilityStatus(ctx, config.InstanceID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading instance capability status", err.Error())
		return
	}

	capabilities := make(map[string]attr.Value, len(status.Capabilities))
	for name, ok := range status.Capabilities {
		capabilities[name] = types.BoolValue(ok)
	}
	capabilityMap, diags := types.MapValue(types.BoolType, capabilities)
	resp.Diagnostics.Append(diags...)

	reasons := make(map[string]attr.Value, len(status.Reasons))
	for name, reason := range status.Reasons {
		reasons[name] = types.StringValue(reason)
	}
	reasonMap, diags := types.MapValue(types.StringType, reasons)
	resp.Diagnostics.Append(diags...)

	// Non-nil even when the agent reported none, so length()/for_each over the
	// result works. `reported` is what carries the "we have not heard" state.
	pendingElems := make([]attr.Value, 0, len(status.Pending))
	for _, name := range status.Pending {
		pendingElems = append(pendingElems, types.StringValue(name))
	}
	pendingList, diags := types.ListValue(types.StringType, pendingElems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	config.Capabilities = capabilityMap
	config.Pending = pendingList
	config.Reasons = reasonMap
	config.Reported = types.BoolValue(len(status.Capabilities) > 0)
	config.UpdatedAt = optionalString(status.UpdatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
