package resources

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func statusPageSchema(t *testing.T) schema.Schema {
	t.Helper()
	resp := &resource.SchemaResponse{}
	NewStatusPageResource().(*statusPageResource).Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema
}

// nullState returns a state holding the schema's shape with every attribute
// null, which is the starting point for Get/Set round-trips.
func nullState(t *testing.T, s schema.Schema) tfsdk.State {
	t.Helper()
	obj, ok := s.Type().TerraformType(context.Background()).(tftypes.Object)
	if !ok {
		t.Fatal("schema type is not an object")
	}
	values := make(map[string]tftypes.Value, len(obj.AttributeTypes))
	for name, attrType := range obj.AttributeTypes {
		values[name] = tftypes.NewValue(attrType, nil)
	}
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(obj, values)}
}

// item builds one nested items element. Anything not named is null, which is
// what a configuration that does not manage that field produces.
func item(attrs map[string]attr.Value) attr.Value {
	full := map[string]attr.Value{
		"item_type":     types.StringValue("Host"),
		"item_id":       types.StringNull(),
		"display_label": types.StringNull(),
		"description":   types.StringNull(),
		"section":       types.StringNull(),
	}
	for k, v := range attrs {
		full[k] = v
	}
	return types.ObjectValueMust(statusPageItemAttrTypes, full)
}

func itemList(items ...attr.Value) types.List {
	return types.ListValueMust(types.ObjectType{AttrTypes: statusPageItemAttrTypes}, items)
}

func labelOf(t *testing.T, list types.List, index int) types.String {
	t.Helper()
	if index >= len(list.Elements()) {
		t.Fatalf("no element at index %d in %v", index, list)
	}
	return list.Elements()[index].(types.Object).Attributes()["display_label"].(types.String)
}

// --- schema ---

func TestStatusPageSchema_Implementation(t *testing.T) {
	if diags := statusPageSchema(t).ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("invalid schema implementation: %v", diags)
	}
}

// statusPageModel and the schema have to agree on every attribute name and
// type, in both directions, or the resource panics at runtime.
func TestStatusPageSchema_RoundTripsModel(t *testing.T) {
	ctx := context.Background()
	state := nullState(t, statusPageSchema(t))

	var model statusPageModel
	if diags := state.Get(ctx, &model); diags.HasError() {
		t.Fatalf("reading the model from state: %v", diags)
	}

	mapStatusPageToState(&client.StatusPage{
		ID:       7,
		Name:     "Status",
		Sections: []string{"Core"},
		Items:    []client.StatusPageItem{{ItemType: "Host", ItemID: "uuid-1"}},
	}, &model)

	if diags := state.Set(ctx, &model); diags.HasError() {
		t.Fatalf("writing the model back to state: %v", diags)
	}
}

func TestStatusPageSchema_LogoIsOptionalOnlyAndSensitive(t *testing.T) {
	attrs := statusPageSchema(t).Attributes

	logo, ok := attrs["logo"].(schema.StringAttribute)
	if !ok {
		t.Fatal("expected logo to be a string attribute")
	}
	if !logo.Sensitive {
		t.Error("expected logo to be sensitive")
	}
	// Optional-only is what makes `logo` clearable: as Optional+Computed a null
	// config would resolve to the prior state and the image could never be
	// removed. It is also why UpdateStatusPageInput.Logo carries the clearing
	// tag — see TestUpdateInputTagsMatchTheirPolicy.
	if logo.Computed {
		t.Error("expected logo not to be computed")
	}
	if !attrs["logo_url"].IsComputed() {
		t.Error("expected logo_url to be computed")
	}
}

// --- mapStatusPageToState ---

