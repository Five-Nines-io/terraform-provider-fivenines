package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// --- parseISOTime ---

func TestParseWindowTime(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatalf("loading Europe/Paris: %v", err)
	}

	tests := []struct {
		name      string
		input     string
		wantUTC   string
		wantZoned bool
		wantOK    bool
	}{
		{"utc", "2026-09-01T22:00:00Z", "2026-09-01T22:00:00Z", true, true},
		{"offset", "2026-09-02T00:00:00+02:00", "2026-09-01T22:00:00Z", true, true},
		{"offset without colon", "2026-09-02T00:00:00+0200", "2026-09-01T22:00:00Z", true, true},
		{"fractional seconds", "2026-09-01T22:00:00.500Z", "2026-09-01T22:00:00Z", true, true},
		{"minute precision", "2026-09-01T22:00Z", "2026-09-01T22:00:00Z", true, true},
		{"space separator", "2026-09-01 22:00:00Z", "2026-09-01T22:00:00Z", true, true},
		{"bare local read in page zone", "2026-09-02T00:00:00", "2026-09-01T22:00:00Z", false, true},
		{"bare local minute precision", "2026-09-02T00:00", "2026-09-01T22:00:00Z", false, true},
		{"whitespace before the offset", "2026-09-02T00:00:00 +02:00", "2026-09-01T22:00:00Z", true, true},
		{"impossible date", "2026-02-30T00:00:00Z", "", false, false},
		{"not a timestamp", "tomorrow", "", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, zoned, ok := parseISOTime(tt.input, paris)
			if ok != tt.wantOK {
				t.Fatalf("parseISOTime(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if zoned != tt.wantZoned {
				t.Errorf("parseISOTime(%q) zoned = %v, want %v", tt.input, zoned, tt.wantZoned)
			}
			if utc := got.UTC().Format(time.RFC3339); utc != tt.wantUTC {
				t.Errorf("parseISOTime(%q) = %s, want %s", tt.input, utc, tt.wantUTC)
			}
		})
	}
}

// --- preserveTimestamp ---

