package resources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &instanceResource{}
	_ resource.ResourceWithImportState = &instanceResource{}
)

type instanceResource struct {
	client *client.Client
}

type instanceModel struct {
	ID                  types.String `tfsdk:"id"`
	DisplayName         types.String `tfsdk:"display_name"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	MaintenanceMode     types.Bool   `tfsdk:"maintenance_mode"`
	Hostname            types.String `tfsdk:"hostname"`
	OperatingSystemName types.String `tfsdk:"operating_system_name"`
	KernelVersion       types.String `tfsdk:"kernel_version"`
	CPUArchitecture     types.String `tfsdk:"cpu_architecture"`
	CPUModel            types.String `tfsdk:"cpu_model"`
	CPUCount            types.Int64  `tfsdk:"cpu_count"`
	MemorySize          types.Int64  `tfsdk:"memory_size"`
	IPv4                types.String `tfsdk:"ipv4"`
	IPv6                types.String `tfsdk:"ipv6"`
	Source              types.String `tfsdk:"source"`
	ClientVersion       types.String `tfsdk:"client_version"`
	Status              types.String `tfsdk:"status"`
	FirstSyncAt         types.String `tfsdk:"first_sync_at"`
	LastSyncAt          types.String `tfsdk:"last_sync_at"`
	LastRequestAt       types.String `tfsdk:"last_request_at"`
	CreatedAt           types.String `tfsdk:"created_at"`
	UpdatedAt           types.String `tfsdk:"updated_at"`
}

func NewInstanceResource() resource.Resource {
	return &instanceResource{}
}

func (r *instanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

func (r *instanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines instance (monitored server).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier (UUID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"display_name": schema.StringAttribute{
				Description: "Display name of the instance.",
				Required:    true,
			},
			"enabled": schema.BoolAttribute{
				Description: "Whether the instance is enabled for monitoring.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"maintenance_mode": schema.BoolAttribute{
				Description: "Whether the instance is in maintenance mode.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"hostname": schema.StringAttribute{
				Description: "Hostname reported by the agent.",
				Computed:    true,
			},
			"operating_system_name": schema.StringAttribute{
				Description: "Operating system name.",
				Computed:    true,
			},
			"kernel_version": schema.StringAttribute{
				Description: "Kernel version.",
				Computed:    true,
			},
			"cpu_architecture": schema.StringAttribute{
				Description: "CPU architecture.",
				Computed:    true,
			},
			"cpu_model": schema.StringAttribute{
				Description: "CPU model.",
				Computed:    true,
			},
			"cpu_count": schema.Int64Attribute{
				Description: "Number of CPUs.",
				Computed:    true,
			},
			"memory_size": schema.Int64Attribute{
				Description: "Memory size in bytes.",
				Computed:    true,
			},
			"ipv4": schema.StringAttribute{
				Description: "IPv4 address.",
				Computed:    true,
			},
			"ipv6": schema.StringAttribute{
				Description: "IPv6 address.",
				Computed:    true,
			},
			"source": schema.StringAttribute{
				Description: "Agent type (e.g., fivenines-agent).",
				Computed:    true,
			},
			"client_version": schema.StringAttribute{
				Description: "Agent client version.",
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Current status of the instance.",
				Computed:    true,
			},
			"first_sync_at": schema.StringAttribute{
				Description: "First agent sync time.",
				Computed:    true,
			},
			"last_sync_at": schema.StringAttribute{
				Description: "Last time the agent synced.",
				Computed:    true,
			},
			"last_request_at": schema.StringAttribute{
				Description: "Last API request time from the agent.",
				Computed:    true,
			},
			"created_at": schema.StringAttribute{
				Description: "Creation timestamp.",
				Computed:    true,
			},
			"updated_at": schema.StringAttribute{
				Description: "Last update timestamp.",
				Computed:    true,
			},
		},
	}
}

func (r *instanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			"Expected *client.Client, got unexpected type.")
		return
	}
	r.client = c
}

func (r *instanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	enabled := plan.Enabled.ValueBool()
	maintenance := plan.MaintenanceMode.ValueBool()
	input := client.CreateInstanceInput{
		DisplayName:     plan.DisplayName.ValueString(),
		Enabled:         &enabled,
		MaintenanceMode: &maintenance,
	}

	tflog.Debug(ctx, "Creating instance", map[string]interface{}{"display_name": input.DisplayName})

	instance, err := r.client.CreateInstance(input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating instance", err.Error())
		return
	}

	mapInstanceToState(instance, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *instanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	instance, _, err := r.client.GetInstance(state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading instance", err.Error())
		return
	}

	mapInstanceToState(instance, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *instanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan instanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Fetch current to get ETag
	_, etag, err := r.client.GetInstance(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading instance for update", err.Error())
		return
	}

	displayName := plan.DisplayName.ValueString()
	enabled := plan.Enabled.ValueBool()
	maintenance := plan.MaintenanceMode.ValueBool()
	input := client.UpdateInstanceInput{
		DisplayName:     &displayName,
		Enabled:         &enabled,
		MaintenanceMode: &maintenance,
	}

	instance, err := r.client.UpdateInstance(state.ID.ValueString(), etag, input)
	if err != nil {
		resp.Diagnostics.AddError("Error updating instance", err.Error())
		return
	}

	mapInstanceToState(instance, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *instanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting instance", map[string]interface{}{"id": state.ID.ValueString()})

	err := r.client.DeleteInstance(state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting instance", err.Error())
	}
}

func (r *instanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapInstanceToState(i *client.Instance, state *instanceModel) {
	state.ID = types.StringValue(i.ID)
	state.DisplayName = types.StringValue(i.DisplayName)
	state.Enabled = types.BoolValue(i.Enabled)
	state.MaintenanceMode = types.BoolValue(i.MaintenanceMode)
	state.Hostname = types.StringValue(i.Hostname)
	state.OperatingSystemName = types.StringValue(i.OperatingSystemName)
	state.KernelVersion = types.StringValue(i.KernelVersion)
	state.CPUArchitecture = types.StringValue(i.CPUArchitecture)
	state.CPUModel = types.StringValue(i.CPUModel)
	state.CPUCount = types.Int64Value(int64(i.CPUCount))
	state.MemorySize = types.Int64Value(i.MemorySize)
	state.IPv4 = types.StringValue(i.IPv4)
	state.IPv6 = types.StringValue(i.IPv6)
	state.Source = types.StringValue(i.Source)
	state.ClientVersion = types.StringValue(i.ClientVersion)
	state.Status = types.StringValue(i.Status)
	state.FirstSyncAt = optionalString(i.FirstSyncAt)
	state.LastSyncAt = optionalString(i.LastSyncAt)
	state.LastRequestAt = optionalString(i.LastRequestAt)
	state.CreatedAt = types.StringValue(i.CreatedAt)
	state.UpdatedAt = types.StringValue(i.UpdatedAt)
}

func optionalString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}