func TestMapStatusPageToState_NewFields(t *testing.T) {
	contact := "https://acme.example/support"
	logoURL := "https://cdn.example/logo.png"
	label, description, section := "API Gateway", "Public API", "Core"

	page := &client.StatusPage{
		ID:                          3,
		Name:                        "ACME Status",
		ContactURL:                  &contact,
		SubscriptionsEnabled:        true,
		UptimeGreenToleranceSeconds: 120,
		UptimeWindowDays:            90,
		SearchIndexingEnabled:       false,
		LogoURL:                     &logoURL,
		Sections:                    []string{"Core", "Edge"},
		Items: []client.StatusPageItem{
			{ItemType: "Host", ItemID: "uuid-1", Position: 0, DisplayLabel: &label, Description: &description, Section: &section},
			{ItemType: "Task", ItemID: "uuid-2", Position: 1},
		},
	}

	state := &statusPageModel{Logo: types.StringValue("configured-base64")}
	mapStatusPageToState(page, state)

	if state.ContactURL.ValueString() != contact {
		t.Errorf("expected contact_url %q, got %q", contact, state.ContactURL.ValueString())
	}
	if !state.SubscriptionsEnabled.ValueBool() {
		t.Error("expected subscriptions_enabled true")
	}
	if state.UptimeGreenToleranceSeconds.ValueInt64() != 120 {
		t.Errorf("expected tolerance 120, got %d", state.UptimeGreenToleranceSeconds.ValueInt64())
	}
	if state.UptimeWindowDays.ValueInt64() != 90 {
		t.Errorf("expected window 90, got %d", state.UptimeWindowDays.ValueInt64())
	}
	if state.SearchIndexingEnabled.ValueBool() {
		t.Error("expected search_indexing_enabled false")
	}
	if state.LogoURL.ValueString() != logoURL {
		t.Errorf("expected logo_url %q, got %q", logoURL, state.LogoURL.ValueString())
	}
	// The API never returns the image, so a read has nothing to say about it.
	if state.Logo.ValueString() != "configured-base64" {
		t.Errorf("expected logo to be left untouched, got %q", state.Logo.ValueString())
	}
	if got := len(state.Sections.Elements()); got != 2 {
		t.Fatalf("expected 2 sections, got %d", got)
	}
	if got := labelOf(t, state.Items, 0).ValueString(); got != label {
		t.Errorf("expected display_label %q, got %q", label, got)
	}
	if !labelOf(t, state.Items, 1).IsNull() {
		t.Error("expected an unset display_label to map to null")
	}
}

func TestMapStatusPageToState_NullOptionals(t *testing.T) {
	state := &statusPageModel{}
	mapStatusPageToState(&client.StatusPage{ID: 1, Name: "Bare"}, state)

	if !state.ContactURL.IsNull() {
		t.Error("expected contact_url to be null")
	}
	if !state.LogoURL.IsNull() {
		t.Error("expected logo_url to be null")
	}
}

// Sections follow the same pinned-empty rule as items: only a list the
// configuration pinned to [] survives an API that reports none, because that is
// the one distinction the API cannot express.
func TestMapStatusPageToState_SectionsPinnedEmpty(t *testing.T) {
	tests := []struct {
		name  string
		prior types.List
		api   []string
		want  string
	}{
		{"sections added out of band", types.ListNull(types.StringType), []string{"Core"}, "populated"},
		{"all sections deleted out of band", types.ListValueMust(types.StringType, []attr.Value{types.StringValue("Core")}), nil, "null"},
		{"pinned empty survives", types.ListValueMust(types.StringType, []attr.Value{}), nil, "empty"},
		{"nothing anywhere", types.ListNull(types.StringType), nil, "null"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &statusPageModel{Sections: tt.prior}
			mapStatusPageToState(&client.StatusPage{ID: 1, Name: "x", Sections: tt.api}, state)

			var got string
			switch {
			case state.Sections.IsNull():
				got = "null"
			case len(state.Sections.Elements()) == 0:
				got = "empty"
			default:
				got = "populated"
			}
			if got != tt.want {
				t.Errorf("expected %s, got %s (%v)", tt.want, got, state.Sections)
			}
		})
	}
}

// --- mapStatusPageToPlan: the nested dimension ---
//
// main's TestMapStatusPageToPlan_KnownPlanAlwaysSurvives covers the LIST as a
// whole. Optional+Computed children add a second axis that did not exist then:
// the list can be known while a label inside it is unknown. These tests cover
// that axis.