func TestPreserveTimestamp_SameInstantKeepsConfiguredFormat(t *testing.T) {
	// The API normalizes into the page timezone; the practitioner wrote UTC.
	got := preserveTimestamp(types.StringValue("2026-09-01T22:00:00Z"), "2026-09-02T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-01T22:00:00Z" {
		t.Errorf("expected the configured value to be kept, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_BareLocalResolvedInPageZone(t *testing.T) {
	got := preserveTimestamp(types.StringValue("2026-09-02T00:00:00"), "2026-09-02T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-02T00:00:00" {
		t.Errorf("expected the configured value to be kept, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_DifferentInstantTakesAPIValue(t *testing.T) {
	got := preserveTimestamp(types.StringValue("2026-09-01T22:00:00Z"), "2026-09-05T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-05T00:00:00+02:00" {
		t.Errorf("expected drift to surface the API value, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_NullPriorTakesAPIValue(t *testing.T) {
	got := preserveTimestamp(types.StringNull(), "2026-09-02T00:00:00+02:00", "Europe/Paris")
	if got.ValueString() != "2026-09-02T00:00:00+02:00" {
		t.Errorf("expected the API value, got %q", got.ValueString())
	}
}

func TestPreserveTimestamp_UnparseableFallsBackToAPIValue(t *testing.T) {
	got := preserveTimestamp(types.StringValue("whenever"), "2026-09-02T00:00:00+02:00", "")
	if got.ValueString() != "2026-09-02T00:00:00+02:00" {
		t.Errorf("expected the API value, got %q", got.ValueString())
	}
}

// --- affectedItemsToState / planAffectedItemsToClient ---

func maintenanceWindowItemList(t *testing.T, items ...client.MaintenanceWindowAffectedItem) types.List {
	t.Helper()
	objType := types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}
	values := make([]attr.Value, len(items))
	for i, item := range items {
		values[i] = types.ObjectValueMust(maintenanceWindowItemAttrTypes, map[string]attr.Value{
			"item_type": types.StringValue(item.ItemType),
			"item_id":   types.StringValue(item.ItemID),
		})
	}
	return types.ListValueMust(objType, values)
}

func TestAffectedItemsToState_Populated(t *testing.T) {
	got := affectedItemsToState(
		[]client.MaintenanceWindowAffectedItem{{ItemType: "Host", ItemID: "host-uuid"}},
		types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}),
	)
	if len(got.Elements()) != 1 {
		t.Fatalf("expected 1 element, got %d", len(got.Elements()))
	}
	attrs := got.Elements()[0].(types.Object).Attributes()
	if attrs["item_type"].(types.String).ValueString() != "Host" {
		t.Errorf("unexpected item_type: %v", attrs["item_type"])
	}
	if attrs["item_id"].(types.String).ValueString() != "host-uuid" {
		t.Errorf("unexpected item_id: %v", attrs["item_id"])
	}
}

func TestAffectedItemsToState_EmptyKeepsNullVersusEmptyChoice(t *testing.T) {
	nullList := types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes})
	if got := affectedItemsToState(nil, nullList); !got.IsNull() {
		t.Error("expected an unconfigured list to stay null")
	}

	emptyList := maintenanceWindowItemList(t)
	got := affectedItemsToState(nil, emptyList)
	if got.IsNull() || len(got.Elements()) != 0 {
		t.Errorf("expected an explicitly empty list to stay empty, got %v", got)
	}
}

func TestAffectedItemsToState_ClearedOutOfBandSurfacesAsDrift(t *testing.T) {
	prior := maintenanceWindowItemList(t, client.MaintenanceWindowAffectedItem{ItemType: "Host", ItemID: "host-uuid"})
	if got := affectedItemsToState(nil, prior); !got.IsNull() {
		t.Errorf("expected null so the removal shows up in the plan, got %v", got)
	}
}

func TestPlanAffectedItemsToClient(t *testing.T) {
	nullList := types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes})
	// An unconfigured list must send [] so removing the block clears the items.
	if got := planAffectedItemsToClient(nullList); got == nil || len(got) != 0 {
		t.Errorf("expected an empty non-nil slice, got %#v", got)
	}

	list := maintenanceWindowItemList(t,
		client.MaintenanceWindowAffectedItem{ItemType: "UptimeMonitor", ItemID: "monitor-uuid"},
		client.MaintenanceWindowAffectedItem{ItemType: "Task", ItemID: "task-uuid"},
	)
	got := planAffectedItemsToClient(list)
	if len(got) != 2 || got[0].ItemType != "UptimeMonitor" || got[1].ItemID != "task-uuid" {
		t.Errorf("unexpected conversion: %#v", got)
	}
}

// --- parseCompositeInt64ID, via the maintenance window import form ---

func TestParseMaintenanceWindowImportID(t *testing.T) {
	statusPageID, id, err := parseCompositeInt64ID("12:77", "status_page_id", "id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusPageID != 12 || id != 77 {
		t.Errorf("expected 12:77, got %d:%d", statusPageID, id)
	}

	for _, bad := range []string{"77", "12:77:3", "", ":77", "12:", "abc:77", "12:abc"} {
		if _, _, err := parseCompositeInt64ID(bad, "status_page_id", "id"); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}

// --- schema & ValidateConfig ---

func TestStatusPageMaintenanceWindowResource_SchemaImplementation(t *testing.T) {
	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %+v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema implementation diagnostics: %+v", diags)
	}
}

func statusPageMaintenanceWindowConfig(t *testing.T, startsAt, endsAt string) tfsdk.Config {
	t.Helper()

	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %+v", resp.Diagnostics)
	}

	// Start from an all-null object so the helper keeps working as attributes
	// are added, then fill in only what the validation looks at.
	objType := resp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	values["status_page_id"] = tftypes.NewValue(tftypes.Number, int64(12))
	values["title"] = tftypes.NewValue(tftypes.String, "Database upgrade")
	values["starts_at"] = tftypes.NewValue(tftypes.String, startsAt)
	values["ends_at"] = tftypes.NewValue(tftypes.String, endsAt)

	return tfsdk.Config{Schema: resp.Schema, Raw: tftypes.NewValue(objType, values)}
}

func TestStatusPageMaintenanceWindowResource_ValidateConfig(t *testing.T) {
	tests := []struct {
		name             string
		startsAt, endsAt string
		wantError        bool
	}{
		{"ordered utc", "2026-09-01T22:00:00Z", "2026-09-02T00:00:00Z", false},
		{"ordered bare local", "2026-09-02T00:00:00", "2026-09-02T02:00:00", false},
		{"inverted utc", "2026-09-02T00:00:00Z", "2026-09-01T22:00:00Z", true},
		{"inverted bare local", "2026-09-02T02:00:00", "2026-09-02T00:00:00", true},
		{"equal", "2026-09-01T22:00:00Z", "2026-09-01T22:00:00Z", true},
		// The page timezone decides the ordering, so mixed forms are left to the API.
		{"mixed offset information", "2026-09-02T00:00:00", "2026-09-01T22:00:00Z", false},
		// An offset that actually resolves the apparent inversion.
		{"inverted wall clock, ordered instants", "2026-09-02T00:00:00+02:00", "2026-09-01T23:00:00Z", false},
		// A bare date parses since the layouts moved to mapping.go and gained the
		// date-only form for api_token expiries. The window API's own format
		// requires a time, so these are values it will refuse anyway — but the
		// ordering check now sees them, and that is a decision this table records
		// rather than a side effect nobody pinned.
		{"date only, ordered", "2026-09-01", "2026-09-02", false},
		{"date only, inverted", "2026-09-02", "2026-09-01", true},
		{"date only against a local time, inverted", "2026-09-02T10:00:00", "2026-09-02", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewStatusPageMaintenanceWindowResource().(interface {
				ValidateConfig(context.Context, fwresource.ValidateConfigRequest, *fwresource.ValidateConfigResponse)
			})
			resp := &fwresource.ValidateConfigResponse{}
			r.ValidateConfig(context.Background(), fwresource.ValidateConfigRequest{
				Config: statusPageMaintenanceWindowConfig(t, tt.startsAt, tt.endsAt),
			}, resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("HasError() = %v, want %v (diagnostics: %+v)", got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}

func TestStatusPageMaintenanceWindowResource_ValidateConfig_NullConfig(t *testing.T) {
	ctx := context.Background()
	schemaResp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, schemaResp)

	r := NewStatusPageMaintenanceWindowResource().(interface {
		ValidateConfig(context.Context, fwresource.ValidateConfigRequest, *fwresource.ValidateConfigResponse)
	})
	resp := &fwresource.ValidateConfigResponse{}
	r.ValidateConfig(ctx, fwresource.ValidateConfigRequest{
		Config: tfsdk.Config{
			Schema: schemaResp.Schema,
			Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
		},
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("a null config must not error: %+v", resp.Diagnostics)
	}
}

// --- mapMaintenanceWindowToPlan: the apply-consistency grid ---
//
// Terraform fails an apply outright when the state a provider returns differs
// from what it planned, so every cell here is a potential crash rather than a
// cosmetic diff. The dimension that matters is what the SERVER echo does to a
// value the plan already decided: reorder it, normalise it, or drop it.

func windowWith(mutate func(*client.StatusPageMaintenanceWindow)) *client.StatusPageMaintenanceWindow {
	w := &client.StatusPageMaintenanceWindow{
		ID: 77, StatusPageID: 12,
		Title:     "Database upgrade",
		StartsAt:  "2026-09-02T00:00:00+02:00",
		EndsAt:    "2026-09-02T02:00:00+02:00",
		TimeZone:  "Europe/Paris",
		Status:    "scheduled",
		State:     "scheduled",
		CreatedAt: "2026-09-01T00:00:00Z",
		UpdatedAt: "2026-09-01T00:00:00Z",
	}
	if mutate != nil {
		mutate(w)
	}
	return w
}

func itemsOf(t *testing.T, list types.List) []string {
	t.Helper()
	if list.IsNull() {
		return nil
	}
	ids := make([]string, 0, len(list.Elements()))
	for _, elem := range list.Elements() {
		ids = append(ids, elem.(types.Object).Attributes()["item_id"].(types.String).ValueString())
	}
	return ids
}

func TestMapMaintenanceWindowToPlan_AffectedItemsGrid(t *testing.T) {
	hostA := client.MaintenanceWindowAffectedItem{ItemType: "Host", ItemID: "aaa"}
	hostB := client.MaintenanceWindowAffectedItem{ItemType: "Host", ItemID: "bbb"}
	nullList := types.ListNull(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes})

	tests := []struct {
		name     string
		planned  types.List
		server   []client.MaintenanceWindowAffectedItem
		wantNull bool
		wantIDs  []string
	}{
		// The plan asked for nothing; the request sent [] and the server cleared.
		{"null plan, empty echo", nullList, nil, true, nil},
		// A pinned empty list stays an empty list, never collapses to null.
		{"empty plan, empty echo", maintenanceWindowItemList(t), nil, false, []string{}},
		{"populated plan, matching echo", maintenanceWindowItemList(t, hostA, hostB),
			[]client.MaintenanceWindowAffectedItem{hostA, hostB}, false, []string{"aaa", "bbb"}},
		// The cell that crashes an apply. A Rails has_many with no explicit order
		// returns storage order, not insertion order; taking the echo verbatim
		// would hand Terraform a list whose order it did not plan.
		{"populated plan, echo reordered", maintenanceWindowItemList(t, hostA, hostB),
			[]client.MaintenanceWindowAffectedItem{hostB, hostA}, false, []string{"aaa", "bbb"}},
		// Same failure mode by length: the plan's length is what Terraform checks.
		{"populated plan, echo dropped one", maintenanceWindowItemList(t, hostA, hostB),
			[]client.MaintenanceWindowAffectedItem{hostA}, false, []string{"aaa", "bbb"}},
		{"populated plan, echo added one", maintenanceWindowItemList(t, hostA),
			[]client.MaintenanceWindowAffectedItem{hostA, hostB}, false, []string{"aaa"}},
		// Only an unknown plan has nothing to assert, so the server fills it.
		{"unknown plan takes the echo", types.ListUnknown(types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}),
			[]client.MaintenanceWindowAffectedItem{hostB}, false, []string{"bbb"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &statusPageMaintenanceWindowModel{
				StatusPageID:  types.Int64Value(12),
				Title:         types.StringValue("Database upgrade"),
				Body:          types.StringNull(),
				StartsAt:      types.StringValue("2026-09-02T00:00:00+02:00"),
				EndsAt:        types.StringValue("2026-09-02T02:00:00+02:00"),
				AffectedItems: tt.planned,
			}
			mapMaintenanceWindowToPlan(windowWith(func(w *client.StatusPageMaintenanceWindow) {
				w.AffectedItems = tt.server
			}), plan)

			if plan.AffectedItems.IsUnknown() {
				t.Fatal("state must never stay unknown after an apply")
			}
			if got := plan.AffectedItems.IsNull(); got != tt.wantNull {
				t.Fatalf("IsNull() = %v, want %v", got, tt.wantNull)
			}
			if tt.wantNull {
				return
			}
			got := itemsOf(t, plan.AffectedItems)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("item_ids = %v, want %v", got, tt.wantIDs)
			}
			for i := range got {
				if got[i] != tt.wantIDs[i] {
					t.Fatalf("item_ids = %v, want %v", got, tt.wantIDs)
				}
			}
		})
	}
}

func TestMapMaintenanceWindowToPlan_NormalisedScalarsKeepThePlannedValue(t *testing.T) {
	// Every one of these is a server normalisation that is plausible for a Rails
	// API and fatal to an apply if the echo is taken verbatim.
	tests := []struct {
		name          string
		planned, echo string
		field         func(*statusPageMaintenanceWindowModel) *types.String
		apply         func(*client.StatusPageMaintenanceWindow, string)
	}{
		{"title stripped of surrounding space", "  Database upgrade  ", "Database upgrade",
			func(m *statusPageMaintenanceWindowModel) *types.String { return &m.Title },
			func(w *client.StatusPageMaintenanceWindow, v string) { w.Title = v }},
		{"starts_at rewritten into the page timezone", "2026-09-01T22:00:00Z", "2026-09-02T00:00:00+02:00",
			func(m *statusPageMaintenanceWindowModel) *types.String { return &m.StartsAt },
			func(w *client.StatusPageMaintenanceWindow, v string) { w.StartsAt = v }},
		{"ends_at moved to a different instant", "2026-09-02T02:00:00+02:00", "2026-09-09T02:00:00+02:00",
			func(m *statusPageMaintenanceWindowModel) *types.String { return &m.EndsAt },
			func(w *client.StatusPageMaintenanceWindow, v string) { w.EndsAt = v }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &statusPageMaintenanceWindowModel{
				StatusPageID: types.Int64Value(12),
				Title:        types.StringValue("Database upgrade"),
				Body:         types.StringNull(),
				StartsAt:     types.StringValue("2026-09-02T00:00:00+02:00"),
				EndsAt:       types.StringValue("2026-09-02T02:00:00+02:00"),
				AffectedItems: types.ListNull(
					types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}),
			}
			*tt.field(plan) = types.StringValue(tt.planned)

			mapMaintenanceWindowToPlan(windowWith(func(w *client.StatusPageMaintenanceWindow) {
				tt.apply(w, tt.echo)
			}), plan)

			if got := tt.field(plan).ValueString(); got != tt.planned {
				t.Errorf("kept %q, want the planned %q — Terraform rejects the apply otherwise", got, tt.planned)
			}
		})
	}
}

