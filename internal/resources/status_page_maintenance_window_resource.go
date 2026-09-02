package resources

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	// The status page timezone is resolved with time.LoadLocation to compare
	// timestamps across offsets. Embedding tzdata keeps that working on
	// platforms without a system zoneinfo database (notably Windows).
	_ "time/tzdata"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                   = &statusPageMaintenanceWindowResource{}
	_ resource.ResourceWithImportState    = &statusPageMaintenanceWindowResource{}
	_ resource.ResourceWithValidateConfig = &statusPageMaintenanceWindowResource{}
)

// maintenanceWindowTimestampRE mirrors the extended ISO 8601 form the API
// accepts: a date, a T (or space) separator, a time, and an optional UTC
// offset. Values without an offset are interpreted in the status page timezone.
var maintenanceWindowTimestampRE = regexp.MustCompile(
	`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?$`,
)

type statusPageMaintenanceWindowResource struct {
	client *client.Client
}

type statusPageMaintenanceWindowModel struct {
	ID              types.Int64  `tfsdk:"id"`
	StatusPageID    types.Int64  `tfsdk:"status_page_id"`
	Title           types.String `tfsdk:"title"`
	Body            types.String `tfsdk:"body"`
	StartsAt        types.String `tfsdk:"starts_at"`
	EndsAt          types.String `tfsdk:"ends_at"`
	AffectedItems   types.List   `tfsdk:"affected_items"`
	CancelOnDestroy types.Bool   `tfsdk:"cancel_on_destroy"`
	TimeZone        types.String `tfsdk:"time_zone"`
	Status          types.String `tfsdk:"status"`
	State           types.String `tfsdk:"state"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

var maintenanceWindowItemAttrTypes = map[string]attr.Type{
	"item_type": types.StringType,
	"item_id":   types.StringType,
}

func NewStatusPageMaintenanceWindowResource() resource.Resource {
	return &statusPageMaintenanceWindowResource{}
}

func (r *statusPageMaintenanceWindowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page_maintenance_window"
}

func (r *statusPageMaintenanceWindowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a scheduled maintenance window on a FiveNines status page. " +
			"Creating a window announces it to confirmed status page subscribers immediately.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"status_page_id": schema.Int64Attribute{
				Description: "ID of the status page the window belongs to. Changing this forces a new window.",
				Required:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"title": schema.StringAttribute{
				Description: "Title of the maintenance window (up to 120 characters).",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(120),
				},
			},
			"body": schema.StringAttribute{
				Description: "Markdown description of the maintenance (up to 5000 characters). Removing it clears the body.",
				Optional:    true,
				Validators: []validator.String{
					stringvalidator.LengthAtMost(5000),
				},
			},
			"starts_at": schema.StringAttribute{
				Description: "Start of the window, in extended ISO 8601 (for example `2026-09-01T22:00:00Z`). " +
					"A value without a UTC offset is read in the status page timezone. " +
					"Moving a window that has already started back into the future re-arms its \"maintenance started\" email.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						maintenanceWindowTimestampRE,
						"must be an extended ISO 8601 timestamp, for example 2026-09-01T22:00:00Z or 2026-09-01T22:00:00",
					),
				},
			},
			"ends_at": schema.StringAttribute{
				Description: "End of the window, in extended ISO 8601. Must be after `starts_at`.",
				Required:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						maintenanceWindowTimestampRE,
						"must be an extended ISO 8601 timestamp, for example 2026-09-02T02:00:00Z or 2026-09-02T02:00:00",
					),
				},
			},
			"affected_items": schema.ListNestedAttribute{
				Description: "Items affected by the maintenance (up to 200). Each item must already be listed on the status page. " +
					"The list is replaced wholesale on update; omitting the attribute clears it.",
				Optional: true,
				Validators: []validator.List{
					listvalidator.SizeAtMost(200),
				},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"item_type": schema.StringAttribute{
							Description: "Type of item (Host, UptimeMonitor, Task).",
							Required:    true,
							Validators: []validator.String{
								stringvalidator.OneOf("Host", "UptimeMonitor", "Task"),
							},
						},
						"item_id": schema.StringAttribute{
							Description: "UUID of the underlying host, uptime monitor or task — not the status page item ID.",
							Required:    true,
						},
					},
				},
			},
			"cancel_on_destroy": schema.BoolAttribute{
				Description: "When true, destroying the resource cancels the window and keeps it in the status page history. " +
					"When false (the default), the window is deleted permanently and disappears from the history.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"time_zone": schema.StringAttribute{
				Description: "Timezone of the status page, used to interpret timestamps without an offset.",
				Computed:    true,
			},
			"status": schema.StringAttribute{
				Description: "Stored lifecycle status (scheduled, in_progress, completed, canceled). " +
					"Advanced by a background job, so it can lag behind `state`.",
				Computed: true,
			},
			"state": schema.StringAttribute{
				Description: "Clock-derived state rendered on the public status page. Prefer this over `status`.",
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

func (r *statusPageMaintenanceWindowResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ValidateConfig reports an inverted window at plan time instead of waiting for
// the API to 422. The two timestamps are only compared when they carry the same
// offset information: resolving a bare local time needs the status page
// timezone, which is not known until apply. Comparing two bare local times
// against each other is still sound, other than for a window that both straddles
// a daylight-saving fall-back and reads backwards in wall-clock terms — hence
// the hint to disambiguate with an explicit offset.
func (r *statusPageMaintenanceWindowResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if req.Config.Raw.IsNull() {
		return
	}

	var config statusPageMaintenanceWindowModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !isKnown(config.StartsAt) || !isKnown(config.EndsAt) {
		return
	}

	start, startZoned, startOK := parseWindowTime(config.StartsAt.ValueString(), time.UTC)
	end, endZoned, endOK := parseWindowTime(config.EndsAt.ValueString(), time.UTC)
	if !startOK || !endOK || startZoned != endZoned {
		return
	}
	if !end.After(start) {
		resp.Diagnostics.AddAttributeError(
			path.Root("ends_at"),
			"Invalid maintenance window",
			fmt.Sprintf(
				"ends_at (%s) must be after starts_at (%s). "+
					"If the window crosses a daylight-saving change, write both timestamps with an explicit UTC offset.",
				config.EndsAt.ValueString(), config.StartsAt.ValueString(),
			),
		)
	}
}

func (r *statusPageMaintenanceWindowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statusPageMaintenanceWindowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateStatusPageMaintenanceWindowInput{
		Title:    plan.Title.ValueString(),
		StartsAt: plan.StartsAt.ValueString(),
		EndsAt:   plan.EndsAt.ValueString(),
	}
	if isKnown(plan.Body) {
		v := plan.Body.ValueString()
		input.Body = &v
	}
	if isKnown(plan.AffectedItems) {
		items := planAffectedItemsToClient(plan.AffectedItems)
		input.AffectedItems = &items
	}

	statusPageID := plan.StatusPageID.ValueInt64()
	tflog.Debug(ctx, "Creating status page maintenance window", map[string]interface{}{
		"status_page_id": statusPageID,
		"title":          input.Title,
	})

	window, err := r.client.CreateStatusPageMaintenanceWindow(ctx, statusPageID, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating status page maintenance window", err.Error())
		return
	}

	mapMaintenanceWindowToState(window, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageMaintenanceWindowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statusPageMaintenanceWindowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	window, _, err := r.client.GetStatusPageMaintenanceWindow(ctx, state.StatusPageID.ValueInt64(), state.ID.ValueInt64())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading status page maintenance window", err.Error())
		return
	}

	mapMaintenanceWindowToState(window, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *statusPageMaintenanceWindowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan statusPageMaintenanceWindowModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state statusPageMaintenanceWindowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	statusPageID := state.StatusPageID.ValueInt64()
	id := state.ID.ValueInt64()

	title := plan.Title.ValueString()
	startsAt := plan.StartsAt.ValueString()
	endsAt := plan.EndsAt.ValueString()
	input := client.UpdateStatusPageMaintenanceWindowInput{
		Title:    &title,
		StartsAt: &startsAt,
		EndsAt:   &endsAt,
	}
	// Body is always part of the payload: leaving it nil sends JSON null, which
	// clears the body when the practitioner drops it from the configuration.
	if isKnown(plan.Body) {
		v := plan.Body.ValueString()
		input.Body = &v
	}
	// affected_items is always sent as well. The API keeps the existing list
	// when the key is omitted, which would strand items the practitioner has
	// removed from the configuration.
	items := planAffectedItemsToClient(plan.AffectedItems)
	input.AffectedItems = &items

	var window *client.StatusPageMaintenanceWindow
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetStatusPageMaintenanceWindow(ctx, statusPageID, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading status page maintenance window for update", err.Error())
			return
		}
		window, err = r.client.UpdateStatusPageMaintenanceWindow(ctx, statusPageID, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on maintenance window update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating status page maintenance window", err.Error())
			return
		}
		break
	}

	mapMaintenanceWindowToState(window, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageMaintenanceWindowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statusPageMaintenanceWindowModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	statusPageID := state.StatusPageID.ValueInt64()
	id := state.ID.ValueInt64()

	var err error
	if state.CancelOnDestroy.ValueBool() {
		tflog.Debug(ctx, "Canceling status page maintenance window", map[string]interface{}{"status_page_id": statusPageID, "id": id})
		err = r.client.CancelStatusPageMaintenanceWindow(ctx, statusPageID, id)
	} else {
		tflog.Debug(ctx, "Deleting status page maintenance window", map[string]interface{}{"status_page_id": statusPageID, "id": id})
		err = r.client.DeleteStatusPageMaintenanceWindow(ctx, statusPageID, id)
	}
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.StatusCode == 404 {
			return
		}
		resp.Diagnostics.AddError("Error destroying status page maintenance window", err.Error())
	}
}

func (r *statusPageMaintenanceWindowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	statusPageID, id, err := parseMaintenanceWindowImportID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("status_page_id"), types.Int64Value(statusPageID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
	// Seed the provider-only attribute with its schema default so the first plan
	// after an import is clean.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("cancel_on_destroy"), types.BoolValue(false))...)
}

// parseMaintenanceWindowImportID splits the "<status_page_id>:<id>" import ID.
func parseMaintenanceWindowImportID(importID string) (int64, int64, error) {
	parts := strings.Split(importID, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, 0, fmt.Errorf("expected %q to be in the form <status_page_id>:<id>", importID)
	}
	statusPageID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse status page ID %q as an integer: %s", parts[0], err)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot parse maintenance window ID %q as an integer: %s", parts[1], err)
	}
	return statusPageID, id, nil
}

// mapMaintenanceWindowToState copies an API window onto target, which arrives
// holding the plan (create/update) or the prior state (read). That incoming
// value is used to keep practitioner-chosen representations — timestamp
// formatting, and null versus [] for affected_items — stable across a round
// trip. status_page_id is deliberately untouched: it is always known from the
// plan, prior state or import ID.
func mapMaintenanceWindowToState(w *client.StatusPageMaintenanceWindow, target *statusPageMaintenanceWindowModel) {
	priorStartsAt := target.StartsAt
	priorEndsAt := target.EndsAt
	priorBody := target.Body
	priorItems := target.AffectedItems

	target.ID = types.Int64Value(w.ID)
	target.Title = types.StringValue(w.Title)
	target.Body = preserveBlankString(w.Body, priorBody)
	target.TimeZone = types.StringValue(w.TimeZone)
	target.StartsAt = preserveTimestamp(priorStartsAt, w.StartsAt, w.TimeZone)
	target.EndsAt = preserveTimestamp(priorEndsAt, w.EndsAt, w.TimeZone)
	target.AffectedItems = affectedItemsToState(w.AffectedItems, priorItems)
	target.Status = types.StringValue(w.Status)
	target.State = types.StringValue(w.State)
	target.CreatedAt = types.StringValue(w.CreatedAt)
	target.UpdatedAt = types.StringValue(w.UpdatedAt)
}

// preserveTimestamp keeps the configured timestamp string whenever it denotes
// the same instant as the value the API returned. The API normalizes timestamps
// into the status page timezone, so a window configured as
// "2026-09-01T22:00:00Z" comes back as "2026-09-02T00:00:00+02:00" — the same
// moment, but a permanent diff if stored verbatim.
func preserveTimestamp(configured types.String, apiValue, timeZone string) types.String {
	if !isKnown(configured) {
		return types.StringValue(apiValue)
	}
	loc := time.UTC
	if timeZone != "" {
		if l, err := time.LoadLocation(timeZone); err == nil {
			loc = l
		}
	}
	want, _, wantOK := parseWindowTime(configured.ValueString(), loc)
	got, _, gotOK := parseWindowTime(apiValue, loc)
	if wantOK && gotOK && want.Equal(got) {
		return configured
	}
	return types.StringValue(apiValue)
}

// Layouts accepted by parseWindowTime, after the date/time separator has been
// normalized to "T". The zoned set carries an explicit UTC offset; the local
// set is resolved in the status page timezone.
var (
	maintenanceWindowZonedLayouts = []string{
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999Z0700",
		"2006-01-02T15:04Z07:00",
		"2006-01-02T15:04Z0700",
	}
	maintenanceWindowLocalLayouts = []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04",
	}
)

// parseWindowTime parses an extended ISO 8601 timestamp, resolving values with
// no UTC offset in loc. It reports whether the input carried its own offset,
// and whether it parsed at all.
func parseWindowTime(s string, loc *time.Location) (parsed time.Time, zoned bool, ok bool) {
	s = strings.Replace(strings.TrimSpace(s), " ", "T", 1)
	for _, layout := range maintenanceWindowZonedLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true, true
		}
	}
	for _, layout := range maintenanceWindowLocalLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, false, true
		}
	}
	return time.Time{}, false, false
}

// preserveBlankString maps a nullable API string back to state, treating null
// and "" as interchangeable so whichever of the two the practitioner wrote
// survives the round trip and does not fail the apply as an inconsistent result.
func preserveBlankString(apiValue *string, prior types.String) types.String {
	if apiValue == nil || *apiValue == "" {
		if isKnown(prior) && prior.ValueString() == "" {
			return prior
		}
		if prior.IsNull() {
			return prior
		}
		return types.StringNull()
	}
	return types.StringValue(*apiValue)
}

// affectedItemsToState converts the API list back to state. An empty API list
// is ambiguous — it means both "not configured" and "explicitly empty" — so the
// prior null-versus-[] choice is kept when it was already empty.
func affectedItemsToState(items []client.MaintenanceWindowAffectedItem, prior types.List) types.List {
	objType := types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}
	if len(items) == 0 {
		if isKnown(prior) && len(prior.Elements()) == 0 {
			return prior
		}
		return types.ListNull(objType)
	}

	values := make([]attr.Value, len(items))
	for i, item := range items {
		values[i], _ = types.ObjectValue(maintenanceWindowItemAttrTypes, map[string]attr.Value{
			"item_type": types.StringValue(item.ItemType),
			"item_id":   types.StringValue(item.ItemID),
		})
	}
	list, _ := types.ListValue(objType, values)
	return list
}

// planAffectedItemsToClient converts the planned list to the API payload. A null
// or unknown list yields an empty (non-nil) slice, which clears the list server
// side rather than leaving the previous items in place.
func planAffectedItemsToClient(itemsList types.List) []client.MaintenanceWindowAffectedItem {
	if !isKnown(itemsList) {
		return []client.MaintenanceWindowAffectedItem{}
	}
	elements := itemsList.Elements()
	result := make([]client.MaintenanceWindowAffectedItem, len(elements))
	for i, elem := range elements {
		attrs := elem.(types.Object).Attributes()
		result[i] = client.MaintenanceWindowAffectedItem{
			ItemType: attrs["item_type"].(types.String).ValueString(),
			ItemID:   attrs["item_id"].(types.String).ValueString(),
		}
	}
	return result
}

// isKnown reports whether an attribute holds a usable value.
func isKnown(v attr.Value) bool {
	return !v.IsNull() && !v.IsUnknown()
}
