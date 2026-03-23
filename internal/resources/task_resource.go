package resources

import (
	"context"
	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &taskResource{}
	_ resource.ResourceWithImportState = &taskResource{}
)

type taskResource struct {
	client *client.Client
}

type taskModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	ScheduleType       types.String `tfsdk:"schedule_type"`
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
			"schedule": schema.StringAttribute{
				Description: "Cron expression (required when schedule_type is cron).",
				Optional:    true,
			},
			"interval_seconds": schema.Int64Attribute{
				Description: "Interval in seconds (required when schedule_type is interval).",
				Optional:    true,
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
				Description: "Ping key for sending heartbeats.",
				Computed:    true,
				Sensitive:   true,
			},
			"ping_url": schema.StringAttribute{
				Description: "URL to send heartbeat pings to.",
				Computed:    true,
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

	task, err := r.client.CreateTask(input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating task", err.Error())
		return
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

	task, _, err := r.client.GetTask(state.ID.ValueString())
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
	_, etag, err := r.client.GetTask(id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading task for update", err.Error())
		return
	}

	name := plan.Name.ValueString()
	input := client.UpdateTaskInput{
		Name: &name,
	}
	if !plan.Schedule.IsNull() && !plan.Schedule.IsUnknown() {
		v := plan.Schedule.ValueString()
		input.Schedule = &v
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
		v := plan.TimeZone.ValueString()
		input.TimeZone = &v
	}
	if !plan.HostID.IsNull() && !plan.HostID.IsUnknown() {
		v := plan.HostID.ValueString()
		input.HostID = &v
	}

	task, err := r.client.UpdateTask(id, etag, input)
	if err != nil {
		resp.Diagnostics.AddError("Error updating task", err.Error())
		return
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

	err := r.client.DeleteTask(state.ID.ValueString())
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
	state.Schedule = types.StringValue(t.Schedule)
	if t.IntervalSeconds != nil {
		state.IntervalSeconds = types.Int64Value(*t.IntervalSeconds)
	} else {
		state.IntervalSeconds = types.Int64Null()
	}
	state.GracePeriodMinutes = types.Int64Value(int64(t.GracePeriodMinutes))
	state.TimeZone = types.StringValue(t.TimeZone)
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
