package datasources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSourceWithValidateConfig = &tasksDataSource{}

type tasksDataSource struct {
	client *client.Client
}

type tasksModel struct {
	Status       types.String `tfsdk:"status"`
	ScheduleType types.String `tfsdk:"schedule_type"`
	Query        types.String `tfsdk:"query"`
	UpdatedSince types.String `tfsdk:"updated_since"`
	Order        types.String `tfsdk:"order"`
	Direction    types.String `tfsdk:"direction"`
	Limit        types.Int64  `tfsdk:"limit"`

	Tasks []taskEntryModel `tfsdk:"tasks"`
}

type taskEntryModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	ScheduleType       types.String `tfsdk:"schedule_type"`
	Schedule           types.String `tfsdk:"schedule"`
	IntervalSeconds    types.Int64  `tfsdk:"interval_seconds"`
	TimeZone           types.String `tfsdk:"time_zone"`
	GracePeriodMinutes types.Int64  `tfsdk:"grace_period_minutes"`
	Status             types.String `tfsdk:"status"`
	Paused             types.Bool   `tfsdk:"paused"`
	MonitoringStatus   types.String `tfsdk:"monitoring_status"`
	PingKey            types.String `tfsdk:"ping_key"`
	PingURL            types.String `tfsdk:"ping_url"`
	HostID             types.String `tfsdk:"host_id"`
	ExpectedPingAt     types.String `tfsdk:"expected_ping_at"`
	LastPingAt         types.String `tfsdk:"last_ping_at"`
	CreatedAt          types.String `tfsdk:"created_at"`
	UpdatedAt          types.String `tfsdk:"updated_at"`
}

func NewTasksDataSource() datasource.DataSource {
	return &tasksDataSource{}
}

func (d *tasksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tasks"
}

func (d *tasksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the cron and heartbeat tasks in the organization. All filters are " +
			"optional and are combined; omitting them returns every task.\n\n" +
			"Each entry carries `ping_key` and `ping_url`, which are secrets — see the " +
			"per-attribute notes below.",
		Attributes: map[string]schema.Attribute{
			"status": schema.StringAttribute{
				Description: "Only tasks in this status: `active` or `paused`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("active", "paused"),
				},
			},
			"schedule_type": schema.StringAttribute{
				Description: "Only tasks of this schedule type: `cron` or `interval`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("cron", "interval"),
				},
			},
			"query": schema.StringAttribute{
				Description: "Case-insensitive substring match on the task NAME (the API's `q` filter). " +
					"The cron expression and the associated host are not searched.",
				Optional: true,
			},
			"updated_since": schema.StringAttribute{
				Description: "Only tasks whose `updated_at` is at or after this ISO 8601 timestamp " +
					"(inclusive). Pass back the newest `updated_at` you received to poll incrementally; " +
					"a task updated in the same instant as your cursor is returned again rather than skipped. " +
					"That inclusivity is why it cannot be combined with `limit` — see there.\n\n" +
					"It tracks the row's own `updated_at` and nothing else. A task going `late` is derived at " +
					"read time and writes no column, deletions leave no tombstone, and association-only edits " +
					"do not move it — so an incremental poll must not be the only source for health or for " +
					"removals. Read the unfiltered list for those.",
				Optional: true,
			},
			"order": schema.StringAttribute{
				Description: "Sort column: `created_at`, `updated_at` or `name`. Defaults to `created_at`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("created_at", "updated_at", "name"),
				},
			},
			"direction": schema.StringAttribute{
				Description: "Sort direction, `asc` or `desc`. Defaults to `desc`.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.OneOf("asc", "desc"),
				},
			},
			"limit": schema.Int64Attribute{
				Description: "Stop after this many tasks. Unset walks the whole index, which costs one " +
					"request per 100 tasks — the API throttles per IP, so an unbounded read of a large " +
					"organization can spend the rate budget the rest of the run needs. Pair it with " +
					"`order` and `direction`: without a sort, the API's own default decides which tasks " +
					"the cap keeps. It cannot be combined with `updated_since`: that cursor is " +
					"inclusive, so a capped incremental poll can stop advancing and re-read the " +
					"same tasks forever. The provider rejects the pair rather than letting a sync " +
					"stall silently.",
				Optional: true,
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
					int64validator.AtMost(maxListLimit),
				},
			},
			"tasks": schema.ListNestedAttribute{
				Description: "Matching tasks.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Description: "Unique identifier (UUID).",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Task name.",
							Computed:    true,
						},
						"schedule_type": schema.StringAttribute{
							Description: "Schedule type: `cron` or `interval`.",
							Computed:    true,
						},
						"schedule": schema.StringAttribute{
							Description: "Cron expression, for cron tasks. Null on an interval task that has " +
								"never carried one — but non-null on one that used to be a cron task, since the " +
								"API keeps the counterpart across a `schedule_type` switch.",
							Computed: true,
						},
						"interval_seconds": schema.Int64Attribute{
							Description: "Expected ping interval in seconds, for interval tasks. Non-null on a " +
								"cron task that used to be an interval one: the API keeps the counterpart across " +
								"a `schedule_type` switch rather than clearing it. Classify on `schedule_type`, " +
								"not on which of the two is set.",
							Computed: true,
						},
						"time_zone": schema.StringAttribute{
							Description: "Time zone the cron expression is evaluated in.",
							Computed:    true,
						},
						"grace_period_minutes": schema.Int64Attribute{
							Description: "Grace period in minutes before a missed ping raises an incident.",
							Computed:    true,
						},
						"status": schema.StringAttribute{
							Description: "Lifecycle status: `active` or `paused`.",
							Computed:    true,
						},
						"paused": schema.BoolAttribute{
							Description: "Whether the task is paused, i.e. `status` is `paused`. The same " +
								"value `fivenines_task.paused` writes.",
							Computed: true,
						},
						"monitoring_status": schema.StringAttribute{
							Description: "Derived health: `ok`, `late`, `waiting` (no ping has ever arrived, so " +
								"the task is not late — it has never started) or `paused` (mirrors `status`). " +
								"Distinct from `status`, which is the lifecycle state you set. Computed at read " +
								"time, so it moves without the task row being written — see `updated_since`.",
							Computed: true,
						},
						"ping_key": schema.StringAttribute{
							Description: "Ping key for sending heartbeats. Reading this data source copies " +
								"the key of every matching task into Terraform state — treat the state file " +
								"as a secret.",
							Computed:  true,
							Sensitive: true,
						},
						"ping_url": schema.StringAttribute{
							Description: "URL to send heartbeat pings to. Embeds `ping_key`, so it carries " +
								"the same secret and the same state-file caveat.",
							Computed:  true,
							Sensitive: true,
						},
						"host_id": schema.StringAttribute{
							Description: "Associated host ID (UUID), or null when the task is not tied to a host.",
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
				},
			},
		},
	}
}