func TestMapMaintenanceWindowToPlan_BodyGrid(t *testing.T) {
	text := "We are upgrading the cluster.\n"
	trimmed := "We are upgrading the cluster."
	blank := ""

	tests := []struct {
		name     string
		planned  types.String
		echo     *string
		wantNull bool
		want     string
	}{
		// A markdown body that lost its trailing newline server-side.
		{"echo normalised", types.StringValue(text), &trimmed, false, text},
		{"echo matches", types.StringValue(text), &text, false, text},
		// The plan asked for the body to be cleared and the request said so with
		// an explicit null, so null is a decision, not an absence.
		{"null plan, stale echo", types.StringNull(), &text, true, ""},
		{"null plan, null echo", types.StringNull(), nil, true, ""},
		// "" and null are interchangeable to the API; the written form survives.
		{"blank plan, null echo", types.StringValue(blank), nil, false, ""},
		{"unknown plan takes the echo", types.StringUnknown(), &text, false, text},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &statusPageMaintenanceWindowModel{
				StatusPageID: types.Int64Value(12),
				Title:        types.StringValue("Database upgrade"),
				Body:         tt.planned,
				StartsAt:     types.StringValue("2026-09-02T00:00:00+02:00"),
				EndsAt:       types.StringValue("2026-09-02T02:00:00+02:00"),
				AffectedItems: types.ListNull(
					types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}),
			}
			mapMaintenanceWindowToPlan(windowWith(func(w *client.StatusPageMaintenanceWindow) {
				w.Body = tt.echo
			}), plan)

			if plan.Body.IsUnknown() {
				t.Fatal("state must never stay unknown after an apply")
			}
			if got := plan.Body.IsNull(); got != tt.wantNull {
				t.Fatalf("IsNull() = %v, want %v", got, tt.wantNull)
			}
			if !tt.wantNull && plan.Body.ValueString() != tt.want {
				t.Errorf("body = %q, want %q", plan.Body.ValueString(), tt.want)
			}
		})
	}
}

