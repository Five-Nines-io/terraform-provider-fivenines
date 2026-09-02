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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                   = &taskResource{}
	_ resource.ResourceWithImportState    = &taskResource{}
	_ resource.ResourceWithValidateConfig = &taskResource{}
)

type taskResource struct {
	client *client.Client
}

type taskModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	ScheduleType       types.String `tfsdk:"schedule_type"`
	Paused             types.Bool   `tfsdk:"paused"`
	Schedule           types.String `tfsdk:"schedule"`
	IntervalSeconds    types.Int64  `tfsdk:"interval_seconds"`
	GracePeriodMinutes types.Int64  `tfsdk:"grace_period_minutes"`
	TimeZone           types.String `tfsdk:"time_zone"`
	HostID             types.String `tfsdk:"host_id"`
	Status             types.String `tfsdk:"status"`
	MonitoringStatus   types.String `tfsdk:"monitoring_status"`
	PingKey            types.String `tfsdk:"ping_key"`
	PingURL            types.String `tfsdk:"ping_url"`
	ExpectedPingAt     types.String `tfsdk:"expected_ping_at"`
	LastPingAt         types.String `tfsdk:"last_ping_at"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewTaskResource() resource.Resource {
	return &taskResource{}
}

func (r *taskResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task"
}

func (r *taskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines task (cron/heartbeat monitor).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier (UUID).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the task.",
				Required:    true,
			},
			"schedule_type": schema.StringAttribute{
				Description: `Schedule type: "cron" or "interval".`,
				Required:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("cron", "interval"),
				},
			},
			"paused": schema.BoolAttribute{
				Description: "Whether the task is paused.",
				Optional:    true,
				Computed:    true,
			},
			"schedule": schema.StringAttribute{
				Description: `Cron expression. Required while schedule_type is "cron". Once schedule_type is "interval" you may drop it, but the API keeps the last value rather than clearing it.`,
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"interval_seconds": schema.Int64Attribute{
				Description: `Interval in seconds. Required while schedule_type is "interval". Once schedule_type is "cron" you may drop it, but the API keeps the last value rather than clearing it.`,
				Optional:    true,
				Computed:    true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"grace_period_minutes": schema.Int64Attribute{
				Description: "Grace period in minutes before marking as missed.",
				Optional:    true,
				Computed:    true,
			},
			"time_zone": schema.StringAttribute{
				Description: "Time zone for cron schedule.",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("UTC"),
			},
			"host_id": schema.StringAttribute{
				Description: "Optional host ID to associate this task with.",
				Optional:    true,
			},
			"status": schema.StringAttribute{
				Description: "Current status.",
				Computed:    true,
			},
			"monitoring_status": schema.StringAttribute{
				Description: "Monitoring status.",
				Computed:    true,
			},
			"ping_key": schema.StringAttribute{
				Description: "Ping key for sending heartbeats. Server-generated, and stored in Terraform state — treat the state file as a secret. The key only authenticates heartbeats for this one task; replace the task to issue a new one.",
				Computed:    true,
				Sensitive:   true,
			},
			"ping_url": schema.StringAttribute{
				Description: "URL to send heartbeat pings to. Embeds ping_key, so it carries the same secret and the same state-file caveat.",
				Computed:    true,
				Sensitive:   true,
			},
			"expected_ping_at": schema.StringAttribute{
				Description: "Next expected ping time.",
				Computed:    true,
			},
			"last_ping_at": schema.StringAttribute{
				Description: "Last ping received time.",
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

func (r *taskResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *taskResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config taskModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateTaskSchedule(config)...)
}

// validateTaskSchedule enforces the schedule_type-specific required fields.
// schedule_type is updatable in place, so a config can move between the two shapes
// and each half has to bring its own field along.
func validateTaskSchedule(config taskModel) diag.Diagnostics {
	var diags diag.Diagnostics

	// Unknown values only resolve at apply time, so leave them to the API.
	if config.ScheduleType.IsNull() || config.ScheduleType.IsUnknown() {
		return diags
	}

	switch config.ScheduleType.ValueString() {
	case "cron":
		if config.Schedule.IsNull() {
			diags.AddAttributeError(
				path.Root("schedule"),
				"Missing required attribute",
				`"schedule" is required when "schedule_type" is "cron".`,
			)
		}
	case "interval":
		if config.IntervalSeconds.IsNull() {
			diags.AddAttributeError(
				path.Root("interval_seconds"),
				"Missing required attribute",
				`"interval_seconds" is required when "schedule_type" is "interval".`,
			)
		}
	}

	return diags
}

func (r *taskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan taskModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateTaskInput{
		Name:         plan.Name.ValueString(),
		ScheduleType: plan.ScheduleType.ValueString(),
	}
	if !plan.Schedule.IsNull() && !plan.Schedule.IsUnknown() {
		input.Schedule = plan.Schedule.ValueString()
	}
	if !plan.IntervalSeconds.IsNull() && !plan.IntervalSeconds.IsUnknown() {
		v := plan.IntervalSeconds.ValueInt64()
		input.IntervalSeconds = &v
	}
	if !plan.GracePeriodMinutes.IsNull() && !plan.GracePeriodMinutes.IsUnknown() {
		v := int(plan.GracePeriodMinutes.ValueInt64())
		input.GracePeriodMinutes = &v
	}
	if !plan.TimeZone.IsNull() && !plan.TimeZone.IsUnknown() {
		input.TimeZone = plan.TimeZone.ValueString()
	}
	if !plan.HostID.IsNull() && !plan.HostID.IsUnknown() {
		input.HostID = plan.HostID.ValueString()
	}

	tflog.Debug(ctx, "Creating task", map[string]interface{}{"name": input.Name})

	task, err := r.client.CreateTask(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating task", err.Error())
		return
	}

	// Handle pause state after creation
	if !plan.Paused.IsNull() && plan.Paused.ValueBool() {
		paused, err := r.client.PauseTask(ctx, task.ID)
		if err != nil {
			// The task already exists server-side. Terraform taints a resource whose
			// Create errors with state set, so the next apply replaces it — that still
			// beats writing no state, which would leak this task and create a second
			// one on the next apply.
			mapTaskToState(task, &plan)
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			resp.Diagnostics.AddError("Error pausing task after creation",
				"The task was created but could not be paused. It is recorded as tainted, so the next apply "+
					"will destroy and recreate it (issuing a new ping_key).\n\n"+err.Error())
			return
		}
		task = paused
	}

	mapTaskToState(task, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *taskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state taskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	task, _, err := r.client.GetTask(ctx, state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading task", err.Error())
		return
	}

	mapTaskToState(task, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *taskResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan taskModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state taskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()

	// schedule/interval_seconds are Optional+Computed since #8 — the API keeps
	// the counterpart it already stored when you switch schedule_type — so they
	// are omitted rather than cleared when the plan has no value. host_id is
	// Optional-only and provider-owned: dropping it has to clear it server-side.
	input := client.UpdateTaskInput{
		Name:               stringPtr(plan.Name),
		ScheduleType:       stringPtr(plan.ScheduleType),
		Schedule:           stringPtr(plan.Schedule),
		IntervalSeconds:    int64Ptr(plan.IntervalSeconds),
		GracePeriodMinutes: intPtr(plan.GracePeriodMinutes),
		TimeZone:           stringPtr(plan.TimeZone),
		HostID:             stringPtr(plan.HostID),
	}

	var task *client.Task
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetTask(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading task for update", err.Error())
			return
		}
		task, err = r.client.UpdateTask(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on task update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating task", err.Error())
			return
		}
		break
	}

	// Handle pause/resume state change
	if !plan.Paused.IsNull() && !plan.Paused.IsUnknown() {
		wantPaused := plan.Paused.ValueBool()
		isPaused := task.Status == client.StatusPaused
		if wantPaused && !isPaused {
			paused, err := r.client.PauseTask(ctx, id)
			if err != nil {
				resp.Diagnostics.AddError("Error pausing task", err.Error())
				return
			}
			task = paused
		} else if !wantPaused && isPaused {
			resumed, err := r.client.ResumeTask(ctx, id)
			if err != nil {
				resp.Diagnostics.AddError("Error resuming task", err.Error())
				return
			}
			task = resumed
		}
	}

	mapTaskToState(task, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *taskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state taskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting task", map[string]interface{}{"id": state.ID.ValueString()})

	err := r.client.DeleteTask(ctx, state.ID.ValueString())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error deleting task", err.Error())
	}
}

func (r *taskResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapTaskToState(t *client.Task, state *taskModel) {
	state.ID = types.StringValue(t.ID)
	state.Name = types.StringValue(t.Name)
	state.ScheduleType = types.StringValue(t.ScheduleType)
	state.Paused = types.BoolValue(t.Status == client.StatusPaused)
	// The API answers null for the schedule of an interval task, and has been
	// seen to answer "" — both mean "no cron expression".
	state.Schedule = optionalNonEmptyString(t.Schedule)
	state.IntervalSeconds = optionalInt64(t.IntervalSeconds)
	state.GracePeriodMinutes = types.Int64Value(int64(t.GracePeriodMinutes))
	state.TimeZone = stringOrKeep(t.TimeZone, state.TimeZone)
	state.HostID = optionalString(t.HostID)
	state.Status = types.StringValue(t.Status)
	state.MonitoringStatus = types.StringValue(t.MonitoringStatus)
	state.PingKey = types.StringValue(t.PingKey)
	state.PingURL = types.StringValue(t.PingURL)
	state.ExpectedPingAt = optionalString(t.ExpectedPingAt)
	state.LastPingAt = optionalString(t.LastPingAt)
	state.CreatedAt = types.StringValue(t.CreatedAt)
	state.UpdatedAt = types.StringValue(t.UpdatedAt)
}