// The composition check. PreserveForSameItem deliberately introduces unknown
// per-item values at plan time; if mapStatusPageToPlan restored the planned list
// wholesale — as it did when items had no nullable children — those unknowns
// would reach the final state and Terraform would reject the apply with
// "Provider returned invalid result object after apply". Nothing may be unknown
// once the response has been folded in.
func TestMapStatusPageToPlan_NoUnknownSurvivesIntoState(t *testing.T) {
	plan := &statusPageModel{Items: itemList(
		item(map[string]attr.Value{"item_id": types.StringValue("a"), "display_label": types.StringUnknown()}),
		item(map[string]attr.Value{"item_id": types.StringValue("b"), "description": types.StringUnknown(), "section": types.StringUnknown()}),
	)}

	mapStatusPageToPlan(&client.StatusPage{ID: 1, Name: "x", Items: []client.StatusPageItem{
		{ItemType: "Host", ItemID: "a"},
		{ItemType: "Host", ItemID: "b"},
	}}, plan)

	for i, elem := range plan.Items.Elements() {
		for name, value := range elem.(types.Object).Attributes() {
			if value.IsUnknown() {
				t.Errorf("items[%d].%s is still unknown — Terraform rejects the apply", i, name)
			}
		}
	}
}

// The per-value rule, enumerated: a known planned value always wins, and only an
// unknown one takes what the API reports for that item.
func TestMapStatusPageToPlan_NestedValueGrid(t *testing.T) {
	server := "from-server"

	tests := []struct {
		name    string
		planned attr.Value
		api     []client.StatusPageItem
		want    types.String
	}{
		{
			"null planned, API has a label — plan wins, the label is not adopted",
			types.StringNull(),
			[]client.StatusPageItem{{ItemType: "Host", ItemID: "a", DisplayLabel: &server}},
			types.StringNull(),
		},
		{
			"known planned, API disagrees — plan wins",
			types.StringValue("from-config"),
			[]client.StatusPageItem{{ItemType: "Host", ItemID: "a", DisplayLabel: &server}},
			types.StringValue("from-config"),
		},
		{
			"unknown planned, API has a label — the API fills it",
			types.StringUnknown(),
			[]client.StatusPageItem{{ItemType: "Host", ItemID: "a", DisplayLabel: &server}},
			types.StringValue(server),
		},
		{
			"unknown planned, API has no label — resolves to null",
			types.StringUnknown(),
			[]client.StatusPageItem{{ItemType: "Host", ItemID: "a"}},
			types.StringNull(),
		},
		{
			"unknown planned, item absent from the response — resolves to null",
			types.StringUnknown(),
			nil,
			types.StringNull(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &statusPageModel{Items: itemList(
				item(map[string]attr.Value{"item_id": types.StringValue("a"), "display_label": tt.planned}),
			)}
			mapStatusPageToPlan(&client.StatusPage{ID: 1, Name: "x", Items: tt.api}, plan)

			if got := labelOf(t, plan.Items, 0); !got.Equal(tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

// The response is correlated by item_id, not by position. Index correlation is
// the defect PreserveForSameItem exists to stop at plan time; repeating it here
// would reintroduce it one layer down.
func TestMapStatusPageToPlan_MatchesServerItemsByID(t *testing.T) {
	alpha, beta := "Alpha", "Beta"

	// The plan puts B first; the response still lists A first.
	plan := &statusPageModel{Items: itemList(
		item(map[string]attr.Value{"item_id": types.StringValue("b"), "display_label": types.StringUnknown()}),
		item(map[string]attr.Value{"item_id": types.StringValue("a"), "display_label": types.StringUnknown()}),
	)}

	mapStatusPageToPlan(&client.StatusPage{ID: 1, Name: "x", Items: []client.StatusPageItem{
		{ItemType: "Host", ItemID: "a", DisplayLabel: &alpha},
		{ItemType: "Host", ItemID: "b", DisplayLabel: &beta},
	}}, plan)

	if got := labelOf(t, plan.Items, 0).ValueString(); got != beta {
		t.Errorf("expected item b to keep its own label %q, got %q", beta, got)
	}
	if got := labelOf(t, plan.Items, 1).ValueString(); got != alpha {
		t.Errorf("expected item a to keep its own label %q, got %q", alpha, got)
	}
}

// The #32 crash, re-run against the nested shape: an item added in the dashboard
// between refresh and apply must not extend a plan that did not ask for it.
func TestMapStatusPageToPlan_DashboardInsertDoesNotExtendThePlan(t *testing.T) {
	plan := &statusPageModel{Items: itemList(
		item(map[string]attr.Value{"item_id": types.StringValue("a")}),
	)}

	mapStatusPageToPlan(&client.StatusPage{ID: 1, Name: "x", Items: []client.StatusPageItem{
		{ItemType: "Host", ItemID: "a"},
		{ItemType: "Host", ItemID: "b"},
	}}, plan)

	if got := len(plan.Items.Elements()); got != 1 {
		t.Fatalf("expected the plan's single item to survive, got %d elements", got)
	}
}

// Sections are conditionally omitted by sectionsUpdate, so they need the same
// plan-side protection items get.
func TestMapStatusPageToPlan_SectionsFollowThePlan(t *testing.T) {
	planned := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("Core")})
	plan := &statusPageModel{Sections: planned}

	mapStatusPageToPlan(&client.StatusPage{ID: 1, Name: "x", Sections: []string{"Core", "Edge"}}, plan)
	if !plan.Sections.Equal(planned) {
		t.Errorf("expected the planned sections to survive, got %v", plan.Sections)
	}

	plan = &statusPageModel{Sections: types.ListUnknown(types.StringType)}
	mapStatusPageToPlan(&client.StatusPage{ID: 1, Name: "x", Sections: []string{"Core"}}, plan)
	if plan.Sections.IsUnknown() || len(plan.Sections.Elements()) != 1 {
		t.Errorf("expected an unknown plan to take the API value, got %v", plan.Sections)
	}
}

// --- request shape ---

func marshalItems(t *testing.T, list types.List) string {
	t.Helper()
	encoded, err := json.Marshal(planItemsToClient(list))
	if err != nil {
		t.Fatalf("marshalling items: %v", err)
	}
	return string(encoded)
}

func TestPlanItemsToClient_OmitsPosition(t *testing.T) {
	got := marshalItems(t, itemList(item(map[string]attr.Value{"item_id": types.StringValue("uuid-1")})))
	if strings.Contains(got, "position") {
		t.Errorf("expected no position in the input, got %s", got)
	}
}

// An unmanaged label must stay out of the body entirely. Sending an explicit
// null would clear a label curated in the dashboard.
func TestPlanItemsToClient_OmitsUnsetAndUnknownLabels(t *testing.T) {
	for _, planned := range []attr.Value{types.StringNull(), types.StringUnknown()} {
		got := marshalItems(t, itemList(item(map[string]attr.Value{
			"item_id":       types.StringValue("uuid-1"),
			"display_label": planned,
		})))
		for _, key := range []string{"display_label", "description", "section"} {
			if strings.Contains(got, key) {
				t.Errorf("expected %s to be omitted for planned %v, got %s", key, planned, got)
			}
		}
	}
}

func TestPlanItemsToClient_SendsSetLabels(t *testing.T) {
	got := marshalItems(t, itemList(item(map[string]attr.Value{
		"item_type":     types.StringValue("UptimeMonitor"),
		"item_id":       types.StringValue("uuid-2"),
		"display_label": types.StringValue("API"),
		"description":   types.StringValue("Public API"),
		"section":       types.StringValue("Core"),
	})))
	want := `[{"item_type":"UptimeMonitor","item_id":"uuid-2","display_label":"API","description":"Public API","section":"Core"}]`
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

// `sections = []` has to reach the API as an explicit [], and an unchanged list
// has to stay out of the request so a dashboard edit survives.
func TestSectionsUpdate(t *testing.T) {
	core := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("Core")})
	empty := types.ListValueMust(types.StringType, []attr.Value{})

	if got := sectionsUpdate(core, core); got != nil {
		t.Errorf("expected an unchanged list to be omitted, got %v", *got)
	}
	if got := sectionsUpdate(types.ListNull(types.StringType), core); got != nil {
		t.Error("expected a null plan to be omitted")
	}
	got := sectionsUpdate(empty, core)
	if got == nil {
		t.Fatal("expected `sections = []` to be sent")
	}
	if len(*got) != 0 || *got == nil {
		t.Errorf("expected a non-nil empty slice, got %v", *got)
	}
}

// The clearing tag in action: nil Logo has to reach the wire as an explicit
// null, or dropping `logo` from a configuration could never delete the image.
func TestUpdateStatusPageInput_LogoClearsOnNil(t *testing.T) {
	encoded, err := json.Marshal(client.UpdateStatusPageInput{})
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	if got := string(encoded); got != `{"logo":null}` {
		t.Errorf(`expected {"logo":null}, got %s`, got)
	}

	logo := "aGk="
	encoded, err = json.Marshal(client.UpdateStatusPageInput{Logo: &logo})
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	if got := string(encoded); got != `{"logo":"aGk="}` {
		t.Errorf(`expected {"logo":"aGk="}, got %s`, got)
	}
}

// --- validators ---

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name      string
		values    []attr.Value
		wantError bool
	}{
		{"unique", []attr.Value{types.StringValue("Core"), types.StringValue("Edge")}, false},
		{"duplicate", []attr.Value{types.StringValue("Core"), types.StringValue("Core")}, true},
		{"empty", []attr.Value{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &validator.ListResponse{}
			UniqueStrings().ValidateList(context.Background(), validator.ListRequest{
				Path:        path.Root("sections"),
				ConfigValue: types.ListValueMust(types.StringType, tt.values),
			}, resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("expected error=%v, got %v (%v)", tt.wantError, got, resp.Diagnostics)
			}
		})
	}
}