// The plan-path merge degenerates to "keep the planned list" only because no
// child of affected_items can be unknown at apply time. Optional+Computed
// children are what forced mapStatusPageToPlan into per-value merging; if one
// ever lands here, this fails and points at that fix rather than letting the
// list silently stop reflecting the server.
func TestAffectedItemsChildrenAreAllRequired(t *testing.T) {
	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, resp)

	nested, ok := resp.Schema.Attributes["affected_items"].(schema.ListNestedAttribute)
	if !ok {
		t.Fatalf("affected_items is %T, not a ListNestedAttribute", resp.Schema.Attributes["affected_items"])
	}
	for name, attribute := range nested.NestedObject.Attributes {
		if !attribute.IsRequired() {
			t.Errorf("affected_items.%s is no longer Required. mapMaintenanceWindowToPlan keeps the "+
				"planned list verbatim, which assumes no child can be unknown at apply time — port "+
				"mergePlannedItems/resolvePlanned from the status page resource instead.", name)
		}
	}
}

// --- mapMaintenanceWindowToState: the server is the truth ---

func TestMapMaintenanceWindowToState_ReportsDrift(t *testing.T) {
	hostA := client.MaintenanceWindowAffectedItem{ItemType: "Host", ItemID: "aaa"}
	newBody := "Rescheduled."

	state := &statusPageMaintenanceWindowModel{
		StatusPageID:  types.Int64Value(12),
		Title:         types.StringValue("Database upgrade"),
		Body:          types.StringValue("Original."),
		StartsAt:      types.StringValue("2026-09-01T22:00:00Z"),
		EndsAt:        types.StringValue("2026-09-02T00:00:00Z"),
		AffectedItems: maintenanceWindowItemList(t, hostA),
	}
	mapMaintenanceWindowToState(windowWith(func(w *client.StatusPageMaintenanceWindow) {
		w.Title = "Database upgrade (rescheduled)"
		w.Body = &newBody
		w.StartsAt = "2026-09-09T00:00:00+02:00"
		w.State = "canceled"
		w.Status = "canceled"
		w.AffectedItems = nil
	}), state)

	if state.Title.ValueString() != "Database upgrade (rescheduled)" {
		t.Errorf("title = %q, want the server's", state.Title.ValueString())
	}
	if state.Body.ValueString() != newBody {
		t.Errorf("body = %q, want the server's", state.Body.ValueString())
	}
	if state.StartsAt.ValueString() != "2026-09-09T00:00:00+02:00" {
		t.Errorf("starts_at = %q, want the server's", state.StartsAt.ValueString())
	}
	if !state.AffectedItems.IsNull() {
		t.Errorf("affected_items = %v, want null so the removal shows in the plan", state.AffectedItems)
	}
	if state.State.ValueString() != "canceled" || state.Status.ValueString() != "canceled" {
		t.Errorf("status/state = %q/%q, want canceled/canceled", state.Status.ValueString(), state.State.ValueString())
	}
}

