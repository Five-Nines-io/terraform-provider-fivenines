package resources

import (
	"context"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &networkDeviceResource{}
	_ resource.ResourceWithImportState = &networkDeviceResource{}
)

type networkDeviceResource struct {
	client *client.Client
}

type networkDeviceModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	IPAddress         types.String `tfsdk:"ip_address"`
	PollingHostID     types.String `tfsdk:"polling_host_id"`
	DeviceType        types.String `tfsdk:"device_type"`
	PollingInterval   types.Int64  `tfsdk:"polling_interval"`
	SNMPVersion       types.String `tfsdk:"snmp_version"`
	SNMPCommunity     types.String `tfsdk:"snmp_community"`
	SNMPUsername      types.String `tfsdk:"snmp_username"`
	SNMPSecurityLevel types.String `tfsdk:"snmp_security_level"`
	SNMPAuthProtocol  types.String `tfsdk:"snmp_auth_protocol"`
	SNMPAuthPassword  types.String `tfsdk:"snmp_auth_password"`
	SNMPPrivProtocol  types.String `tfsdk:"snmp_priv_protocol"`
	SNMPPrivPassword  types.String `tfsdk:"snmp_priv_password"`
	MaintenanceMode   types.Bool   `tfsdk:"maintenance_mode"`
	Status            types.String `tfsdk:"status"`
	Vendor            types.String `tfsdk:"vendor"`
	Model             types.String `tfsdk:"model"`
	SysName           types.String `tfsdk:"sys_name"`
	LastPolledAt      types.String `tfsdk:"last_polled_at"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func NewNetworkDeviceResource() resource.Resource {
	return &networkDeviceResource{}
}

func (r *networkDeviceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network_device"
}

func (r *networkDeviceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines network device (SNMP monitoring).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier (UUID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the network device.",
				Required:    true,
			},
			"ip_address": schema.StringAttribute{
				Description: "IP address of the device.",
				Required:    true,
			},
			"polling_host_id": schema.StringAttribute{
				Description: "UUID of the instance to poll from. If omitted, polling is done from the FiveNines cloud.",
				Optional:    true,
			},
			"device_type": schema.StringAttribute{
				Description: "Type of device (e.g., switch, router, firewall, other).",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("other"),
			},
			"polling_interval": schema.Int64Attribute{
				Description: "Polling interval in seconds (10-3600).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(60),
				Validators: []validator.Int64{
					int64validator.Between(10, 3600),
				},
			},
			"snmp_version": schema.StringAttribute{
				Description: "SNMP version (v2c or v3).",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("v2c", "v3"),
				},
			},
			"snmp_community": schema.StringAttribute{
				Description: "SNMP community string (required for v2c). Write-only — not returned by the API.",
				Optional:    true,
				Sensitive:   true,
			},
			"snmp_username": schema.StringAttribute{
				Description: "SNMPv3 username.",
				Optional:    true,
				Sensitive:   true,
			},
			"snmp_security_level": schema.StringAttribute{
				Description: "SNMPv3 security level.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("no_auth_no_priv"),
				Validators: []validator.String{
					stringvalidator.OneOf("no_auth_no_priv", "auth_no_priv", "auth_priv"),
				},
			},
			"snmp_auth_protocol": schema.StringAttribute{
				Description: "SNMPv3 authentication protocol.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("md5"),
				Validators: []validator.String{
					stringvalidator.OneOf("md5", "sha"),
				},
			},
			"snmp_auth_password": schema.StringAttribute{
				Description: "SNMPv3 authentication password. Write-only — not returned by the API.",
				Optional:    true,
				Sensitive:   true,
			},
			"snmp_priv_protocol": schema.StringAttribute{
				Description: "SNMPv3 privacy protocol.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("des"),
				Validators: []validator.String{
					stringvalidator.OneOf("des", "aes"),
				},
			},
			"snmp_priv_password": schema.StringAttribute{
				Description: "SNMPv3 privacy password. Write-only — not returned by the API.",
				Optional:    true,
				Sensitive:   true,
			},
			"maintenance_mode": schema.BoolAttribute{
				Description: "Whether the device is in maintenance mode.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"status": schema.StringAttribute{
				Description: "Current status (up, down, unknown, unreachable).",
				Computed:    true,
			},
			"vendor": schema.StringAttribute{
				Description: "Detected vendor.",
				Computed:    true,
			},
			"model": schema.StringAttribute{
				Description: "Detected model.",
				Computed:    true,
			},
			"sys_name": schema.StringAttribute{
				Description: "SNMP sysName.",
				Computed:    true,
			},
			"last_polled_at": schema.StringAttribute{
				Description: "Last poll timestamp.",
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

func (r *networkDeviceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *client.Client.")
		return
	}
	r.client = c
}

func (r *networkDeviceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkDeviceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateNetworkDeviceInput{
		Name:        plan.Name.ValueString(),
		IPAddress:   plan.IPAddress.ValueString(),
		SNMPVersion: plan.SNMPVersion.ValueString(),
	}
	if !plan.PollingHostID.IsNull() && !plan.PollingHostID.IsUnknown() {
		v := plan.PollingHostID.ValueString()
		input.PollingHostID = &v
	}
	if !plan.DeviceType.IsNull() && !plan.DeviceType.IsUnknown() {
		input.DeviceType = plan.DeviceType.ValueString()
	}
	if !plan.PollingInterval.IsNull() && !plan.PollingInterval.IsUnknown() {
		v := int(plan.PollingInterval.ValueInt64())
		input.PollingInterval = &v
	}
	if !plan.SNMPCommunity.IsNull() {
		input.SNMPCommunity = plan.SNMPCommunity.ValueString()
	}
	if !plan.SNMPUsername.IsNull() {
		input.SNMPUsername = plan.SNMPUsername.ValueString()
	}
	if !plan.SNMPSecurityLevel.IsNull() {
		input.SNMPSecurityLevel = plan.SNMPSecurityLevel.ValueString()
	}
	if !plan.SNMPAuthProtocol.IsNull() {
		input.SNMPAuthProtocol = plan.SNMPAuthProtocol.ValueString()
	}
	if !plan.SNMPAuthPassword.IsNull() {
		input.SNMPAuthPassword = plan.SNMPAuthPassword.ValueString()
	}
	if !plan.SNMPPrivProtocol.IsNull() {
		input.SNMPPrivProtocol = plan.SNMPPrivProtocol.ValueString()
	}
	if !plan.SNMPPrivPassword.IsNull() {
		input.SNMPPrivPassword = plan.SNMPPrivPassword.ValueString()
	}

	tflog.Debug(ctx, "Creating network device", map[string]interface{}{"name": input.Name})

	device, err := r.client.CreateNetworkDevice(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating network device", err.Error())
		return
	}

	// Handle maintenance mode if requested
	if plan.MaintenanceMode.ValueBool() {
		if err := r.client.EnterMaintenanceNetworkDevice(ctx, device.ID); err != nil {
			resp.Diagnostics.AddError("Error entering maintenance mode", err.Error())
			return
		}
		device, _, err = r.client.GetNetworkDevice(ctx, device.ID)
		if err != nil {
			resp.Diagnostics.AddError("Error reading network device after maintenance", err.Error())
			return
		}
	}

	mapNetworkDeviceToState(device, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkDeviceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkDeviceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	device, _, err := r.client.GetNetworkDevice(ctx, state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading network device", err.Error())
		return
	}

	mapNetworkDeviceToState(device, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *networkDeviceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan networkDeviceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state networkDeviceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// Build update input
	name := plan.Name.ValueString()
	ipAddr := plan.IPAddress.ValueString()
	input := client.UpdateNetworkDeviceInput{
		Name:      &name,
		IPAddress: &ipAddr,
	}
	if !plan.PollingHostID.IsNull() {
		v := plan.PollingHostID.ValueString()
		input.PollingHostID = &v
	}
	if !plan.DeviceType.IsNull() {
		v := plan.DeviceType.ValueString()
		input.DeviceType = &v
	}
	if !plan.PollingInterval.IsNull() {
		v := int(plan.PollingInterval.ValueInt64())
		input.PollingInterval = &v
	}
	if !plan.SNMPVersion.IsNull() {
		v := plan.SNMPVersion.ValueString()
		input.SNMPVersion = &v
	}
	if !plan.SNMPCommunity.IsNull() {
		v := plan.SNMPCommunity.ValueString()
		input.SNMPCommunity = &v
	}
	if !plan.SNMPUsername.IsNull() {
		v := plan.SNMPUsername.ValueString()
		input.SNMPUsername = &v
	}
	if !plan.SNMPSecurityLevel.IsNull() {
		v := plan.SNMPSecurityLevel.ValueString()
		input.SNMPSecurityLevel = &v
	}
	if !plan.SNMPAuthProtocol.IsNull() {
		v := plan.SNMPAuthProtocol.ValueString()
		input.SNMPAuthProtocol = &v
	}
	if !plan.SNMPAuthPassword.IsNull() {
		v := plan.SNMPAuthPassword.ValueString()
		input.SNMPAuthPassword = &v
	}
	if !plan.SNMPPrivProtocol.IsNull() {
		v := plan.SNMPPrivProtocol.ValueString()
		input.SNMPPrivProtocol = &v
	}
	if !plan.SNMPPrivPassword.IsNull() {
		v := plan.SNMPPrivPassword.ValueString()
		input.SNMPPrivPassword = &v
	}

	// ETag retry loop
	var device *client.NetworkDevice
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetNetworkDevice(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading network device for update", err.Error())
			return
		}
		device, err = r.client.UpdateNetworkDevice(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on network device update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating network device", err.Error())
			return
		}
		break
	}

	// Handle maintenance mode transitions
	wantMaintenance := plan.MaintenanceMode.ValueBool()
	if wantMaintenance && !device.MaintenanceMode {
		if err := r.client.EnterMaintenanceNetworkDevice(ctx, id); err != nil {
			resp.Diagnostics.AddError("Error entering maintenance mode", err.Error())
			return
		}
	} else if !wantMaintenance && device.MaintenanceMode {
		if err := r.client.ExitMaintenanceNetworkDevice(ctx, id); err != nil {
			resp.Diagnostics.AddError("Error exiting maintenance mode", err.Error())
			return
		}
	}

	// Re-read final state
	device, _, err := r.client.GetNetworkDevice(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading network device after update", err.Error())
		return
	}

	mapNetworkDeviceToState(device, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkDeviceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkDeviceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting network device", map[string]interface{}{"id": state.ID.ValueString()})

	err := r.client.DeleteNetworkDevice(ctx, state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting network device", err.Error())
	}
}

func (r *networkDeviceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapNetworkDeviceToState(d *client.NetworkDevice, state *networkDeviceModel) {
	state.ID = types.StringValue(d.ID)
	state.Name = types.StringValue(d.Name)
	state.IPAddress = types.StringValue(d.IPAddress)
	if d.PollingHostID != nil {
		state.PollingHostID = types.StringValue(*d.PollingHostID)
	} else {
		state.PollingHostID = types.StringNull()
	}
	state.DeviceType = types.StringValue(d.DeviceType)
	state.PollingInterval = types.Int64Value(int64(d.PollingInterval))
	state.SNMPVersion = types.StringValue(d.SNMPVersion)
	// snmp_username is returned by API (not sensitive)
	state.SNMPUsername = types.StringValue(d.SNMPUsername)
	state.SNMPSecurityLevel = types.StringValue(d.SNMPSecurityLevel)
	state.SNMPAuthProtocol = types.StringValue(d.SNMPAuthProtocol)
	state.SNMPPrivProtocol = types.StringValue(d.SNMPPrivProtocol)
	// Write-only fields (community, auth_password, priv_password) are NOT
	// returned by the API, so we preserve whatever the user set in config.
	state.MaintenanceMode = types.BoolValue(d.MaintenanceMode)
	state.Status = types.StringValue(d.Status)
	state.Vendor = types.StringValue(d.Vendor)
	state.Model = types.StringValue(d.Model)
	state.SysName = types.StringValue(d.SysName)
	state.LastPolledAt = optionalString(d.LastPolledAt)
	state.CreatedAt = types.StringValue(d.CreatedAt)
	state.UpdatedAt = types.StringValue(d.UpdatedAt)
}