func TestUniqueStrings_SkipsNull(t *testing.T) {
	resp := &validator.ListResponse{}
	UniqueStrings().ValidateList(context.Background(), validator.ListRequest{
		Path:        path.Root("sections"),
		ConfigValue: types.ListNull(types.StringType),
	}, resp)

	if resp.Diagnostics.HasError() {
		t.Errorf("expected a null list to pass, got %v", resp.Diagnostics)
	}
}

func TestBase64PNG(t *testing.T) {
	png := base64.StdEncoding.EncodeToString(append(append([]byte{}, pngSignature...), 'x'))
	jpeg := base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0})
	// 40 decoded bytes: rejected by the encoded-length pre-check at maxBytes=16.
	oversized := base64.StdEncoding.EncodeToString(append(append([]byte{}, pngSignature...), make([]byte, 32)...))
	// 17 decoded bytes: encodes to exactly EncodedLen(16), so it slips past the
	// pre-check and has to be caught on the decoded length.
	justOver := base64.StdEncoding.EncodeToString(append(append([]byte{}, pngSignature...), make([]byte, 9)...))

	tests := []struct {
		name      string
		value     types.String
		maxBytes  int
		wantError bool
	}{
		{"valid png", types.StringValue(png), maxLogoBytes, false},
		{"not base64", types.StringValue("not base64!!"), maxLogoBytes, true},
		{"not a png", types.StringValue(jpeg), maxLogoBytes, true},
		{"too large, rejected before decoding", types.StringValue(oversized), 16, true},
		{"too large by a whisker, rejected after decoding", types.StringValue(justOver), 16, true},
		{"null", types.StringNull(), maxLogoBytes, false},
		{"unknown", types.StringUnknown(), maxLogoBytes, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &validator.StringResponse{}
			Base64PNG(tt.maxBytes).ValidateString(context.Background(), validator.StringRequest{
				Path:        path.Root("logo"),
				ConfigValue: tt.value,
			}, resp)

			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Errorf("expected error=%v, got %v (%v)", tt.wantError, got, resp.Diagnostics)
			}
		})
	}
}