func TestMapMaintenanceWindowToState_KeepsEquivalentTimestampFormatting(t *testing.T) {
	state := &statusPageMaintenanceWindowModel{
		StartsAt: types.StringValue("2026-09-01T22:00:00Z"),
		EndsAt:   types.StringValue("2026-09-02T00:00:00Z"),
		AffectedItems: types.ListNull(
			types.ObjectType{AttrTypes: maintenanceWindowItemAttrTypes}),
	}
	mapMaintenanceWindowToState(windowWith(nil), state)

	if state.StartsAt.ValueString() != "2026-09-01T22:00:00Z" {
		t.Errorf("starts_at = %q — the same instant must not read as drift", state.StartsAt.ValueString())
	}
	if state.EndsAt.ValueString() != "2026-09-02T00:00:00Z" {
		t.Errorf("ends_at = %q — the same instant must not read as drift", state.EndsAt.ValueString())
	}
}

// --- the window changed outside Terraform ---
//
// A maintenance window is a short-lived record that people delete and cancel
// from the dashboard, so the out-of-band paths are the normal case, not the
// exotic one. These drive the real Read and Delete against a stub API because
// the branch under test is the status-code dispatch, which no mapping test
// reaches.

func maintenanceWindowStateFor(t *testing.T, cancelOnDestroy bool) tfsdk.State {
	t.Helper()

	ctx := context.Background()
	schemaResp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, schemaResp)

	objType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, attrType := range objType.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	values["id"] = tftypes.NewValue(tftypes.Number, int64(77))
	values["status_page_id"] = tftypes.NewValue(tftypes.Number, int64(12))
	values["title"] = tftypes.NewValue(tftypes.String, "Database upgrade")
	values["starts_at"] = tftypes.NewValue(tftypes.String, "2026-09-01T22:00:00Z")
	values["ends_at"] = tftypes.NewValue(tftypes.String, "2026-09-02T00:00:00Z")
	values["cancel_on_destroy"] = tftypes.NewValue(tftypes.Bool, cancelOnDestroy)

	return tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.NewValue(objType, values)}
}

