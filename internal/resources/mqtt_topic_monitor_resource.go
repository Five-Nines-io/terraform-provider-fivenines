package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                   = &mqttTopicMonitorResource{}
	_ resource.ResourceWithImportState    = &mqttTopicMonitorResource{}
	_ resource.ResourceWithValidateConfig = &mqttTopicMonitorResource{}
)

type mqttTopicMonitorResource struct {
	client *client.Client
}

type mqttTopicMonitorModel struct {
	ID                      types.String `tfsdk:"id"`
	MQTTBrokerID            types.String `tfsdk:"mqtt_broker_id"`
	TopicFilter             types.String `tfsdk:"topic_filter"`
	StaleAfterSeconds       types.Int64  `tfsdk:"stale_after_seconds"`
	MatchKind               types.String `tfsdk:"match_kind"`
	ExpectedValue           types.String `tfsdk:"expected_value"`
	JSONKey                 types.String `tfsdk:"json_key"`
	CapturePayload          types.Bool   `tfsdk:"capture_payload"`
	EffectiveCapturePayload types.Bool   `tfsdk:"effective_capture_payload"`
	FreshnessCheck          types.Bool   `tfsdk:"freshness_check"`
	PayloadCheck            types.Bool   `tfsdk:"payload_check"`
	ExactTopic              types.Bool   `tfsdk:"exact_topic"`
	SubscribedSince         types.String `tfsdk:"subscribed_since"`
	Capped                  types.Bool   `tfsdk:"capped"`
	CreatedAt               types.String `tfsdk:"created_at"`
	UpdatedAt               types.String `tfsdk:"updated_at"`
}

func NewMQTTTopicMonitorResource() resource.Resource {
	return &mqttTopicMonitorResource{}
}

func (r *mqttTopicMonitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mqtt_topic_monitor"
}

func (r *mqttTopicMonitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages one topic monitor on a FiveNines MQTT broker: a freshness timeout, a payload " +
			"expectation, or both. Each monitor counts 1 toward the plan's monitor limit, alongside instances, " +
			"uptime monitors, network devices and tasks.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier (UUID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"mqtt_broker_id": schema.StringAttribute{
				Description: "UUID of the broker this monitor watches. Monitors live under their broker, so " +
					"pointing one at a different broker replaces it.",
				Required: true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"topic_filter": schema.StringAttribute{
				Description: "An exact topic or an MQTT 3.1.1 filter: `+` matches one level and must occupy it " +
					"whole, `#` matches the rest and is only valid as the final level.",
				Required: true,
			},
			"stale_after_seconds": schema.Int64Attribute{
				Description: "Fire when no live message has arrived on a matched topic for this long (5–604800). " +
					"Omit for no freshness check. Retained messages never count as fresh, so a dead device holding " +
					"a retained payload still goes stale.",
				Optional: true,
				Validators: []validator.Int64{
					int64validator.Between(5, 604800),
				},
			},
			"match_kind": schema.StringAttribute{
				Description: "Payload expectation: `exact`, `contains` or `json_key`. Omit for no payload check. " +
					"The monitor fires while the last payload does not match.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("exact", "contains", "json_key"),
				},
			},
			"expected_value": schema.StringAttribute{
				Description: "The payload value to expect. Required when `match_kind` is set.",
				Optional:    true,
			},
			"json_key": schema.StringAttribute{
				Description: "Key to read from a JSON payload. Required when `match_kind` is `json_key`. A dotted " +
					"path digs nested objects (`battery.level`); a bare key reads the top level.",
				Optional: true,
			},
			"capture_payload": schema.BoolAttribute{
				Description: "Whether the agent retains the last payload for this monitor. A payload expectation " +
					"forces capture on regardless — read `effective_capture_payload` for what the agent is actually told.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"effective_capture_payload": schema.BoolAttribute{
				Description: "What the agent is told, which is `capture_payload` OR `payload_check`: a payload " +
					"expectation would otherwise read a permanent null.",
				Computed: true,
			},
			"freshness_check": schema.BoolAttribute{
				Description: "Derived — true when `stale_after_seconds` is set.",
				Computed:    true,
			},
			"payload_check": schema.BoolAttribute{
				Description: "Derived — true when `match_kind` is set.",
				Computed:    true,
			},
			"exact_topic": schema.BoolAttribute{
				Description: "Derived — true when the filter carries no wildcard. Only an exact-topic freshness " +
					"monitor can alert on a device that was already silent when the monitor was created; a wildcard " +
					"has to see a message before it knows the topic exists.",
				Computed: true,
			},
			"subscribed_since": schema.StringAttribute{
				Description: "When the watcher's subscription for this monitor became healthy. Null means it does " +
					"not hold one, so no freshness alarm can fire yet — freshness alarms arm per monitor, because " +
					"nothing can be known about a window nobody was watching.",
				Computed: true,
			},
			"capped": schema.BoolAttribute{
				Description: "True when wildcard discovery hit its per-monitor topic cap on the last tick, so the " +
					"observed topic set is a floor rather than the whole truth.",
				Computed: true,
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

func (r *mqttTopicMonitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *mqttTopicMonitorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config mqttTopicMonitorModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateMQTTTopicMonitorChecks(config)...)
}

