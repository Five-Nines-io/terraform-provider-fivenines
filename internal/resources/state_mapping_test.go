package resources

import (
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- mapNetworkDeviceToState ---

func TestMapNetworkDeviceToState_NullsFromTheAPI(t *testing.T) {
	// A v2c device leaves every SNMPv3 field null, and reports nothing about
	// itself until the first successful poll.
	device := &client.NetworkDevice{
		ID:        "dev-uuid",
		Name:      "core-sw",
		IPAddress: "192.0.2.1",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	// The plan carries the schema defaults; a null from the API must not wipe them.
	state := &networkDeviceModel{
		DeviceType:        types.StringValue("other"),
		SNMPVersion:       types.StringValue("v2c"),
		SNMPSecurityLevel: types.StringValue("no_auth_no_priv"),
		SNMPAuthProtocol:  types.StringValue("md5"),
		SNMPPrivProtocol:  types.StringValue("des"),
	}
	mapNetworkDeviceToState(device, state)

	for name, got := range map[string]types.String{
		"device_type":         state.DeviceType,
		"snmp_version":        state.SNMPVersion,
		"snmp_security_level": state.SNMPSecurityLevel,
		"snmp_auth_protocol":  state.SNMPAuthProtocol,
		"snmp_priv_protocol":  state.SNMPPrivProtocol,
	} {
		if got.IsNull() {
			t.Errorf("expected %s to keep its planned default, got null", name)
		}
	}
	for name, got := range map[string]types.String{
		"polling_host_id": state.PollingHostID,
		"status":          state.Status,
		"vendor":          state.Vendor,
		"model":           state.Model,
		"sys_name":        state.SysName,
		"last_polled_at":  state.LastPolledAt,
	} {
		if !got.IsNull() {
			t.Errorf("expected %s to be null, got %q", name, got.ValueString())
		}
	}
}

// snmp_username has no schema default and is not Computed, so the config owns
// it: an out-of-band clear has to show up as drift, not be papered over.
func TestMapNetworkDeviceToState_SNMPUsernameReportsDrift(t *testing.T) {
	state := &networkDeviceModel{SNMPUsername: types.StringValue("admin")}
	mapNetworkDeviceToState(&client.NetworkDevice{ID: "dev-uuid"}, state)
	if !state.SNMPUsername.IsNull() {
		t.Errorf("expected a cleared username to read back as null, got %q", state.SNMPUsername.ValueString())
	}

	state = &networkDeviceModel{SNMPUsername: types.StringValue("admin")}
	mapNetworkDeviceToState(&client.NetworkDevice{ID: "dev-uuid", SNMPUsername: ptr("monitor")}, state)
	if state.SNMPUsername.ValueString() != "monitor" {
		t.Errorf("expected the API value to win, got %q", state.SNMPUsername.ValueString())
	}
}

// --- mapStatusPageToState ---

func statusPageItems(t *testing.T, items ...client.StatusPageItem) types.List {
	t.Helper()
	values := make([]attr.Value, len(items))
	for i, item := range items {
		obj, diags := types.ObjectValue(statusPageItemAttrTypes, map[string]attr.Value{
			"item_type": types.StringValue(item.ItemType),
			"item_id":   types.StringValue(item.ItemID),
		})
		if diags.HasError() {
			t.Fatalf("building item: %v", diags)
		}
		values[i] = obj
	}
	list, diags := types.ListValue(types.ObjectType{AttrTypes: statusPageItemAttrTypes}, values)
	if diags.HasError() {
		t.Fatalf("building list: %v", diags)
	}
	return list
}

func TestMapStatusPageToState_Items(t *testing.T) {
	itemType := types.ObjectType{AttrTypes: statusPageItemAttrTypes}

	tests := []struct {
		name    string
		api     []client.StatusPageItem
		planned types.List
		check   func(*testing.T, types.List)
	}{
		{
			name: "items from the API are mapped in order",
			api: []client.StatusPageItem{
				{ItemType: "UptimeMonitor", ItemID: "mon-1", Position: 0},
				{ItemType: "Host", ItemID: "host-1", Position: 1},
			},
			planned: types.ListNull(itemType),
			check: func(t *testing.T, got types.List) {
				if len(got.Elements()) != 2 {
					t.Fatalf("expected 2 items, got %d", len(got.Elements()))
				}
			},
		},
		{
			// `items = []` must survive the round trip or the plan re-proposes
			// it forever and the apply fails as an inconsistent result.
			name:    "an explicitly empty list survives an empty response",
			api:     nil,
			planned: statusPageItems(t),
			check: func(t *testing.T, got types.List) {
				if got.IsNull() || len(got.Elements()) != 0 {
					t.Errorf("expected the pinned empty list to survive, got %v", got)
				}
			},
		},
		{
			name:    "an unmanaged list stays null",
			api:     nil,
			planned: types.ListNull(itemType),
			check: func(t *testing.T, got types.List) {
				if !got.IsNull() {
					t.Errorf("expected null, got %v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &statusPageModel{Items: tt.planned}
			mapStatusPageToState(&client.StatusPage{ID: 1, Name: "Status", Items: tt.api}, state)
			tt.check(t, state.Items)
		})
	}
}

func TestMapStatusPageToState_Nulls(t *testing.T) {
	state := &statusPageModel{ThemeVariant: types.StringValue("system")}
	mapStatusPageToState(&client.StatusPage{ID: 1, Name: "Status"}, state)

	for name, got := range map[string]types.String{
		"description":   state.Description,
		"custom_domain": state.CustomDomain,
		"custom_footer": state.CustomFooter,
	} {
		if !got.IsNull() {
			t.Errorf("expected %s to be null, got %q", name, got.ValueString())
		}
	}
	// theme_variant carries a schema default, so a null must not wipe it.
	if state.ThemeVariant.ValueString() != "system" {
		t.Errorf("expected theme_variant to keep its default, got %v", state.ThemeVariant)
	}
}

// --- planItemsToUpdateInput ---

func TestPlanItemsToUpdateInput(t *testing.T) {
	itemType := types.ObjectType{AttrTypes: statusPageItemAttrTypes}

	// A config that never mentions items must not touch them: the API spec warns
	// Go clients specifically about wiping dashboard-curated content.
	if got := planItemsToUpdateInput(types.ListNull(itemType)); got != nil {
		t.Errorf("expected an absent list to omit the key, got %v", *got)
	}
	if got := planItemsToUpdateInput(types.ListUnknown(itemType)); got != nil {
		t.Errorf("expected an unknown list to omit the key, got %v", *got)
	}

	// `items = []` is the explicit way to empty a page, and has to reach the
	// API as [] rather than being swallowed by omitempty.
	got := planItemsToUpdateInput(statusPageItems(t))
	if got == nil {
		t.Fatal("expected an explicitly empty list to send []")
	}
	if len(*got) != 0 || *got == nil {
		t.Errorf("expected a non-nil empty slice, got %v", *got)
	}

	got = planItemsToUpdateInput(statusPageItems(t,
		client.StatusPageItem{ItemType: "UptimeMonitor", ItemID: "mon-1"},
		client.StatusPageItem{ItemType: "Host", ItemID: "host-1"},
	))
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 items, got %v", got)
	}
	// Position is the array order; the API dropped it from the input but the
	// client still sends what it is given.
	if (*got)[1].ItemID != "host-1" || (*got)[1].Position != 1 {
		t.Errorf("expected order to be preserved, got %+v", (*got)[1])
	}
}

// --- mapTaskToState / mapInstanceToState null cases ---

func TestMapTaskToState_Nulls(t *testing.T) {
	state := &taskModel{TimeZone: types.StringValue("UTC")}
	mapTaskToState(&client.Task{ID: "task-uuid", Name: "backup", ScheduleType: "interval"}, state)

	if !state.Schedule.IsNull() {
		t.Errorf("expected a nil schedule to be null, got %q", state.Schedule.ValueString())
	}
	if !state.HostID.IsNull() {
		t.Errorf("expected host_id to be null, got %q", state.HostID.ValueString())
	}
	// time_zone carries a schema default; a null must not wipe it.
	if state.TimeZone.ValueString() != "UTC" {
		t.Errorf("expected time_zone to keep its default, got %v", state.TimeZone)
	}

	state = &taskModel{TimeZone: types.StringValue("UTC")}
	mapTaskToState(&client.Task{ID: "task-uuid", TimeZone: ptr("Europe/Paris")}, state)
	if state.TimeZone.ValueString() != "Europe/Paris" {
		t.Errorf("expected the API time_zone to win, got %v", state.TimeZone)
	}
}

func TestMapInstanceToState_NullNumbers(t *testing.T) {
	// A host that has never synced reports null for everything the agent fills
	// in, including the numbers — 0 CPUs would be a lie.
	state := &instanceModel{}
	mapInstanceToState(&client.Instance{ID: "uuid-1", DisplayName: "web-1"}, state)

	if !state.CPUCount.IsNull() {
		t.Errorf("expected cpu_count to be null, got %d", state.CPUCount.ValueInt64())
	}
	if !state.MemorySize.IsNull() {
		t.Errorf("expected memory_size to be null, got %d", state.MemorySize.ValueInt64())
	}
	if !state.Source.IsNull() {
		t.Errorf("expected source to be null, got %q", state.Source.ValueString())
	}
}

// Import starts from an empty model — ImportStatePassthroughID puts only `id`
// in state — so stringOrKeep has nothing to keep and every defaulted attribute
// falls back to whatever the API sends. This pins that behaviour: it is fine
// while the API returns these stored fields (snmp_version is Required, and
// device_type is stored with its default), and this test is what fails if that
// assumption ever stops holding.
func TestMapNetworkDeviceToState_ImportStartsFromEmptyState(t *testing.T) {
	state := &networkDeviceModel{}
	mapNetworkDeviceToState(&client.NetworkDevice{
		ID:                "dev-uuid",
		Name:              "core-sw",
		IPAddress:         "192.0.2.10",
		DeviceType:        ptr("switch"),
		SNMPVersion:       ptr("v2c"),
		SNMPSecurityLevel: ptr("no_auth_no_priv"),
		SNMPAuthProtocol:  ptr("md5"),
		SNMPPrivProtocol:  ptr("des"),
	}, state)

	for name, got := range map[string]types.String{
		"device_type":         state.DeviceType,
		"snmp_version":        state.SNMPVersion,
		"snmp_security_level": state.SNMPSecurityLevel,
		"snmp_auth_protocol":  state.SNMPAuthProtocol,
		"snmp_priv_protocol":  state.SNMPPrivProtocol,
	} {
		if got.IsNull() {
			t.Errorf("%s is null after import; a defaulted attribute reading back null "+
				"makes ImportStateVerify diff against the schema default", name)
		}
	}

	// The same call with an API that omits them would yield nulls. That is the
	// failure the acceptance suite's ImportStateVerify exists to catch.
	empty := &networkDeviceModel{}
	mapNetworkDeviceToState(&client.NetworkDevice{ID: "dev-uuid"}, empty)
	if !empty.DeviceType.IsNull() {
		t.Error("expected the no-prior-state, no-API-value case to be null — " +
			"if this changed, revisit the import path")
	}
}