// --- ValidateConfig ---

func baseStatusPageModel() statusPageModel {
	return statusPageModel{
		ID:                          types.Int64Null(),
		Name:                        types.StringValue("Status"),
		Slug:                        types.StringNull(),
		Description:                 types.StringNull(),
		Public:                      types.BoolNull(),
		Uptime:                      types.BoolNull(),
		CustomDomain:                types.StringNull(),
		CustomDomainEnabled:         types.BoolNull(),
		CustomFooter:                types.StringNull(),
		CustomFooterEnabled:         types.BoolNull(),
		IncidentsHistoryEnabled:     types.BoolNull(),
		ThemeVariant:                types.StringNull(),
		ContactURL:                  types.StringNull(),
		SubscriptionsEnabled:        types.BoolNull(),
		UptimeGreenToleranceSeconds: types.Int64Null(),
		UptimeWindowDays:            types.Int64Null(),
		SearchIndexingEnabled:       types.BoolNull(),
		Logo:                        types.StringNull(),
		LogoURL:                     types.StringNull(),
		Sections:                    types.ListNull(types.StringType),
		Items:                       types.ListNull(types.ObjectType{AttrTypes: statusPageItemAttrTypes}),
		CreatedAt:                   types.StringNull(),
		UpdatedAt:                   types.StringNull(),
	}
}

