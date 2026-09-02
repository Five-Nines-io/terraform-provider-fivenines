package resources

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                = &mqttBrokerResource{}
	_ resource.ResourceWithImportState = &mqttBrokerResource{}
)

type mqttBrokerResource struct {
	client *client.Client
}

type mqttBrokerModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Host              types.String `tfsdk:"host"`
	Port              types.Int64  `tfsdk:"port"`
	TLS               types.Bool   `tfsdk:"tls"`
	Username          types.String `tfsdk:"username"`
	Password          types.String `tfsdk:"password"`
	UsernameSet       types.Bool   `tfsdk:"username_set"`
	PasswordSet       types.Bool   `tfsdk:"password_set"`
	WatcherHostID     types.String `tfsdk:"watcher_host_id"`
	Status            types.String `tfsdk:"status"`
	LastErrorMessage  types.String `tfsdk:"last_error_message"`
	LastConnectedAt   types.String `tfsdk:"last_connected_at"`
	LastSyncedAt      types.String `tfsdk:"last_synced_at"`
	Stale             types.Bool   `tfsdk:"stale"`
	TopicMonitorCount types.Int64  `tfsdk:"topic_monitor_count"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func NewMQTTBrokerResource() resource.Resource {
	return &mqttBrokerResource{}
}

func (r *mqttBrokerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_mqtt_broker"
}

func (r *mqttBrokerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines MQTT broker. The broker itself is free and consumes no monitor quota — the " +
			"`fivenines_mqtt_topic_monitor` resources under it are the billable unit. A broker is inert until " +
			"`watcher_host_id` names an instance: that agent holds the subscription and reports topic state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier (UUID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the broker. Unique within the organization.",
				Required:    true,
			},
			"host": schema.StringAttribute{
				Description: "Broker hostname or IP, as the watcher agent reaches it.",
				Required:    true,
			},
			"port": schema.Int64Attribute{
				Description: "Broker port. Defaults to 1883 (8883 is the usual TLS port).",
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1883),
				Validators: []validator.Int64{
					int64validator.Between(1, 65535),
				},
			},
			"tls": schema.BoolAttribute{
				Description: "Whether the watcher connects over TLS.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"username": schema.StringAttribute{
				Description: "Broker username. Write-only — the API never returns it, so `username_set` reports " +
					"whether one is stored. Setting it rotates the stored value; dropping it from the " +
					"configuration leaves that value alone rather than wiping a credential Terraform cannot " +
					"read back. Clear one from the dashboard.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					// An empty string means "keep the stored value" to the API, which would
					// leave state claiming "" while the server still holds a credential. The
					// same reason task.host_id and instance secrets reject one.
					stringvalidator.LengthAtLeast(1),
				},
			},
			"password": schema.StringAttribute{
				Description: "Broker password. Write-only — the API never returns it, so `password_set` reports " +
					"whether one is stored. Same preserve-on-omission rule as `username`.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"username_set": schema.BoolAttribute{
				Description: "Whether a username is stored for this broker.",
				Computed:    true,
			},
			"password_set": schema.BoolAttribute{
				Description: "Whether a password is stored for this broker.",
				Computed:    true,
			},
			"watcher_host_id": schema.StringAttribute{
				Description: "UUID of the instance that subscribes to this broker and reports topic state. Must be " +
					"an instance in your organization — the watcher is shipped these credentials decrypted. Until " +
					"one is assigned, nothing is collected.",
				Optional: true,
			},
			"status": schema.StringAttribute{
				Description: "Health of the subscription, not of the monitored devices: `unknown`, `connected`, " +
					"`unreachable` (the agent reached the box and the connection failed) or `config_error` (bad " +
					"credentials or TLS). Read it together with `stale`.",
				Computed: true,
			},
			"last_error_message": schema.StringAttribute{
				Description: "The agent's error detail for the last failed connection attempt.",
				Computed:    true,
			},
			"last_connected_at": schema.StringAttribute{
				Description: "When the watcher last connected to the broker.",
				Computed:    true,
			},
			"last_synced_at": schema.StringAttribute{
				Description: "When the watcher agent last reported this broker.",
				Computed:    true,
			},
			"stale": schema.BoolAttribute{
				Description: "True when the watcher agent has not reported this broker within 10 minutes — `status` " +
					"is then a last-known state, not a current one.",
				Computed: true,
			},
			"topic_monitor_count": schema.Int64Attribute{
				Description: "How many topic monitors are configured on this broker.",
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

func (r *mqttBrokerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *mqttBrokerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan mqttBrokerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateMQTTBrokerInput{
		Name:          plan.Name.ValueString(),
		Host:          plan.Host.ValueString(),
		Port:          intPtr(plan.Port),
		TLS:           boolPtr(plan.TLS),
		Username:      stringPtr(plan.Username),
		Password:      stringPtr(plan.Password),
		WatcherHostID: stringPtr(plan.WatcherHostID),
	}

	tflog.Debug(ctx, "Creating MQTT broker", map[string]interface{}{"name": input.Name, "host": input.Host})

	broker, err := r.client.CreateMQTTBroker(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating MQTT broker", mqttErrorDetail(err))
		return
	}

	mapMQTTBrokerToState(broker, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mqttBrokerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state mqttBrokerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	broker, _, err := r.client.GetMQTTBroker(ctx, state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading MQTT broker", mqttErrorDetail(err))
		return
	}

	mapMQTTBrokerToState(broker, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *mqttBrokerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan mqttBrokerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state mqttBrokerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	input := client.UpdateMQTTBrokerInput{
		Name:     stringPtr(plan.Name),
		Host:     stringPtr(plan.Host),
		Port:     intPtr(plan.Port),
		TLS:      boolPtr(plan.TLS),
		Username: stringPtr(plan.Username),
		Password: stringPtr(plan.Password),
		// Optional-only: dropping it from the configuration unassigns the watcher.
		WatcherHostID: stringPtr(plan.WatcherHostID),
	}

	// ETag retry loop
	var broker *client.MQTTBroker
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetMQTTBroker(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading MQTT broker for update", mqttErrorDetail(err))
			return
		}
		broker, err = r.client.UpdateMQTTBroker(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on MQTT broker update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating MQTT broker", mqttErrorDetail(err))
			return
		}
		break
	}

	mapMQTTBrokerToState(broker, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *mqttBrokerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state mqttBrokerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting MQTT broker", map[string]interface{}{"id": state.ID.ValueString()})

	err := r.client.DeleteMQTTBroker(ctx, state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting MQTT broker", mqttErrorDetail(err))
	}
}

func (r *mqttBrokerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapMQTTBrokerToState(b *client.MQTTBroker, state *mqttBrokerModel) {
	state.ID = types.StringValue(b.ID)
	state.Name = types.StringValue(b.Name)
	state.Host = types.StringValue(b.Host)
	state.Port = types.Int64Value(int64(b.Port))
	state.TLS = types.BoolValue(b.TLS)
	// username and password are write-only: the API never returns either, so the
	// configured values stay as they are and the _set booleans carry the truth.
	state.UsernameSet = types.BoolValue(b.UsernameSet)
	state.PasswordSet = types.BoolValue(b.PasswordSet)
	state.WatcherHostID = optionalString(b.WatcherHostID)
	state.Status = types.StringValue(b.Status)
	state.LastErrorMessage = optionalString(b.LastErrorMessage)
	state.LastConnectedAt = optionalString(b.LastConnectedAt)
	state.LastSyncedAt = optionalString(b.LastSyncedAt)
	state.Stale = types.BoolValue(b.Stale)
	state.TopicMonitorCount = types.Int64Value(int64(b.TopicMonitorCount))
	state.CreatedAt = types.StringValue(b.CreatedAt)
	state.UpdatedAt = types.StringValue(b.UpdatedAt)
}

// mqttErrorDetail names the account state behind the two status codes that are
// about entitlement rather than about the request. MQTT is a gated, billable
// feature, so the bare "API error 403" an operator would otherwise read sends
// them to check an API key that is working fine.
func mqttErrorDetail(err error) string {
	apiErr, ok := err.(*client.APIError)
	if !ok {
		return err.Error()
	}

	switch apiErr.StatusCode {
	case http.StatusPaymentRequired:
		return fmt.Sprintf("%s\n\nThe organization's access is restricted — a suspended or lapsed "+
			"subscription. Settle billing in the FiveNines dashboard; the API key itself is valid.", apiErr.Error())
	case http.StatusForbidden:
		return fmt.Sprintf("%s\n\nMQTT monitoring is enabled per organization, and writes need an API key "+
			"with write scope. Check both under Settings > API — an invalid key would have failed with 401, "+
			"not 403.", apiErr.Error())
	}
	return apiErr.Error()
}