// validateMQTTTopicMonitorChecks enforces the rules the API 422s on: a monitor
// needs at least one check, and a payload expectation brings its own fields.
// Failing at plan time keeps a generated fleet of monitors from getting halfway
// applied before the server refuses one.
func validateMQTTTopicMonitorChecks(config mqttTopicMonitorModel) diag.Diagnostics {
	var diags diag.Diagnostics

	// Unknown values only resolve at apply time, so leave those to the API.
	staleKnown := !config.StaleAfterSeconds.IsUnknown()
	matchKnown := !config.MatchKind.IsUnknown()

	if staleKnown && matchKnown && config.StaleAfterSeconds.IsNull() && config.MatchKind.IsNull() {
		diags.AddError(
			"Monitor has no check",
			`A topic monitor needs "stale_after_seconds", "match_kind", or both — otherwise there is nothing for it to alert on.`,
		)
	}

	if matchKnown && !config.MatchKind.IsNull() {
		if config.ExpectedValue.IsNull() {
			diags.AddAttributeError(
				path.Root("expected_value"),
				"Missing required attribute",
				`"expected_value" is required when "match_kind" is set.`,
			)
		}
		if config.MatchKind.ValueString() == "json_key" && config.JSONKey.IsNull() {
			diags.AddAttributeError(
				path.Root("json_key"),
				"Missing required attribute",
				`"json_key" is required when "match_kind" is "json_key".`,
			)
		}
	}

	return diags
}

func (r *mqttTopicMonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mqttTopicMonitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	brokerID := plan.MQTTBrokerID.ValueString()
	input := client.CreateMQTTTopicMonitorInput{
		TopicFilter:       plan.TopicFilter.ValueString(),
		StaleAfterSeconds: int64Ptr(plan.StaleAfterSeconds),
		MatchKind:         stringPtr(plan.MatchKind),
		ExpectedValue:     stringPtr(plan.ExpectedValue),
		JSONKey:           stringPtr(plan.JSONKey),
		CapturePayload:    boolPtr(plan.CapturePayload),
	}

	tflog.Debug(ctx, "Creating MQTT topic monitor", map[string]interface{}{
		"mqtt_broker_id": brokerID,
		"topic_filter":   input.TopicFilter,
	})

	monitor, err := r.client.CreateMQTTTopicMonitor(ctx, brokerID, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating MQTT topic monitor", mqttErrorDetail(err))
		return
	}

	mapMQTTTopicMonitorToState(monitor, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mqttTopicMonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mqttTopicMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitor, _, err := r.client.GetMQTTTopicMonitor(ctx, state.MQTTBrokerID.ValueString(), state.ID.ValueString())
	if err != nil {
		// A deleted broker takes its monitors with it, so a 404 here can mean
		// either row is gone — both leave nothing to refresh.
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading MQTT topic monitor", mqttErrorDetail(err))
		return
	}

	mapMQTTTopicMonitorToState(monitor, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *mqttTopicMonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mqttTopicMonitorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state mqttTopicMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	brokerID := state.MQTTBrokerID.ValueString()
	id := state.ID.ValueString()
	// Every check is sent on every update: dropping one from the configuration
	// has to reach the API as an explicit null, or a monitor could never lose a
	// check it once had. The tags on UpdateMQTTTopicMonitorInput carry that.
	input := client.UpdateMQTTTopicMonitorInput{
		TopicFilter:       stringPtr(plan.TopicFilter),
		StaleAfterSeconds: int64Ptr(plan.StaleAfterSeconds),
		MatchKind:         stringPtr(plan.MatchKind),
		ExpectedValue:     stringPtr(plan.ExpectedValue),
		JSONKey:           stringPtr(plan.JSONKey),
		CapturePayload:    boolPtr(plan.CapturePayload),
	}

	// ETag retry loop
	var monitor *client.MQTTTopicMonitor
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetMQTTTopicMonitor(ctx, brokerID, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading MQTT topic monitor for update", mqttErrorDetail(err))
			return
		}
		monitor, err = r.client.UpdateMQTTTopicMonitor(ctx, brokerID, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on MQTT topic monitor update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating MQTT topic monitor", mqttErrorDetail(err))
			return
		}
		break
	}

	mapMQTTTopicMonitorToState(monitor, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mqttTopicMonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mqttTopicMonitorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting MQTT topic monitor", map[string]interface{}{"id": state.ID.ValueString()})

	err := r.client.DeleteMQTTTopicMonitor(ctx, state.MQTTBrokerID.ValueString(), state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting MQTT topic monitor", mqttErrorDetail(err))
	}
}

// ImportState takes "<mqtt_broker_id>:<id>" — a monitor is only addressable
// under its broker, so the broker UUID is part of its identity.
func (r *mqttTopicMonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	brokerID, id, found := strings.Cut(req.ID, ":")
	if !found || brokerID == "" || id == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected \"<mqtt_broker_id>:<id>\", got %q.", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("mqtt_broker_id"), types.StringValue(brokerID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.StringValue(id))...)
}

func mapMQTTTopicMonitorToState(m *client.MQTTTopicMonitor, state *mqttTopicMonitorModel) {
	state.ID = types.StringValue(m.ID)
	state.MQTTBrokerID = types.StringValue(m.MQTTBrokerID)
	state.TopicFilter = types.StringValue(m.TopicFilter)
	state.StaleAfterSeconds = optionalInt64(m.StaleAfterSeconds)
	state.MatchKind = optionalString(m.MatchKind)
	state.ExpectedValue = optionalString(m.ExpectedValue)
	state.JSONKey = optionalString(m.JSONKey)
	state.CapturePayload = types.BoolValue(m.CapturePayload)
	state.EffectiveCapturePayload = types.BoolValue(m.EffectiveCapturePayload)
	state.FreshnessCheck = types.BoolValue(m.FreshnessCheck)
	state.PayloadCheck = types.BoolValue(m.PayloadCheck)
	state.ExactTopic = types.BoolValue(m.ExactTopic)
	state.SubscribedSince = optionalString(m.SubscribedSince)
	state.Capped = types.BoolValue(m.Capped)
	state.CreatedAt = types.StringValue(m.CreatedAt)
	state.UpdatedAt = types.StringValue(m.UpdatedAt)
}