func statusPageRaw(t *testing.T, s schema.Schema, model statusPageModel) tftypes.Value {
	t.Helper()
	state := nullState(t, s)
	if diags := state.Set(context.Background(), &model); diags.HasError() {
		t.Fatalf("building the config: %v", diags)
	}
	return state.Raw
}

func validateStatusPageConfig(t *testing.T, model statusPageModel) *resource.ValidateConfigResponse {
	t.Helper()
	s := statusPageSchema(t)
	resp := &resource.ValidateConfigResponse{}
	NewStatusPageResource().(*statusPageResource).ValidateConfig(context.Background(), resource.ValidateConfigRequest{
		Config: tfsdk.Config{Schema: s, Raw: statusPageRaw(t, s, model)},
	}, resp)
	return resp
}

func TestValidateConfig_UndeclaredSection(t *testing.T) {
	model := baseStatusPageModel()
	model.Sections = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("Core")})
	model.Items = itemList(item(map[string]attr.Value{
		"item_id": types.StringValue("uuid-1"),
		"section": types.StringValue("Edge"),
	}))

	resp := validateStatusPageConfig(t, model)
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error for a section missing from sections")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "Edge") {
		t.Errorf("expected the offending section name in the error, got %q", resp.Diagnostics.Errors()[0].Detail())
	}
}

func TestValidateConfig_DeclaredSection(t *testing.T) {
	model := baseStatusPageModel()
	model.Sections = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("Core")})
	model.Items = itemList(item(map[string]attr.Value{
		"item_id": types.StringValue("uuid-1"),
		"section": types.StringValue("Core"),
	}))

	if resp := validateStatusPageConfig(t, model); resp.Diagnostics.HasError() {
		t.Errorf("expected a declared section to pass, got %v", resp.Diagnostics)
	}
}

// Without a sections list in the configuration the page keeps whatever sections
// were curated elsewhere, which the provider cannot enumerate — so it stays quiet.
func TestValidateConfig_SkipsWhenSectionsUnmanaged(t *testing.T) {
	model := baseStatusPageModel()
	model.Items = itemList(item(map[string]attr.Value{
		"item_id": types.StringValue("uuid-1"),
		"section": types.StringValue("Edge"),
	}))

	if resp := validateStatusPageConfig(t, model); resp.Diagnostics.HasError() {
		t.Errorf("expected no error when sections is unmanaged, got %v", resp.Diagnostics)
	}
}

// --- PreserveForSameItem ---

func planModifyLabel(t *testing.T, index int, config types.String, planItems, stateItems types.List, noPriorState bool) types.String {
	t.Helper()
	ctx := context.Background()
	s := statusPageSchema(t)

	planModel := baseStatusPageModel()
	planModel.Items = planItems
	plan := tfsdk.Plan{Schema: s, Raw: statusPageRaw(t, s, planModel)}

	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	if !noPriorState {
		stateModel := baseStatusPageModel()
		stateModel.Items = stateItems
		state = tfsdk.State{Schema: s, Raw: statusPageRaw(t, s, stateModel)}
	}

	planned := types.StringValue("carried over")
	resp := &planmodifier.StringResponse{PlanValue: planned}
	PreserveForSameItem().PlanModifyString(ctx, planmodifier.StringRequest{
		Path:        path.Root("items").AtListIndex(index).AtName("display_label"),
		ConfigValue: config,
		PlanValue:   planned,
		Plan:        plan,
		State:       state,
	}, resp)
	return resp.PlanValue
}

