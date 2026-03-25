package resources

import (
	"context"
	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                = &uptimeMonitorResource{}
	_ resource.ResourceWithImportState = &uptimeMonitorResource{}
)

type uptimeMonitorResource struct {
	client *client.Client
}

type uptimeMonitorModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	Protocol            types.String `tfsdk:"protocol"`
	URL                 types.String `tfsdk:"url"`
	Hostname            types.String `tfsdk:"hostname"`
	Port                types.Int64  `tfsdk:"port"`
	HTTPMethod          types.String `tfsdk:"http_method"`
	IPVersion           types.String `tfsdk:"ip_version"`
	IntervalSeconds     types.Int64  `tfsdk:"interval_seconds"`
	TimeoutSeconds      types.Int64  `tfsdk:"timeout_seconds"`
	ConfirmationCount   types.Int64  `tfsdk:"confirmation_count"`
	Keyword             types.String `tfsdk:"keyword"`
	KeywordAbsent       types.Bool   `tfsdk:"keyword_absent"`
	FollowRedirects     types.Bool   `tfsdk:"follow_redirects"`
	ExpectedStatusCodes types.List   `tfsdk:"expected_status_codes"`
	ProbeRegionIDs      types.List   `tfsdk:"probe_region_ids"`
	Status              types.String `tfsdk:"status"`
	SSLExpiresAt        types.String `tfsdk:"ssl_expires_at"`
	LastError           types.String `tfsdk:"last_error"`
	NextCheckAt         types.String `tfsdk:"next_check_at"`
	LastCheckAt         types.String `tfsdk:"last_check_at"`
	// DNS fields
	DNSRecordType      types.String `tfsdk:"dns_record_type"`
	DNSExpectedRecords types.List   `tfsdk:"dns_expected_records"`
	// Custom HTTP fields
	CustomHeaders types.Map    `tfsdk:"custom_headers"`
	CustomBody    types.String `tfsdk:"custom_body"`
	ContentType   types.String `tfsdk:"content_type"`
	// Recovery
	RecoveryCount types.Int64  `tfsdk:"recovery_count"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func NewUptimeMonitorResource() resource.Resource {
	return &uptimeMonitorResource{}
}

func (r *uptimeMonitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_uptime_monitor"
}

func (r *uptimeMonitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines uptime monitor.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier (UUID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the uptime monitor.",
				Required:    true,
			},
			"protocol": schema.StringAttribute{
				Description: `Protocol: "https", "tcp", "icmp", "dns", or "custom_http".`,
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("https", "tcp", "icmp", "dns", "custom_http"),
				},
			},
			"url": schema.StringAttribute{
				Description: "URL to monitor (required for https protocol).",
				Optional:    true,
				Computed:    true,
			},
			"hostname": schema.StringAttribute{
				Description: "Hostname to monitor (required for tcp/icmp protocols).",
				Optional:    true,
				Computed:    true,
			},
			"port": schema.Int64Attribute{
				Description: "Port to monitor (required for tcp protocol).",
				Optional:    true,
			},
			"http_method": schema.StringAttribute{
				Description: `HTTP method: "GET", "HEAD", or "POST".`,
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("GET"),
				Validators: []validator.String{
					stringvalidator.OneOf("GET", "HEAD", "POST"),
				},
			},
			"ip_version": schema.StringAttribute{
				Description: `IP version: "auto", "ipv4", or "ipv6".`,
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("auto"),
				Validators: []validator.String{
					stringvalidator.OneOf("auto", "ipv4", "ipv6"),
				},
			},
			"interval_seconds": schema.Int64Attribute{
				Description: "Check interval in seconds.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(300),
			},
			"timeout_seconds": schema.Int64Attribute{
				Description: "Timeout in seconds (max 15).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(15),
				Validators: []validator.Int64{
					int64validator.AtMost(15),
				},
			},
			"confirmation_count": schema.Int64Attribute{
				Description: "Number of probe regions that must confirm status (quorum).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
			},
			"keyword": schema.StringAttribute{
				Description: "Keyword that must be present in the response body.",
				Optional:    true,
			},
			"keyword_absent": schema.BoolAttribute{
				Description: "If true, alert when the keyword IS found (absent check).",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"follow_redirects": schema.BoolAttribute{
				Description: "Whether to follow HTTP redirects.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"expected_status_codes": schema.ListAttribute{
				Description: "Expected HTTP status codes.",
				Optional:    true,
				Computed:    true,
				ElementType: types.Int64Type,
			},
			"probe_region_ids": schema.ListAttribute{
				Description: "Probe region IDs to check from. Defaults to all active regions.",
				Optional:    true,
				Computed:    true,
				ElementType: types.Int64Type,
			},
			"dns_record_type": schema.StringAttribute{
				Description: `DNS record type to query (required for dns protocol): "A", "AAAA", "CNAME", "MX", "TXT", "NS".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("A", "AAAA", "CNAME", "MX", "TXT", "NS"),
				},
			},
			"dns_expected_records": schema.ListAttribute{
				Description: "Expected DNS record values.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"custom_headers": schema.MapAttribute{
				Description: "Custom HTTP headers as key-value pairs.",
				Optional:    true,
				ElementType: types.StringType,
			},
			"custom_body": schema.StringAttribute{
				Description: "Request body for POST requests (https/custom_http protocols).",
				Optional:    true,
			},
			"content_type": schema.StringAttribute{
				Description: `Content-Type header: "application/json", "application/x-www-form-urlencoded", or "text/plain".`,
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("application/json", "application/x-www-form-urlencoded", "text/plain"),
				},
			},
			"recovery_count": schema.Int64Attribute{
				Description: "Number of successful checks required to transition from down to up.",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
			},
			"status": schema.StringAttribute{
				Description: "Current status.",
				Computed:    true,
			},
			"ssl_expires_at": schema.StringAttribute{
				Description: "SSL certificate expiration date.",
				Computed:    true,
			},
			"last_error": schema.StringAttribute{
				Description: "Last error message.",
				Computed:    true,
			},
			"next_check_at": schema.StringAttribute{
				Description: "Next scheduled check time.",
				Computed:    true,
			},
			"last_check_at": schema.StringAttribute{
				Description: "Last check time.",
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

func (r *uptimeMonitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *uptimeMonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan uptimeMonitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateUptimeMonitorInput{
		Name:     plan.Name.ValueString(),
		Protocol: plan.Protocol.ValueString(),
	}
	if !plan.URL.IsNull() && !plan.URL.IsUnknown() {
		input.URL = plan.URL.ValueString()
	}
	if !plan.Hostname.IsNull() && !plan.Hostname.IsUnknown() {
		input.Hostname = plan.Hostname.ValueString()
	}
	if !plan.Port.IsNull() && !plan.Port.IsUnknown() {
		v := int(plan.Port.ValueInt64())
		input.Port = &v
	}
	if !plan.HTTPMethod.IsNull() && !plan.HTTPMethod.IsUnknown() {
		input.HTTPMethod = plan.HTTPMethod.ValueString()
	}
	if !plan.IPVersion.IsNull() && !plan.IPVersion.IsUnknown() {
		input.IPVersion = plan.IPVersion.ValueString()
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		v := int(plan.IntervalSeconds.ValueInt64())
		input.IntervalSeconds = &v
	}
	if !plan.TimeoutSeconds.IsNull() && !plan.TimeoutSeconds.IsUnknown() {
		v := int(plan.TimeoutSeconds.ValueInt64())
		input.TimeoutSeconds = &v
	}
	if !plan.ConfirmationCount.IsNull() && !plan.ConfirmationCount.IsUnknown() {
		v := int(plan.ConfirmationCount.ValueInt64())
		input.ConfirmationCount = &v
	}
	if !plan.Keyword.IsNull() && !plan.Keyword.IsUnknown() {
		input.Keyword = plan.Keyword.ValueString()
	}
	if !plan.KeywordAbsent.IsNull() && !plan.KeywordAbsent.IsUnknown() {
		v := plan.KeywordAbsent.ValueBool()
		input.KeywordAbsent = &v
	}
	if !plan.FollowRedirects.IsNull() && !plan.FollowRedirects.IsUnknown() {
		v := plan.FollowRedirects.ValueBool()
		input.FollowRedirects = &v
	}
	if !plan.ExpectedStatusCodes.IsNull() && !plan.ExpectedStatusCodes.IsUnknown() {
		var codes []int
		for _, elem := range plan.ExpectedStatusCodes.Elements() {
			if v, ok := elem.(types.Int64); ok {
				codes = append(codes, int(v.ValueInt64()))
			}
		}
		input.ExpectedStatusCodes = codes
	}
	if !plan.ProbeRegionIDs.IsNull() && !plan.ProbeRegionIDs.IsUnknown() {
		var ids []int64
		for _, elem := range plan.ProbeRegionIDs.Elements() {
			if v, ok := elem.(types.Int64); ok {
				ids = append(ids, v.ValueInt64())
			}
		}
		input.ProbeRegionIDs = ids
	}
	if !plan.DNSRecordType.IsNull() && !plan.DNSRecordType.IsUnknown() {
		input.DNSRecordType = plan.DNSRecordType.ValueString()
	}
	if !plan.DNSExpectedRecords.IsNull() && !plan.DNSExpectedRecords.IsUnknown() {
		var records []string
		for _, elem := range plan.DNSExpectedRecords.Elements() {
			if v, ok := elem.(types.String); ok {
				records = append(records, v.ValueString())
			}
		}
		input.DNSExpectedRecords = records
	}
	if !plan.CustomHeaders.IsNull() && !plan.CustomHeaders.IsUnknown() {
		headers := make(map[string]string)
		for k, v := range plan.CustomHeaders.Elements() {
			if sv, ok := v.(types.String); ok {
				headers[k] = sv.ValueString()
			}
		}
		input.CustomHeaders = headers
	}
	if !plan.CustomBody.IsNull() && !plan.CustomBody.IsUnknown() {
		input.CustomBody = plan.CustomBody.ValueString()
	}
	if !plan.ContentType.IsNull() && !plan.ContentType.IsUnknown() {
		input.ContentType = plan.ContentType.ValueString()
	}
	if !plan.RecoveryCount.IsNull() && !plan.RecoveryCount.IsUnknown() {
		v := int(plan.RecoveryCount.ValueInt64())
		input.RecoveryCount = &v
	}

	tflog.Debug(ctx, "Creating uptime monitor", map[string]interface{}{"name": input.Name})

	monitor, err := r.client.CreateUptimeMonitor(input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating uptime monitor", err.Error())
		return
	}

	r.mapToState(ctx, monitor, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *uptimeMonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state uptimeMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitor, _, err := r.client.GetUptimeMonitor(state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading uptime monitor", err.Error())
		return
	}

	r.mapToState(ctx, monitor, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *uptimeMonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan uptimeMonitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state uptimeMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	_, etag, err := r.client.GetUptimeMonitor(id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading uptime monitor for update", err.Error())
		return
	}

	input := client.UpdateUptimeMonitorInput{}
	if !plan.Name.IsNull() {
		v := plan.Name.ValueString()
		input.Name = &v
	}
	if !plan.URL.IsNull() && !plan.URL.IsUnknown() {
		v := plan.URL.ValueString()
		input.URL = &v
	}
	if !plan.Hostname.IsNull() && !plan.Hostname.IsUnknown() {
		v := plan.Hostname.ValueString()
		input.Hostname = &v
	}
	if !plan.Port.IsNull() && !plan.Port.IsUnknown() {
		v := int(plan.Port.ValueInt64())
		input.Port = &v
	}
	if !plan.HTTPMethod.IsNull() && !plan.HTTPMethod.IsUnknown() {
		v := plan.HTTPMethod.ValueString()
		input.HTTPMethod = &v
	}
	if !plan.IPVersion.IsNull() && !plan.IPVersion.IsUnknown() {
		v := plan.IPVersion.ValueString()
		input.IPVersion = &v
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		v := int(plan.IntervalSeconds.ValueInt64())
		input.IntervalSeconds = &v
	}
	if !plan.TimeoutSeconds.IsNull() && !plan.TimeoutSeconds.IsUnknown() {
		v := int(plan.TimeoutSeconds.ValueInt64())
		input.TimeoutSeconds = &v
	}
	if !plan.ConfirmationCount.IsNull() && !plan.ConfirmationCount.IsUnknown() {
		v := int(plan.ConfirmationCount.ValueInt64())
		input.ConfirmationCount = &v
	}
	if !plan.Keyword.IsNull() && !plan.Keyword.IsUnknown() {
		v := plan.Keyword.ValueString()
		input.Keyword = &v
	}
	if !plan.KeywordAbsent.IsNull() && !plan.KeywordAbsent.IsUnknown() {
		v := plan.KeywordAbsent.ValueBool()
		input.KeywordAbsent = &v
	}
	if !plan.FollowRedirects.IsNull() && !plan.FollowRedirects.IsUnknown() {
		v := plan.FollowRedirects.ValueBool()
		input.FollowRedirects = &v
	}
	if !plan.ExpectedStatusCodes.IsNull() && !plan.ExpectedStatusCodes.IsUnknown() {
		var codes []int
		for _, elem := range plan.ExpectedStatusCodes.Elements() {
			if v, ok := elem.(types.Int64); ok {
				codes = append(codes, int(v.ValueInt64()))
			}
		}
		input.ExpectedStatusCodes = codes
	}
	if !plan.ProbeRegionIDs.IsNull() && !plan.ProbeRegionIDs.IsUnknown() {
		var ids []int64
		for _, elem := range plan.ProbeRegionIDs.Elements() {
			if v, ok := elem.(types.Int64); ok {
				ids = append(ids, v.ValueInt64())
			}
		}
		input.ProbeRegionIDs = ids
	}
	if !plan.DNSRecordType.IsNull() && !plan.DNSRecordType.IsUnknown() {
		v := plan.DNSRecordType.ValueString()
		input.DNSRecordType = &v
	}
	if !plan.DNSExpectedRecords.IsNull() && !plan.DNSExpectedRecords.IsUnknown() {
		var records []string
		for _, elem := range plan.DNSExpectedRecords.Elements() {
			if sv, ok := elem.(types.String); ok {
				records = append(records, sv.ValueString())
			}
		}
		input.DNSExpectedRecords = records
	}
	if !plan.CustomHeaders.IsNull() && !plan.CustomHeaders.IsUnknown() {
		headers := make(map[string]string)
		for k, v := range plan.CustomHeaders.Elements() {
			if sv, ok := v.(types.String); ok {
				headers[k] = sv.ValueString()
			}
		}
		input.CustomHeaders = &headers
	}
	if !plan.CustomBody.IsNull() && !plan.CustomBody.IsUnknown() {
		v := plan.CustomBody.ValueString()
		input.CustomBody = &v
	}
	if !plan.ContentType.IsNull() && !plan.ContentType.IsUnknown() {
		v := plan.ContentType.ValueString()
		input.ContentType = &v
	}
	if !plan.RecoveryCount.IsNull() && !plan.RecoveryCount.IsUnknown() {
		v := int(plan.RecoveryCount.ValueInt64())
		input.RecoveryCount = &v
	}

	monitor, err := r.client.UpdateUptimeMonitor(id, etag, input)
	if err != nil {
		resp.Diagnostics.AddError("Error updating uptime monitor", err.Error())
		return
	}

	r.mapToState(ctx, monitor, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *uptimeMonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state uptimeMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting uptime monitor", map[string]interface{}{"id": state.ID.ValueString()})

	err := r.client.DeleteUptimeMonitor(state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting uptime monitor", err.Error())
	}
}

func (r *uptimeMonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *uptimeMonitorResource) mapToState(ctx context.Context, m *client.UptimeMonitor, state *uptimeMonitorModel, diags *diag.Diagnostics) {
	state.ID = types.StringValue(m.ID)
	state.Name = types.StringValue(m.Name)
	state.Protocol = types.StringValue(m.Protocol)
	state.URL = types.StringValue(m.URL)
	state.Hostname = types.StringValue(m.Hostname)
	if m.Port != nil {
		state.Port = types.Int64Value(int64(*m.Port))
	} else {
		state.Port = types.Int64Null()
	}
	state.HTTPMethod = types.StringValue(m.HTTPMethod)
	state.IPVersion = types.StringValue(m.IPVersion)
	state.IntervalSeconds = types.Int64Value(int64(m.IntervalSeconds))
	state.TimeoutSeconds = types.Int64Value(int64(m.TimeoutSeconds))
	state.ConfirmationCount = types.Int64Value(int64(m.ConfirmationCount))
	state.Keyword = types.StringValue(m.Keyword)
	state.KeywordAbsent = types.BoolValue(m.KeywordAbsent)
	state.FollowRedirects = types.BoolValue(m.FollowRedirects)

	// Convert expected_status_codes ([]int → []int64 for ListValueFrom)
	codes64 := make([]int64, len(m.ExpectedStatusCodes))
	for i, c := range m.ExpectedStatusCodes {
		codes64[i] = int64(c)
	}
	codesList, d := types.ListValueFrom(ctx, types.Int64Type, codes64)
	diags.Append(d...)
	state.ExpectedStatusCodes = codesList

	// Convert probe_region_ids
	regionsList, d := types.ListValueFrom(ctx, types.Int64Type, m.ProbeRegionIDs)
	diags.Append(d...)
	state.ProbeRegionIDs = regionsList

	// DNS fields
	if m.DNSRecordType != "" {
		state.DNSRecordType = types.StringValue(m.DNSRecordType)
	} else {
		state.DNSRecordType = types.StringNull()
	}
	if len(m.DNSExpectedRecords) > 0 {
		recordsList, d := types.ListValueFrom(ctx, types.StringType, m.DNSExpectedRecords)
		diags.Append(d...)
		state.DNSExpectedRecords = recordsList
	} else {
		state.DNSExpectedRecords = types.ListNull(types.StringType)
	}

	// Custom HTTP fields
	if len(m.CustomHeaders) > 0 {
		headersMap, d := types.MapValueFrom(ctx, types.StringType, m.CustomHeaders)
		diags.Append(d...)
		state.CustomHeaders = headersMap
	} else {
		state.CustomHeaders = types.MapNull(types.StringType)
	}
	if m.CustomBody != "" {
		state.CustomBody = types.StringValue(m.CustomBody)
	} else {
		state.CustomBody = types.StringNull()
	}
	if m.ContentType != "" {
		state.ContentType = types.StringValue(m.ContentType)
	} else {
		state.ContentType = types.StringNull()
	}

	state.RecoveryCount = types.Int64Value(int64(m.RecoveryCount))

	state.Status = types.StringValue(m.Status)
	state.SSLExpiresAt = optionalString(m.SSLExpiresAt)
	state.LastError = optionalString(m.LastError)
	state.NextCheckAt = optionalString(m.NextCheckAt)
	state.LastCheckAt = optionalString(m.LastCheckAt)
	state.CreatedAt = types.StringValue(m.CreatedAt)
	state.UpdatedAt = types.StringValue(m.UpdatedAt)
}