func (d *tasksDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureClient(req, resp)
}

func (d *tasksDataSource) ValidateConfig(ctx context.Context, req datasource.ValidateConfigRequest, resp *datasource.ValidateConfigResponse) {
	var config tasksModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(validateTasksConfig(config)...)
}

// validateTasksConfig refuses the argument combinations that lose data.
//
// It runs at plan time AND again in Read, because a `limit` or `updated_since`
// wired from another resource's output is UNKNOWN while planning: ValidateConfig
// sees nothing to check, Terraform resolves the value later, and Read is called
// without the framework re-running config validation. Checking only once would
// let exactly the dangerous configs through — the ones a practitioner computed
// rather than typed.
func validateTasksConfig(config tasksModel) diag.Diagnostics {
	var diags diag.Diagnostics
	diags.Append(validateTasksLimit(config)...)
	diags.Append(validateTasksCursor(config)...)
	return diags
}

// validateTasksLimit re-checks at runtime what the schema validator checks at
// plan time. filterLimit has to render an invalid limit as SOMETHING, and both
// available renderings are wrong: 0 means unbounded to the walkers, which would
// silently read the whole index — and copy every task's ping key into state —
// for a practitioner who asked for a cap.
func validateTasksLimit(config tasksModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if config.Limit.IsNull() || config.Limit.IsUnknown() {
		return diags
	}

	switch n := config.Limit.ValueInt64(); {
	case n < 1:
		diags.AddAttributeError(
			path.Root("limit"),
			"Invalid limit",
			fmt.Sprintf("%q must be at least 1, got %d. A non-positive limit is not "+
				"\"no cap\" — omit the argument for that.", "limit", n),
		)
	case n > maxListLimit:
		diags.AddAttributeError(
			path.Root("limit"),
			"Invalid limit",
			fmt.Sprintf("%q must be at most %d, got %d.", "limit", maxListLimit, n),
		)
	}
	return diags
}

