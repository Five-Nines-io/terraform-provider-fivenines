package resources

import (
	"context"
	"fmt"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                   = &uptimeMonitorResource{}
	_ resource.ResourceWithImportState    = &uptimeMonitorResource{}
	_ resource.ResourceWithValidateConfig = &uptimeMonitorResource{}
)

// protocolRequirements lists the attributes each protocol needs. It mirrors the
// server-side validation so a bad combination fails at plan time instead of
// costing a round trip, and it is enforced on update too now that the protocol
// itself can change in place.
var protocolRequirements = map[string][]string{
	"https": {"url"},
	"tcp":   {"hostname", "port"},
	"icmp":  {"hostname"},
	"dns":   {"dns_record_type"},
}

// protocolForbidden is the inverse: the protocol-scoped attributes each protocol
// does not use. Update sends these as explicit nulls to clear them, so leaving
// one in the configuration would plan a KNOWN value that the apply then nulls —
// "Provider produced inconsistent result after apply", with no in-band way for
// the user to see it coming. Catching it here turns that into a plan-time message
// that names the attribute to delete.
//
// Only the seven attributes Update actually clears are listed. url and hostname
// are Computed and are never cleared, so they cannot produce that error.
var protocolForbidden = map[string][]string{
	"https": {"port", "dns_record_type", "dns_expected_records"},
	"tcp":   {"keyword", "dns_record_type", "dns_expected_records", "custom_headers", "custom_body", "content_type"},
	"icmp":  {"port", "keyword", "dns_record_type", "dns_expected_records", "custom_headers", "custom_body", "content_type"},
	"dns":   {"port", "keyword", "custom_headers", "custom_body", "content_type"},
}

type uptimeMonitorResource struct {
	client *client.Client
}

type uptimeMonitorModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	Protocol            types.String `tfsdk:"protocol"`
	Paused              types.Bool   `tfsdk:"paused"`
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
		Description: "Manages a FiveNines uptime monitor.\n\n" +
			"Running a check on demand is deliberately outside Terraform's scope: it is a one-off " +
			"action with no desired state to converge on. Use `POST /api/v1/uptime_monitors/{id}/check_now` " +
			"(the same endpoint as the dashboard's \"Check now\" button) for that. It returns 202, runs " +
			"asynchronously, and is rate limited to one call per monitor per 60 seconds and ten per " +
			"organization per minute.",
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
				Description: `Protocol: "https", "tcp", "icmp", or "dns". Can be changed in place; ` +
					`the attributes the new protocol requires must be set in the same plan. Switching ` +
					`clears the attributes the new protocol does not use (port, keyword, ` +
					`dns_record_type, dns_expected_records, custom_headers, custom_body, content_type). ` +
					`url and hostname are computed and are retained across the switch.`,
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("https", "tcp", "icmp", "dns"),
				},
			},
			"paused": schema.BoolAttribute{
				Description: "Whether the monitor is paused.",
				Optional:    true,
				Computed:    true,
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
				Description: "Port to monitor (required for tcp protocol). 1-65535.",
				Optional:    true,
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
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
				Description: "Keyword that must be present in the response body. https only: the other protocols reject it at plan time.",
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
				Description: "Expected HTTP status codes. 1-50 codes, each between 100 and 599. " +
					"An empty list is rejected because it would match nothing. Defaults to [200] on " +
					"create; because the value is computed, removing it later keeps the last applied " +
					"codes rather than resetting to the default.",
				Optional:    true,
				Computed:    true,
				ElementType: types.Int64Type,
				Validators: []validator.List{
					listvalidator.SizeBetween(1, 50),
					listvalidator.ValueInt64sAre(int64validator.Between(100, 599)),
				},
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
				Description: "Expected DNS record values. Up to 50 records of at most 2048 characters each. " +
					"Set to an empty list to pin no expectation.",
				Optional:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.SizeAtMost(50),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtMost(2048)),
				},
			},
			"custom_headers": schema.MapAttribute{
				Description: "Custom HTTP headers as key-value pairs. https only: the other protocols " +
					"reject it at plan time. Marked sensitive: this is where an Authorization " +
					"header for the monitored endpoint goes.",
				Optional:    true,
				Sensitive:   true,
				ElementType: types.StringType,
			},
			"custom_body": schema.StringAttribute{
				Description: "Request body for POST requests. https only: the other protocols reject it at plan time.",
				Optional:    true,
				Sensitive:   true,
			},
			"content_type": schema.StringAttribute{
				Description: `Content-Type header: "application/json", "application/x-www-form-urlencoded", or "text/plain". ` +
					`https only: the other protocols reject it at plan time.`,
				Optional: true,
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
				Description: `Current status: "unknown", "up", "down", "paused" or "recovering".`,
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

// ValidateConfig enforces the protocol-specific required attributes listed in
// protocolRequirements.
func (r *uptimeMonitorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config uptimeMonitorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.Protocol.IsNull() || config.Protocol.IsUnknown() {
		return
	}

	protocol := config.Protocol.ValueString()
	for _, name := range missingProtocolAttributes(config) {
		resp.Diagnostics.AddAttributeError(
			path.Root(name),
			"Missing required attribute",
			fmt.Sprintf("%q monitors require %q to be set.", protocol, name),
		)
	}
	for _, name := range forbiddenProtocolAttributes(config) {
		resp.Diagnostics.AddAttributeError(
			path.Root(name),
			"Attribute not used by this protocol",
			fmt.Sprintf("%q monitors do not use %q, and updating clears it server-side. "+
				"Remove it from the configuration.", protocol, name),
		)
	}
}

// missingProtocolAttributes returns the attributes that config's protocol
// requires but leaves unset, in schema order.
func protocolScopedValues(config uptimeMonitorModel) map[string]attr.Value {
	return map[string]attr.Value{
		"url":                  config.URL,
		"hostname":             config.Hostname,
		"port":                 config.Port,
		"keyword":              config.Keyword,
		"dns_record_type":      config.DNSRecordType,
		"dns_expected_records": config.DNSExpectedRecords,
		"custom_headers":       config.CustomHeaders,
		"custom_body":          config.CustomBody,
		"content_type":         config.ContentType,
	}
}

func missingProtocolAttributes(config uptimeMonitorModel) []string {
	values := protocolScopedValues(config)

	var missing []string
	for _, name := range protocolRequirements[config.Protocol.ValueString()] {
		// Comma-ok: protocolRequirements and values are two tables that have to
		// stay in step, and indexing a missing name would hand .IsNull() a nil
		// interface and panic the provider mid-plan. Report it as missing instead.
		v, ok := values[name]
		// An unknown value comes from a reference that has not been resolved yet
		// and may well turn out to be set, so only a null is a genuine omission.
		if !ok || v.IsNull() {
			missing = append(missing, name)
		}
	}
	return missing
}

// forbiddenProtocolAttributes returns the attributes set in config that its
// protocol does not use, in schema order.
func forbiddenProtocolAttributes(config uptimeMonitorModel) []string {
	values := protocolScopedValues(config)

	var forbidden []string
	for _, name := range protocolForbidden[config.Protocol.ValueString()] {
		// Only a value the user actually set can be a known plan value, so
		// unknowns and nulls are both fine here.
		if v, ok := values[name]; ok && !v.IsNull() && !v.IsUnknown() {
			forbidden = append(forbidden, name)
		}
	}
	return forbidden
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

	monitor, err := r.client.CreateUptimeMonitor(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating uptime monitor", err.Error())
		return
	}

	// Monitors are created running, so an explicit paused = true is a follow-up call.
	if !plan.Paused.IsNull() && !plan.Paused.IsUnknown() && plan.Paused.ValueBool() {
		paused, err := r.client.PauseUptimeMonitor(ctx, monitor.ID)
		if err != nil {
			// The monitor already exists server-side. Terraform taints a resource whose
			// Create errors with state set, so the next apply replaces it — that still
			// beats writing no state, which would leak this monitor and create a second
			// one on the next apply.
			r.mapToState(ctx, monitor, &plan, &resp.Diagnostics)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.AddError("Error pausing uptime monitor after creation",
				"The monitor was created but could not be paused. It is recorded as tainted, so the next apply "+
					"will destroy and recreate it (losing its check history).\n\n"+err.Error())
			return
		}
		monitor = paused
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

	monitor, _, err := r.client.GetUptimeMonitor(ctx, state.ID.ValueString())
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

	input := client.UpdateUptimeMonitorInput{}
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() {
		v := plan.Name.ValueString()
		input.Name = &v
	}
	if !plan.Protocol.IsNull() && !plan.Protocol.IsUnknown() {
		v := plan.Protocol.ValueString()
		input.Protocol = &v
	}
	if !plan.URL.IsNull() && !plan.URL.IsUnknown() {
		v := plan.URL.ValueString()
		input.URL = &v
	}
	if !plan.Hostname.IsNull() && !plan.Hostname.IsUnknown() {
		v := plan.Hostname.ValueString()
		input.Hostname = &v
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
		// Non-nil even when empty, so `probe_region_ids = []` reaches the API
		// instead of leaving the stored region set in place.
		ids := make([]int64, 0, len(plan.ProbeRegionIDs.Elements()))
		for _, elem := range plan.ProbeRegionIDs.Elements() {
			if v, ok := elem.(types.Int64); ok {
				ids = append(ids, v.ValueInt64())
			}
		}
		input.ProbeRegionIDs = &ids
	}
	if !plan.RecoveryCount.IsNull() && !plan.RecoveryCount.IsUnknown() {
		v := int(plan.RecoveryCount.ValueInt64())
		input.RecoveryCount = &v
	}

	// Protocol-scoped attributes are assigned unconditionally: a null plan value
	// becomes a nil pointer, which serialises as JSON null and clears the stored
	// value. Omitting them instead would leave the previous protocol's settings
	// behind, and the API would keep echoing values the plan says are null —
	// "Provider produced inconsistent result after apply" on the next apply.
	if !plan.Port.IsNull() && !plan.Port.IsUnknown() {
		v := int(plan.Port.ValueInt64())
		input.Port = &v
	}
	if !plan.Keyword.IsNull() && !plan.Keyword.IsUnknown() {
		v := plan.Keyword.ValueString()
		input.Keyword = &v
	}
	if !plan.DNSRecordType.IsNull() && !plan.DNSRecordType.IsUnknown() {
		v := plan.DNSRecordType.ValueString()
		input.DNSRecordType = &v
	}
	if !plan.DNSExpectedRecords.IsNull() && !plan.DNSExpectedRecords.IsUnknown() {
		// Non-nil even when empty: the API normalises [] to "no expectation
		// pinned", which is the same end state as null but keeps the plan's [].
		records := make([]string, 0, len(plan.DNSExpectedRecords.Elements()))
		for _, elem := range plan.DNSExpectedRecords.Elements() {
			if sv, ok := elem.(types.String); ok {
				records = append(records, sv.ValueString())
			}
		}
		input.DNSExpectedRecords = &records
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

	var monitor *client.UptimeMonitor
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetUptimeMonitor(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading uptime monitor for update", err.Error())
			return
		}
		monitor, err = r.client.UpdateUptimeMonitor(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on uptime monitor update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating uptime monitor", err.Error())
			return
		}
		break
	}

	// pause/resume are separate endpoints, and both echo back the monitor.
	//
	// Deciding whether to call them from the echoed `status` alone is unsafe:
	// status is probe-derived, so a monitor that is paused server-side can report
	// "unknown" and make the check conclude it is already running — the resume is
	// skipped, state says the monitor is live, and alerting stays off. Prior state
	// records the intent instead. Act unless BOTH signals agree we are already in
	// the target state; the endpoints are idempotent, so a redundant call is free.
	if !plan.Paused.IsNull() && !plan.Paused.IsUnknown() {
		wantPaused := plan.Paused.ValueBool()
		// Prior state records what the last apply or refresh established, so it is
		// the authoritative signal. The echoed status is only the fallback (import,
		// first apply): it is probe-derived, so a monitor that is paused
		// server-side can report "unknown", and believing that would skip the
		// resume and leave alerting off while state claims the monitor is live.
		isPaused := monitor.Status == client.StatusPaused
		if !state.Paused.IsNull() && !state.Paused.IsUnknown() {
			isPaused = state.Paused.ValueBool()
		}

		if isPaused != wantPaused {
			action, verb := r.client.ResumeUptimeMonitor, "resuming"
			if wantPaused {
				action, verb = r.client.PauseUptimeMonitor, "pausing"
			}
			updated, err := action(ctx, id)
			if err != nil {
				// The PATCH above already landed, so the monitor's configuration has
				// changed server-side. An Update that errors does NOT taint the
				// resource, so returning without writing state would leave Terraform
				// holding the pre-update values with no marker that they are stale.
				r.mapToState(ctx, monitor, &plan, &resp.Diagnostics)
				resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
				resp.Diagnostics.AddError("Error "+verb+" uptime monitor",
					"The monitor's configuration was updated but its paused state was not changed. "+
						"State reflects the update; re-apply to retry.\n\n"+err.Error())
				return
			}
			monitor = updated
		}
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

	err := r.client.DeleteUptimeMonitor(ctx, state.ID.ValueString())
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
	state.Paused = types.BoolValue(m.Status == client.StatusPaused)
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
	switch {
	case m.Keyword != "":
		state.Keyword = types.StringValue(m.Keyword)
	case isKnownEmptyString(state.Keyword):
		// `keyword = ""` is a legal config; keep the pinned empty string rather
		// than flipping it to null, exactly as for empty lists and maps.
	default:
		state.Keyword = types.StringNull()
	}
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
	switch {
	case len(m.DNSExpectedRecords) > 0:
		recordsList, d := types.ListValueFrom(ctx, types.StringType, m.DNSExpectedRecords)
		diags.Append(d...)
		state.DNSExpectedRecords = recordsList
	case isEmptyList(state.DNSExpectedRecords):
		// The API normalises a pinned [] to "no expectation" and reads it back as
		// [], so keep the empty list the config asked for rather than flipping it
		// to null and diffing forever.
	default:
		state.DNSExpectedRecords = types.ListNull(types.StringType)
	}

	// Custom HTTP fields
	switch {
	case len(m.CustomHeaders) > 0:
		headersMap, d := types.MapValueFrom(ctx, types.StringType, m.CustomHeaders)
		diags.Append(d...)
		state.CustomHeaders = headersMap
	case isEmptyMap(state.CustomHeaders):
		// Same pinned-empty rule as dns_expected_records: a config of {} must not
		// come back null, or the plan re-proposes {} and every apply fails.
	default:
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

// isEmptyList reports whether v is a known, non-null list with no elements.
func isEmptyList(v types.List) bool {
	return !v.IsNull() && !v.IsUnknown() && len(v.Elements()) == 0
}

// isEmptyMap reports whether v is a known, non-null map with no elements.
func isEmptyMap(v types.Map) bool {
	return !v.IsNull() && !v.IsUnknown() && len(v.Elements()) == 0
}

// isKnownEmptyString reports whether v is a known, non-null empty string.
func isKnownEmptyString(v types.String) bool {
	return !v.IsNull() && !v.IsUnknown() && v.ValueString() == ""
}