func maintenanceWindowResourceFor(t *testing.T, handler http.HandlerFunc) (*statusPageMaintenanceWindowResource, *[]string) {
	t.Helper()

	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	return &statusPageMaintenanceWindowResource{client: client.NewClient(srv.URL, "test-api-key")}, &calls
}

func TestStatusPageMaintenanceWindowResource_ReadDropsAWindowDeletedOutOfBand(t *testing.T) {
	r, _ := maintenanceWindowResourceFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
	})

	resp := &fwresource.ReadResponse{State: maintenanceWindowStateFor(t, false)}
	r.Read(context.Background(), fwresource.ReadRequest{State: maintenanceWindowStateFor(t, false)}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a window deleted in the dashboard must plan as a recreate, not an error: %+v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Error("expected the resource to be removed from state")
	}
}

// The inverse, and the more dangerous direction: a transient server error must
// never be mistaken for "gone", which would silently drop a live window from
// state and orphan it.
func TestStatusPageMaintenanceWindowResource_ReadKeepsStateOnServerError(t *testing.T) {
	r, _ := maintenanceWindowResourceFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "boom"})
	})

	resp := &fwresource.ReadResponse{State: maintenanceWindowStateFor(t, false)}
	r.Read(context.Background(), fwresource.ReadRequest{State: maintenanceWindowStateFor(t, false)}, resp)

	if !resp.Diagnostics.HasError() {
		t.Error("expected a 500 to surface as an error")
	}
	if resp.State.Raw.IsNull() {
		t.Error("a 500 must not remove the resource from state")
	}
}