// validateTasksCursor refuses to combine `limit` with `updated_since` at all.
//
// The obvious reading is that sorting `updated_at asc` makes the pair safe: the
// cap would take the oldest updates first, so the cursor only advances over rows
// the caller actually received. It does not, because `updated_since` is
// INCLUSIVE — the API returns `updated_at >= updated_since` so that a row written
// in the same instant as the cursor is re-delivered rather than skipped.
//
// Combine the two and the poll can stop advancing entirely. With `limit = 1` it
// always does: the read returns one row at T, the caller feeds T back, and the
// same row comes back forever. With a larger limit it happens whenever `limit` or
// more rows share the boundary timestamp — a bulk edit, a migration, a scripted
// rollout. The loop makes progress right up until the moment it matters.
//
// A correct bounded cursor needs an EXCLUSIVE `(updated_at, id)` tie-break, which
// the API does not offer. Until it does, this pairing has no safe form, so the
// provider refuses it rather than documenting a caveat nobody will read.
func validateTasksCursor(config tasksModel) diag.Diagnostics {
	var diags diag.Diagnostics

	// Unknown values only resolve at apply time; Read re-runs this check then.
	if config.Limit.IsNull() || config.Limit.IsUnknown() ||
		config.UpdatedSince.IsNull() || config.UpdatedSince.IsUnknown() {
		return diags
	}
	// A blank cursor is not a cursor: filterString drops it and query() omits the
	// key, so nothing reaches the API and the read is an ordinary bounded
	// snapshot. `updated_since = var.cursor` with an unset variable is the shape
	// that hits this, and failing it would be a plan error for a config that is
	// actually fine.
	if config.UpdatedSince.ValueString() == "" {
		return diags
	}

	diags.AddAttributeError(
		path.Root("limit"),
		"Unsafe cursor pagination",
		`"limit" cannot be combined with "updated_since".`+"\n\n"+
			`"updated_since" is inclusive — the API returns updated_at >= the cursor, so a `+
			`task written in the same instant as your cursor comes back rather than being `+
			`skipped. Cap that result and the poll can stop advancing: with limit = 1 the `+
			`same task is returned forever, and with any limit it stalls as soon as that `+
			`many tasks share the boundary timestamp (a bulk edit, a migration).`+"\n\n"+
			`Sorting by updated_at ascending does not fix it; a bounded cursor needs an `+
			`exclusive (updated_at, id) tie-break, which the API does not offer yet.`+"\n\n"+
			`Drop "limit" to poll incrementally, or drop "updated_since" to take a bounded `+
			`snapshot.`,
	)
	return diags
}

func (d *tasksDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state tasksModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Values wired from another resource's output were unknown at plan time, so
	// this is the first point at which they can be judged at all.
	resp.Diagnostics.Append(validateTasksConfig(state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tasks, err := d.client.ListTasks(ctx, client.TaskListOptions{
		Status:       filterString(state.Status),
		ScheduleType: filterString(state.ScheduleType),
		Query:        filterString(state.Query),
		UpdatedSince: filterString(state.UpdatedSince),
		Order:        filterString(state.Order),
		Direction:    filterString(state.Direction),
		Limit:        filterLimit(state.Limit),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error listing tasks", err.Error())
		return
	}

	// Non-nil even when nothing matches: a nil slice serialises as a null list, and
	// length()/for_each/toset over a null fail. Zero matches is the normal case for
	// a filtered read, so it has to come back as [].
	state.Tasks = make([]taskEntryModel, 0, len(tasks))
	for _, t := range tasks {
		state.Tasks = append(state.Tasks, taskEntryModel{
			ID:                 types.StringValue(t.ID),
			Name:               types.StringValue(t.Name),
			ScheduleType:       types.StringValue(t.ScheduleType),
			Schedule:           optionalNonEmptyString(t.Schedule),
			IntervalSeconds:    optionalInt64(t.IntervalSeconds),
			TimeZone:           optionalString(t.TimeZone),
			GracePeriodMinutes: types.Int64Value(int64(t.GracePeriodMinutes)),
			Status:             types.StringValue(t.Status),
			Paused:             types.BoolValue(t.Status == client.StatusPaused),
			MonitoringStatus:   types.StringValue(t.MonitoringStatus),
			PingKey:            types.StringValue(t.PingKey),
			PingURL:            types.StringValue(t.PingURL),
			HostID:             optionalNonEmptyString(t.HostID),
			ExpectedPingAt:     optionalString(t.ExpectedPingAt),
			LastPingAt:         optionalString(t.LastPingAt),
			CreatedAt:          types.StringValue(t.CreatedAt),
			UpdatedAt:          types.StringValue(t.UpdatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
