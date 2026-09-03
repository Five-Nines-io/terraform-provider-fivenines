package resources

import (
	"context"
	"fmt"
	"strconv"

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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var (
	_ resource.Resource                   = &statusPageResource{}
	_ resource.ResourceWithImportState    = &statusPageResource{}
	_ resource.ResourceWithValidateConfig = &statusPageResource{}
)

// maxLogoBytes is the decoded size limit the API enforces on uploaded logos.
const maxLogoBytes = 1 << 20

type statusPageResource struct {
	client *client.Client
}

type statusPageModel struct {
	ID                      types.Int64  `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Slug                    types.String `tfsdk:"slug"`
	Description             types.String `tfsdk:"description"`
	Public                  types.Bool   `tfsdk:"public"`
	Uptime                  types.Bool   `tfsdk:"uptime"`
	CustomDomain            types.String `tfsdk:"custom_domain"`
	CustomDomainEnabled     types.Bool   `tfsdk:"custom_domain_enabled"`
	CustomFooter            types.String `tfsdk:"custom_footer"`
	CustomFooterEnabled     types.Bool   `tfsdk:"custom_footer_enabled"`
	IncidentsHistoryEnabled types.Bool   `tfsdk:"incidents_history_enabled"`
	ThemeVariant            types.String `tfsdk:"theme_variant"`

	ContactURL                  types.String `tfsdk:"contact_url"`
	SubscriptionsEnabled        types.Bool   `tfsdk:"subscriptions_enabled"`
	UptimeGreenToleranceSeconds types.Int64  `tfsdk:"uptime_green_tolerance_seconds"`
	UptimeWindowDays            types.Int64  `tfsdk:"uptime_window_days"`
	SearchIndexingEnabled       types.Bool   `tfsdk:"search_indexing_enabled"`
	Logo                        types.String `tfsdk:"logo"`
	LogoURL                     types.String `tfsdk:"logo_url"`
	Sections                    types.List   `tfsdk:"sections"`

	Items     types.List   `tfsdk:"items"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

var statusPageItemAttrTypes = map[string]attr.Type{
	"item_type":     types.StringType,
	"item_id":       types.StringType,
	"display_label": types.StringType,
	"description":   types.StringType,
	"section":       types.StringType,
}

func NewStatusPageResource() resource.Resource {
	return &statusPageResource{}
}

func (r *statusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *statusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a FiveNines status page.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Description: "Unique identifier.",
				Computed:    true,
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "Name of the status page.",
				Required:    true,
			},
			"slug": schema.StringAttribute{
				Description: "URL slug (auto-generated if not provided).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"description": schema.StringAttribute{
				Description: "Description of the status page.",
				Optional:    true,
				Computed:    true,
			},
			"public": schema.BoolAttribute{
				Description: "Whether the status page is publicly accessible.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"uptime": schema.BoolAttribute{
				Description: "Whether to show uptime percentages.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"custom_domain": schema.StringAttribute{
				Description: "Custom domain for the status page.",
				Optional:    true,
				Computed:    true,
			},
			"custom_domain_enabled": schema.BoolAttribute{
				Description: "Whether custom domain is enabled (requires plan upgrade).",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"custom_footer": schema.StringAttribute{
				Description: "Custom footer HTML.",
				Optional:    true,
				Computed:    true,
			},
			"custom_footer_enabled": schema.BoolAttribute{
				Description: "Whether custom footer is enabled (requires plan upgrade).",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"incidents_history_enabled": schema.BoolAttribute{
				Description: "Whether to show incidents history.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
			},
			"theme_variant": schema.StringAttribute{
				Description: "Theme variant (system, dark, light).",
				Optional:    true,
				Computed:    true,
				Default:     stringdefault.StaticString("system"),
				Validators: []validator.String{
					stringvalidator.OneOf("system", "dark", "light"),
				},
			},
			"contact_url": schema.StringAttribute{
				Description: "URL of the contact or support page linked from the status page. Must start with http:// or https://.",
				Optional:    true,
				Computed:    true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(contactURLPattern, "must start with http:// or https://"),
				},
			},
			"subscriptions_enabled": schema.BoolAttribute{
				Description: "Whether the Subscribe button is shown. Setting this to false hides the button but keeps existing subscribers.",
				Optional:    true,
				Computed:    true,
			},
			"uptime_green_tolerance_seconds": schema.Int64Attribute{
				Description: "Downtime, in seconds, a day may accumulate while still being shown as green on the uptime bar (0-600).",
				Optional:    true,
				Computed:    true,
				Validators: []validator.Int64{
					int64validator.Between(0, 600),
				},
			},
			"uptime_window_days": schema.Int64Attribute{
				Description: "Number of days covered by the uptime bar (1, 7, 30 or 90).",
				Optional:    true,
				Computed:    true,
				Validators: []validator.Int64{
					int64validator.OneOf(1, 7, 30, 90),
				},
			},
			"search_indexing_enabled": schema.BoolAttribute{
				Description: "Whether search engines may index the page. When false, the page and its badges are served with noindex/nofollow.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"logo": schema.StringAttribute{
				Description: "Base64-encoded PNG logo, at most 1 MB once decoded. Requires a white-label plan. " +
					"The API never echoes the image back, so the configuration is its only source of truth and this " +
					"attribute is Optional-only: dropping it from a configuration that set it deletes the logo, and " +
					"managing a page without it deletes a logo uploaded from the dashboard. Read the stored image from `logo_url`.",
				Optional:  true,
				Sensitive: true,
				Validators: []validator.String{
					Base64PNG(maxLogoBytes),
				},
			},
			"logo_url": schema.StringAttribute{
				Description: "Public URL of the uploaded logo, or null when the page has no logo.",
				Computed:    true,
			},
			"sections": schema.ListAttribute{
				Description: "Section names, in display order. Items reference a section by name, so a section must be declared here before an item can be placed in it. Omit the attribute and the provider tracks whatever the API last reported, which leaves sections curated in the dashboard alone. Set it to [] to remove them all.",
				ElementType: types.StringType,
				Optional:    true,
				Computed:    true,
				Validators: []validator.List{
					listvalidator.SizeAtMost(50),
					UniqueStrings(),
				},
			},
			"items": schema.ListNestedAttribute{
				Description: "Items displayed on the status page, in order. Omit this attribute and the provider tracks whatever the API last reported, which leaves items curated in the FiveNines dashboard alone (it follows the last refresh, so `-refresh=false` can replay a stale list). Set it to [] to remove every item.",
				Optional:    true,
				// Computed so that omitting the attribute resolves to whatever the
				// API holds. As Optional-only, a null plan against a page that has
				// items fails the apply with "Provider produced inconsistent result
				// after apply" — the server echoes items the plan said were absent.
				Computed: true,
				Validators: []validator.List{
					listvalidator.SizeAtMost(500),
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
							Description: "UUID of the item.",
							Required:    true,
						},
						"display_label": schema.StringAttribute{
							Description: "Label shown instead of the item's own name. Omitting it preserves the label curated elsewhere, for example from the dashboard.",
							Optional:    true,
							Computed:    true,
							Validators: []validator.String{
								stringvalidator.LengthAtMost(80),
							},
							PlanModifiers: []planmodifier.String{
								PreserveForSameItem(),
							},
						},
						"description": schema.StringAttribute{
							Description: "Short text shown under the item. Omitting it preserves the description curated elsewhere, for example from the dashboard.",
							Optional:    true,
							Computed:    true,
							Validators: []validator.String{
								stringvalidator.LengthAtMost(160),
							},
							PlanModifiers: []planmodifier.String{
								PreserveForSameItem(),
							},
						},
						"section": schema.StringAttribute{
							Description: "Name of the section this item belongs to. Must be declared in `sections`. Omitting it preserves the section curated elsewhere, for example from the dashboard.",
							Optional:    true,
							Computed:    true,
							PlanModifiers: []planmodifier.String{
								PreserveForSameItem(),
							},
						},
					},
				},
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

func (r *statusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *statusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	input := client.CreateStatusPageInput{
		Name: plan.Name.ValueString(),
	}
	input.Description = stringPtr(plan.Description)
	if !plan.Public.IsNull() {
		v := plan.Public.ValueBool()
		input.Public = &v
	}
	if !plan.Uptime.IsNull() {
		v := plan.Uptime.ValueBool()
		input.Uptime = &v
	}
	input.CustomDomain = stringPtr(plan.CustomDomain)
	if !plan.CustomDomainEnabled.IsNull() {
		v := plan.CustomDomainEnabled.ValueBool()
		input.CustomDomainEnabled = &v
	}
	input.CustomFooter = stringPtr(plan.CustomFooter)
	if !plan.CustomFooterEnabled.IsNull() {
		v := plan.CustomFooterEnabled.ValueBool()
		input.CustomFooterEnabled = &v
	}
	if !plan.IncidentsHistoryEnabled.IsNull() {
		v := plan.IncidentsHistoryEnabled.ValueBool()
		input.IncidentsHistoryEnabled = &v
	}
	if !plan.ThemeVariant.IsNull() && !plan.ThemeVariant.IsUnknown() {
		input.ThemeVariant = plan.ThemeVariant.ValueString()
	}
	input.ContactURL = stringPtr(plan.ContactURL)
	input.SubscriptionsEnabled = boolPtr(plan.SubscriptionsEnabled)
	input.UptimeGreenToleranceSeconds = int64Ptr(plan.UptimeGreenToleranceSeconds)
	input.UptimeWindowDays = int64Ptr(plan.UptimeWindowDays)
	input.SearchIndexingEnabled = boolPtr(plan.SearchIndexingEnabled)
	input.Logo = stringPtr(plan.Logo)
	// Same shape as the update: `items = []` and `sections = []` at create have
	// to reach the API as an explicit [], which a plain slice with omitempty
	// would drop.
	input.Sections = sectionsInput(plan.Sections)
	input.Items = planItemsToUpdateInput(plan.Items)

	tflog.Debug(ctx, "Creating status page", map[string]interface{}{"name": input.Name})

	page, err := r.client.CreateStatusPage(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Error creating status page", err.Error())
		return
	}

	mapStatusPageToPlan(page, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	page, _, err := r.client.GetStatusPage(ctx, state.ID.ValueInt64())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading status page", err.Error())
		return
	}

	mapStatusPageToState(page, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *statusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan statusPageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueInt64()

	// A null plan value means "keep the server value" and the key is omitted.
	// items needs the pointer-to-slice shape for one reason: an explicit
	// `items = []` has to reach the API as [], which a plain slice with
	// omitempty cannot express.
	input := client.UpdateStatusPageInput{
		Name:                    stringPtr(plan.Name),
		Description:             stringPtr(plan.Description),
		Public:                  boolPtr(plan.Public),
		Uptime:                  boolPtr(plan.Uptime),
		CustomDomain:            stringPtr(plan.CustomDomain),
		CustomDomainEnabled:     boolPtr(plan.CustomDomainEnabled),
		CustomFooter:            stringPtr(plan.CustomFooter),
		CustomFooterEnabled:     boolPtr(plan.CustomFooterEnabled),
		IncidentsHistoryEnabled: boolPtr(plan.IncidentsHistoryEnabled),
		ThemeVariant:            stringPtr(plan.ThemeVariant),

		ContactURL:                  stringPtr(plan.ContactURL),
		SubscriptionsEnabled:        boolPtr(plan.SubscriptionsEnabled),
		UptimeGreenToleranceSeconds: int64Ptr(plan.UptimeGreenToleranceSeconds),
		UptimeWindowDays:            int64Ptr(plan.UptimeWindowDays),
		SearchIndexingEnabled:       boolPtr(plan.SearchIndexingEnabled),
		// Logo is the one field whose tag clears on nil, so this nil is load
		// bearing: it is how `logo` leaving a configuration deletes the image.
		Logo: stringPtr(plan.Logo),

		Sections: sectionsUpdate(plan.Sections, state.Sections),
		Items:    itemsUpdate(plan.Items, state.Items),
	}

	// ETag retry loop
	var page *client.StatusPage
	for attempt := 0; attempt < 3; attempt++ {
		_, etag, err := r.client.GetStatusPage(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Error reading status page for update", err.Error())
			return
		}
		page, err = r.client.UpdateStatusPage(ctx, id, etag, input)
		if err != nil {
			if client.IsPreconditionFailed(err) && attempt < 2 {
				tflog.Debug(ctx, "ETag mismatch on status page update, retrying", map[string]interface{}{"attempt": attempt + 1})
				continue
			}
			resp.Diagnostics.AddError("Error updating status page", err.Error())
			return
		}
		break
	}

	mapStatusPageToPlan(page, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *statusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state statusPageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting status page", map[string]interface{}{"id": state.ID.ValueInt64()})

	err := r.client.DeleteStatusPage(ctx, state.ID.ValueInt64())
	if err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting status page", err.Error())
	}
}

func (r *statusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid ID", fmt.Sprintf("Cannot parse %q as int64: %s", req.ID, err))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.Int64Value(id))...)
}

// mapStatusPageToPlan maps an API response onto a PLAN (Create and Update).
//
// Terraform requires the applied value to equal any KNOWN planned value, so the
// plan wins for `items` and only an unknown one takes what the server reports.
// items is the one attribute where the two can legitimately diverge: it is the
// only one the provider conditionally omits, so the server's list can move
// underneath a plan that never asked to change it — someone adding an item in
// the dashboard between the refresh and the apply, say. Letting the echo win
// there fails the apply instead of converging on the next refresh.
// mapStatusPageToPlan writes an API response onto a PLAN (Create and Update).
// A known planned value wins and only an unknown one takes the server's, because
// Terraform fails the apply outright when the final state contradicts the plan.
// The echo can contradict it for any field the provider conditionally omits:
// items and sections skip an unchanged write, and a per-item label the
// configuration does not manage is never sent at all.
//
// The rule is applied per value, not per attribute. `items` is Optional+Computed
// with Optional+Computed children, so the list itself can be known while
// individual labels inside it are unknown — restoring the planned list wholesale
// would leave those unknowns in the final state, which Terraform rejects just as
// hard as a contradiction. mergePlannedItems resolves them from the response.
//
// Read uses mapStatusPageToState directly so drift still surfaces there.
func mapStatusPageToPlan(p *client.StatusPage, plan *statusPageModel) {
	plannedItems := plan.Items
	plannedSections := plan.Sections

	mapStatusPageToState(p, plan)

	if !plannedSections.IsUnknown() {
		plan.Sections = plannedSections
	}
	if !plannedItems.IsUnknown() {
		plan.Items = mergePlannedItems(plannedItems, p.Items)
	}
}

// mergePlannedItems keeps the planned list's length and order — the server echo
// never adds or removes an item the plan did not ask for — and fills in only the
// per-item values the plan left unknown.
//
// Server items are matched by item_id, not by index. Index correlation is what
// PreserveForSameItem exists to defuse at plan time; correlating by index here
// would hand a label back to whichever item happens to share a position, which
// is the same defect one layer down.
func mergePlannedItems(planned types.List, serverItems []client.StatusPageItem) types.List {
	if planned.IsNull() {
		return planned
	}

	byID := make(map[string]client.StatusPageItem, len(serverItems))
	for _, item := range serverItems {
		byID[item.ItemID] = item
	}

	elements := planned.Elements()
	merged := make([]attr.Value, len(elements))
	for i, elem := range elements {
		obj, ok := elem.(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			// No identity to match on. Fall back to the response at this
			// position, which is the only correlation left.
			if i < len(serverItems) {
				merged[i] = statusPageItemObject(serverItems[i])
				continue
			}
			merged[i] = elem
			continue
		}

		attrs := obj.Attributes()
		id, _ := attrs["item_id"].(types.String)
		server := byID[id.ValueString()]

		values := make(map[string]attr.Value, len(statusPageItemAttrTypes))
		for name, value := range attrs {
			values[name] = value
		}
		values["display_label"] = resolvePlanned(attrs["display_label"], server.DisplayLabel)
		values["description"] = resolvePlanned(attrs["description"], server.Description)
		values["section"] = resolvePlanned(attrs["section"], server.Section)

		merged[i] = types.ObjectValueMust(statusPageItemAttrTypes, values)
	}

	return types.ListValueMust(types.ObjectType{AttrTypes: statusPageItemAttrTypes}, merged)
}

// resolvePlanned keeps a known planned value and lets only an unknown one take
// the server's.
func resolvePlanned(planned attr.Value, server *string) types.String {
	if s, ok := planned.(types.String); ok && !s.IsUnknown() {
		return s
	}
	return optionalString(server)
}

// statusPageItemObject maps one API item onto the nested object type.
func statusPageItemObject(item client.StatusPageItem) attr.Value {
	return types.ObjectValueMust(statusPageItemAttrTypes, map[string]attr.Value{
		"item_type":     types.StringValue(item.ItemType),
		"item_id":       types.StringValue(item.ItemID),
		"display_label": optionalString(item.DisplayLabel),
		"description":   optionalString(item.Description),
		"section":       optionalString(item.Section),
	})
}

// mapStatusPageToState maps an API response onto prior STATE (Read), where the
// server is the truth: reporting it is how a page edited in the dashboard shows
// up as drift.
func mapStatusPageToState(p *client.StatusPage, state *statusPageModel) {
	state.ID = types.Int64Value(p.ID)
	state.Name = types.StringValue(p.Name)
	state.Slug = types.StringValue(p.Slug)
	state.Description = optionalString(p.Description)
	state.Public = types.BoolValue(p.Public)
	state.Uptime = types.BoolValue(p.Uptime)
	state.CustomDomain = optionalString(p.CustomDomain)
	state.CustomDomainEnabled = types.BoolValue(p.CustomDomainEnabled)
	state.CustomFooter = optionalString(p.CustomFooter)
	state.CustomFooterEnabled = types.BoolValue(p.CustomFooterEnabled)
	state.IncidentsHistoryEnabled = types.BoolValue(p.IncidentsHistoryEnabled)
	state.ThemeVariant = stringOrKeep(p.ThemeVariant, state.ThemeVariant)
	state.ContactURL = optionalString(p.ContactURL)
	state.SubscriptionsEnabled = types.BoolValue(p.SubscriptionsEnabled)
	state.UptimeGreenToleranceSeconds = types.Int64Value(p.UptimeGreenToleranceSeconds)
	state.UptimeWindowDays = types.Int64Value(p.UptimeWindowDays)
	state.SearchIndexingEnabled = types.BoolValue(p.SearchIndexingEnabled)
	state.LogoURL = optionalString(p.LogoURL)
	// state.Logo is deliberately untouched. The API never returns the image, so
	// the configuration is its only source of truth and a read has nothing to
	// say about it.
	state.CreatedAt = types.StringValue(p.CreatedAt)
	state.UpdatedAt = types.StringValue(p.UpdatedAt)

	// Sections carry the same pinned-empty rule as items below.
	switch {
	case len(p.Sections) > 0:
		sections := make([]attr.Value, len(p.Sections))
		for i, name := range p.Sections {
			sections[i] = types.StringValue(name)
		}
		state.Sections = types.ListValueMust(types.StringType, sections)
	case !state.Sections.IsNull() && !state.Sections.IsUnknown() && len(state.Sections.Elements()) == 0:
	default:
		state.Sections = types.ListNull(types.StringType)
	}

	itemType := types.ObjectType{AttrTypes: statusPageItemAttrTypes}
	switch {
	case len(p.Items) > 0:
		items := make([]attr.Value, len(p.Items))
		for i, item := range p.Items {
			items[i] = statusPageItemObject(item)
		}
		state.Items = types.ListValueMust(itemType, items)
	case !state.Items.IsNull() && !state.Items.IsUnknown() && len(state.Items.Elements()) == 0:
		// Narrow on purpose: keep ONLY a pinned empty list, which is the one
		// distinction the API cannot express (it reports "no items" for both
		// `[]` and unset). Keeping a non-empty list here would mean a page whose
		// items were deleted in the dashboard never shows as drift.
	default:
		state.Items = types.ListNull(itemType)
	}
}

// planItemsToUpdateInput turns a planned item list into a create value: nil
// omits the key, and a pointer to an empty slice sends the explicit [] that
// `items = []` means. Only null and unknown omit, which at create is the
// "no items block" case.
func planItemsToUpdateInput(itemsList types.List) *[]client.StatusPageItemInput {
	if itemsList.IsNull() || itemsList.IsUnknown() {
		return nil
	}
	items := planItemsToClient(itemsList)
	return &items
}

// itemsUpdate is planItemsToUpdateInput for the update path, where a null plan
// value never actually arrives: items is Optional+Computed, so a config with no
// `items` block plans the LAST REFRESHED list. Sending that back would delete
// anything added in the dashboard since the refresh — under `-refresh=false`,
// or between a saved plan and its apply, that is silent data loss.
//
// So the key is omitted whenever the plan matches what state already holds. An
// update that does not touch items does not write items.
func itemsUpdate(planned, stored types.List) *[]client.StatusPageItemInput {
	if planned.IsNull() || planned.IsUnknown() {
		return nil
	}
	if planned.Equal(stored) {
		return nil
	}
	items := planItemsToClient(planned)
	return &items
}

// planItemsToClient converts planned items into API inputs. There is no
// position field: the API dropped it from the input and derives the display
// order from the array order. The nullable per-item values follow the update
// input's tags — a nil pointer omits the key, so a label the configuration does
// not manage keeps whatever was curated in the dashboard.
func planItemsToClient(itemsList types.List) []client.StatusPageItemInput {
	elements := itemsList.Elements()
	result := make([]client.StatusPageItemInput, len(elements))
	for i, elem := range elements {
		attrs := elem.(types.Object).Attributes()
		result[i] = client.StatusPageItemInput{
			ItemType:     attrs["item_type"].(types.String).ValueString(),
			ItemID:       attrs["item_id"].(types.String).ValueString(),
			DisplayLabel: stringPtr(attrs["display_label"].(types.String)),
			Description:  stringPtr(attrs["description"].(types.String)),
			Section:      stringPtr(attrs["section"].(types.String)),
		}
	}
	return result
}

// sectionsInput mirrors planItemsToUpdateInput for the section names: nil omits
// the key, a pointer to an empty slice sends the [] that removes every section.
func sectionsInput(sections types.List) *[]string {
	if sections.IsNull() || sections.IsUnknown() {
		return nil
	}
	elements := sections.Elements()
	result := make([]string, 0, len(elements))
	for _, elem := range elements {
		result = append(result, elem.(types.String).ValueString())
	}
	return &result
}

// sectionsUpdate is itemsUpdate for section names: an unchanged list is left
// out of the request so a section added in the dashboard survives an update
// that was not about sections.
func sectionsUpdate(planned, stored types.List) *[]string {
	if planned.IsNull() || planned.IsUnknown() || planned.Equal(stored) {
		return nil
	}
	return sectionsInput(planned)
}

// ValidateConfig reports items placed in a section the configuration does not
// declare, which the API rejects with a 422. It only runs when `sections` is set
// in the configuration: when it is omitted the page keeps whatever sections were
// curated elsewhere, and the provider cannot enumerate those.
func (r *statusPageResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config statusPageModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !isKnown(config.Sections) || !isKnown(config.Items) {
		return
	}

	declared := make(map[string]struct{}, len(config.Sections.Elements()))
	for _, elem := range config.Sections.Elements() {
		name, ok := elem.(types.String)
		if !ok || name.IsNull() {
			continue
		}
		if name.IsUnknown() {
			// A section name resolved at apply time could match anything.
			return
		}
		declared[name.ValueString()] = struct{}{}
	}

	for i, elem := range config.Items.Elements() {
		obj, ok := elem.(types.Object)
		if !ok || obj.IsNull() || obj.IsUnknown() {
			continue
		}
		section, ok := obj.Attributes()["section"].(types.String)
		if !ok || !isKnown(section) {
			continue
		}
		if _, found := declared[section.ValueString()]; !found {
			resp.Diagnostics.AddAttributeError(
				path.Root("items").AtListIndex(i).AtName("section"),
				"Undeclared Section",
				fmt.Sprintf("Section %q is not declared in `sections`. Add it there before assigning items to it.", section.ValueString()),
			)
		}
	}
}

// itemFieldGetter is the part of tfsdk.Plan and tfsdk.State that
// resetWhenItemChangedModifier needs to compare two versions of the items list.
type itemFieldGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

// resetWhenItemChangedModifier keeps "omitting the attribute preserves the
// value" from misfiring when the items list shifts. Terraform pairs list
// elements by index, so inserting or reordering an item hands the previous
// occupant's label to whichever item now sits at that index — as a known planned
// value, which the provider would then write to the API under the new item's id.
//
// When the item at this index is not the one that was there before, the planned
// value is reset to unknown. The provider then omits the field, the API keeps
// the new item's own label, and mergePlannedItems resolves the unknown from the
// response.
type resetWhenItemChangedModifier struct{}

func (m resetWhenItemChangedModifier) Description(_ context.Context) string {
	return "Preserves the value already stored for this item, unless a different item took its place in the list."
}

func (m resetWhenItemChangedModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m resetWhenItemChangedModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// An explicitly configured value wins, and on create there is no prior item
	// to carry a value over from.
	if !req.ConfigValue.IsNull() || req.State.Raw.IsNull() {
		return
	}

	index, ok := listIndexOf(req.Path)
	if !ok {
		return
	}

	prior, priorOK := itemIDAt(ctx, req.State, index)
	planned, plannedOK := itemIDAt(ctx, req.Plan, index)
	if !priorOK || !plannedOK || prior != planned {
		resp.PlanValue = types.StringUnknown()
	}
}

// PreserveForSameItem returns a plan modifier that carries an unconfigured
// per-item value over from the prior state only while the same item occupies
// that position in the list.
func PreserveForSameItem() planmodifier.String {
	return resetWhenItemChangedModifier{}
}

// listIndexOf returns the index of the list element an attribute path points
// into, for a path shaped like items[3].display_label.
func listIndexOf(p path.Path) (int, bool) {
	steps := p.Steps()
	if len(steps) < 2 {
		return 0, false
	}
	index, ok := steps[len(steps)-2].(path.PathStepElementKeyInt)
	if !ok {
		return 0, false
	}
	return int(index), true
}

// itemIDAt reads items[index].item_id, reporting false when the index is out of
// range or the identifier is not known yet — both of which mean "there is no
// prior item here to carry a value over from".
//
// It addresses the single element rather than reading the whole list: this runs
// once per nullable attribute per item on both the plan and the state, so
// decoding all of `items` here would be quadratic in the item count, and the
// list is capped at 500.
func itemIDAt(ctx context.Context, src itemFieldGetter, index int) (string, bool) {
	var id types.String
	diags := src.GetAttribute(ctx, path.Root("items").AtListIndex(index).AtName("item_id"), &id)
	if diags.HasError() || !isKnown(id) {
		return "", false
	}
	return id.ValueString(), true
}

// isKnown reports whether an attribute holds a usable value, as opposed to being
// unset or resolved only at apply time.
func isKnown(v attr.Value) bool {
	return !v.IsNull() && !v.IsUnknown()
}