func TestStatusPageMaintenanceWindowResource_Destroy(t *testing.T) {
	tests := []struct {
		name            string
		cancelOnDestroy bool
		status          int
		wantCall        string
		wantError       bool
	}{
		{"deletes by default", false, http.StatusNoContent,
			"DELETE /api/v1/status_pages/12/maintenance_windows/77", false},
		{"cancels when asked, preserving page history", true, http.StatusOK,
			"PATCH /api/v1/status_pages/12/maintenance_windows/77/cancel", false},
		// Already gone is the outcome the practitioner asked for either way.
		{"tolerates a window already deleted out of band", false, http.StatusNotFound,
			"DELETE /api/v1/status_pages/12/maintenance_windows/77", false},
		{"tolerates a window already gone on the cancel path", true, http.StatusNotFound,
			"PATCH /api/v1/status_pages/12/maintenance_windows/77/cancel", false},
		{"surfaces a server error", false, http.StatusInternalServerError,
			"DELETE /api/v1/status_pages/12/maintenance_windows/77", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, calls := maintenanceWindowResourceFor(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				if tt.status >= 400 {
					json.NewEncoder(w).Encode(map[string]any{"error": "nope"})
				}
			})

			resp := &fwresource.DeleteResponse{State: maintenanceWindowStateFor(t, tt.cancelOnDestroy)}
			r.Delete(context.Background(), fwresource.DeleteRequest{
				State: maintenanceWindowStateFor(t, tt.cancelOnDestroy),
			}, resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Fatalf("HasError() = %v, want %v (%+v)", got, tt.wantError, resp.Diagnostics)
			}
			if len(*calls) != 1 || (*calls)[0] != tt.wantCall {
				t.Errorf("requests = %v, want exactly [%s]", *calls, tt.wantCall)
			}
		})
	}
}