func labelled(id, label string) attr.Value {
	return item(map[string]attr.Value{
		"item_id":       types.StringValue(id),
		"display_label": types.StringValue(label),
	})
}

func TestPreserveForSameItem(t *testing.T) {
	tests := []struct {
		name        string
		index       int
		config      types.String
		plan, state types.List
		noPrior     bool
		wantUnknown bool
	}{
		{
			name:  "same item keeps its stored label",
			index: 0, config: types.StringNull(),
			plan:  itemList(labelled("a", "API")),
			state: itemList(labelled("a", "API")),
		},
		{
			// Inserting at the top must not hand A's label to the newcomer.
			name:  "an insert resets the newcomer",
			index: 0, config: types.StringNull(),
			plan:        itemList(labelled("b", "carried over"), labelled("a", "API")),
			state:       itemList(labelled("a", "API")),
			wantUnknown: true,
		},
		{
			name:  "an item past the end of the prior list has nothing to preserve",
			index: 1, config: types.StringNull(),
			plan:        itemList(labelled("a", "API"), labelled("b", "carried over")),
			state:       itemList(labelled("a", "API")),
			wantUnknown: true,
		},
		{
			name:  "a configured label is left alone",
			index: 0, config: types.StringValue("Explicit"),
			plan:  itemList(labelled("b", "Explicit")),
			state: itemList(labelled("a", "API")),
		},
		{
			name:  "create is left to the framework's own unknown handling",
			index: 0, config: types.StringNull(),
			plan:    itemList(labelled("a", "API")),
			noPrior: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planModifyLabel(t, tt.index, tt.config, tt.plan, tt.state, tt.noPrior)
			if got.IsUnknown() != tt.wantUnknown {
				t.Errorf("expected unknown=%v, got %v", tt.wantUnknown, got)
			}
		})
	}
}

// itemIDAt addresses a single element rather than decoding the whole list, so
// an out-of-range index comes back as a path error rather than a short slice.
// It has to report that as "no prior item here", and must not leak the
// diagnostic — a new item at the end of the list is normal, not an error.
func TestItemIDAt(t *testing.T) {
	ctx := context.Background()
	s := statusPageSchema(t)

	model := baseStatusPageModel()
	model.Items = itemList(
		labelled("a", "API"),
		item(map[string]attr.Value{"item_id": types.StringUnknown()}),
	)
	state := tfsdk.State{Schema: s, Raw: statusPageRaw(t, s, model)}

	tests := []struct {
		name   string
		index  int
		want   string
		wantOK bool
	}{
		{"in range", 0, "a", true},
		{"item_id not resolved yet", 1, "", false},
		{"past the end", 2, "", false},
		{"negative", -1, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := itemIDAt(ctx, state, tt.index)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("expected (%q, %v), got (%q, %v)", tt.want, tt.wantOK, got, ok)
			}
		})
	}
}

// Every validator and plan modifier owes the framework a description; an empty
// one shows up as a blank line in `terraform providers schema`.
func TestDescriptionsArePresent(t *testing.T) {
	ctx := context.Background()

	descriptions := map[string][2]string{
		"UniqueStrings":       {UniqueStrings().Description(ctx), UniqueStrings().MarkdownDescription(ctx)},
		"Base64PNG":           {Base64PNG(maxLogoBytes).Description(ctx), Base64PNG(maxLogoBytes).MarkdownDescription(ctx)},
		"PreserveForSameItem": {PreserveForSameItem().Description(ctx), PreserveForSameItem().MarkdownDescription(ctx)},
	}

	for name, pair := range descriptions {
		if pair[0] == "" {
			t.Errorf("%s has an empty Description", name)
		}
		if pair[1] != pair[0] {
			t.Errorf("%s: MarkdownDescription %q does not match Description %q", name, pair[1], pair[0])
		}
	}
}