// A window edited in the dashboard between this apply's GET and its PATCH comes
// back as a 412. Retrying re-reads the ETag first, so the second attempt carries
// the current one; without the retry the apply just fails.
func TestStatusPageMaintenanceWindowResource_UpdateRetriesOnStaleETag(t *testing.T) {
	ctx := context.Background()
	schemaResp := &fwresource.SchemaResponse{}
	NewStatusPageMaintenanceWindowResource().Schema(ctx, fwresource.SchemaRequest{}, schemaResp)

	var patches int
	var lastIfMatch string
	r, calls := maintenanceWindowResourceFor(t, func(w http.ResponseWriter, req *http.Request) {
		if req.Method == "GET" {
			// A fresh ETag on every read: the retry has to pick up the second one.
			w.Header().Set("ETag", `"etag-`+strconv.Itoa(patches)+`"`)
			json.NewEncoder(w).Encode(map[string]any{"maintenance_window": map[string]any{
				"id": 77, "status_page_id": 12, "title": "Database upgrade",
				"starts_at": "2026-09-01T22:00:00Z", "ends_at": "2026-09-02T00:00:00Z",
				"time_zone": "UTC", "status": "scheduled", "state": "scheduled",
				"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-01T00:00:00Z",
			}})
			return
		}
		patches++
		lastIfMatch = req.Header.Get("If-Match")
		if patches == 1 {
			w.WriteHeader(http.StatusPreconditionFailed)
			json.NewEncoder(w).Encode(map[string]any{"error": "stale"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"maintenance_window": map[string]any{
			"id": 77, "status_page_id": 12, "title": "Database upgrade",
			"starts_at": "2026-09-01T22:00:00Z", "ends_at": "2026-09-02T00:00:00Z",
			"time_zone": "UTC", "status": "scheduled", "state": "scheduled",
			"created_at": "2026-09-01T00:00:00Z", "updated_at": "2026-09-01T00:00:00Z",
		}})
	})

	state := maintenanceWindowStateFor(t, false)
	resp := &fwresource.UpdateResponse{State: state}
	r.Update(ctx, fwresource.UpdateRequest{
		Plan:  tfsdk.Plan{Schema: schemaResp.Schema, Raw: state.Raw},
		State: state,
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a single stale ETag must be retried, not surfaced: %+v", resp.Diagnostics)
	}
	if patches != 2 {
		t.Errorf("PATCH attempts = %d, want 2", patches)
	}
	if lastIfMatch == "" {
		t.Error("the retry must still send If-Match, or it overwrites whatever changed")
	}
	// Each attempt re-reads before patching, which is what makes the retry safe.
	gets := 0
	for _, c := range *calls {
		if c[:3] == "GET" {
			gets++
		}
	}
	if gets != 2 {
		t.Errorf("GETs = %d, want 2 — the retry must refresh the ETag", gets)
	}
}

// The API rejects anything outside its documented grammar with a 422, so the
// plan-time regex has to match that grammar rather than a reading of the prose
// next to it: too strict and valid configurations stop planning.
func TestMaintenanceWindowTimestampRE_MatchesTheAPIGrammar(t *testing.T) {
	accepted := []string{
		"2026-09-01T02:00",           // minute precision, no offset
		"2026-09-01T02:00:00",        // bare local time
		"2026-09-01T02:00:00Z",       // explicit UTC
		"2026-09-01T02:00:00.123Z",   // fractional seconds
		"2026-09-01T02:00:00+02:00",  // offset with a colon
		"2026-09-01T02:00:00+0200",   // the spec's optional colon
		"2026-09-01 02:00:00Z",       // the spec's "a space may replace the T"
		"2026-09-01T02:00:00 +02:00", // the spec's \s* before the offset
		"2026-02-30T00:00:00Z",       // impossible date: the API 422s, not us
	}
	rejected := []string{
		"20260901T0200",            // basic notation
		"2026-09-01T02:00:00,123Z", // comma fraction
		"2026-09-01T02:00:00+02",   // hour-only offset
		"2026-09-01",               // date only
		"tomorrow",
		"",
	}

	for _, v := range accepted {
		if !maintenanceWindowTimestampRE.MatchString(v) {
			t.Errorf("rejected %q, which the API accepts", v)
		}
	}
	for _, v := range rejected {
		if maintenanceWindowTimestampRE.MatchString(v) {
			t.Errorf("accepted %q, which the API rejects", v)
		}
	}
}
