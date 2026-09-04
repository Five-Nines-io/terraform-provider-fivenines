package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func ptr[T any](v T) *T { return &v }

// --- mapInstanceToState ---

func TestMapInstanceToState(t *testing.T) {
	inst := &client.Instance{
		ID:          "uuid-1",
		DisplayName: "web-1",
		Description: ptr("primary web tier"),
		Hostname:    ptr("web-1.local"),
		HostGroupID: ptr(int64(7)),
		Enabled:     true,
		CPUCount:    ptr(int64(4)),
		MemorySize:  ptr(int64(8589934592)),
		CreatedAt:   "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-01-01T00:00:00Z",

		RedisEnabled:     ptr(true),
		RedisPort:        ptr(int64(6390)),
		TSDBVerifySSL:    ptr(true),
		CaddyAdminAPIURL: ptr("http://localhost:2019"),

		RedisPasswordSet:      true,
		ProxmoxTokenSecretSet: false,
	}

	state := &instanceModel{}
	// The write-only credentials live only in the configuration; the API
	// cannot echo them, so a read keeps one exactly while its _set boolean
	// reports it stored — and clears one the API reports gone.
	state.RedisPassword = types.StringValue("s3cret")
	state.ProxmoxTokenSecret = types.StringValue("cleared-in-dashboard")
	mapInstanceToState(inst, state)

	if state.ID.ValueString() != "uuid-1" {
		t.Errorf("expected ID uuid-1, got %s", state.ID.ValueString())
	}
	if state.DisplayName.ValueString() != "web-1" {
		t.Errorf("expected display_name web-1, got %s", state.DisplayName.ValueString())
	}
	if state.Description.ValueString() != "primary web tier" {
		t.Errorf("expected description to map, got %v", state.Description)
	}
	if state.HostGroupID.ValueInt64() != 7 {
		t.Errorf("expected host_group_id 7, got %v", state.HostGroupID)
	}
	if !state.Enabled.ValueBool() {
		t.Error("expected enabled true")
	}
	if state.CPUCount.ValueInt64() != 4 {
		t.Errorf("expected cpu_count 4, got %d", state.CPUCount.ValueInt64())
	}
	if state.MemorySize.ValueInt64() != 8589934592 {
		t.Errorf("expected memory_size 8589934592, got %d", state.MemorySize.ValueInt64())
	}
	if !state.LastSyncAt.IsNull() {
		t.Error("expected last_sync_at to be null")
	}
	// An instance that never synced reports null for everything the agent
	// fills in; those must stay null instead of collapsing to "".
	if !state.IPv4.IsNull() {
		t.Errorf("expected ipv4 to be null, got %q", state.IPv4.ValueString())
	}
	if !state.KernelVersion.IsNull() {
		t.Errorf("expected kernel_version to be null, got %q", state.KernelVersion.ValueString())
	}

	// Collector settings map as stored, and a toggle the API reports null for
	// (a host predating that collector's column) stays null, not false.
	if !state.RedisEnabled.ValueBool() || state.RedisPort.ValueInt64() != 6390 {
		t.Errorf("expected redis settings to map, got %v/%v", state.RedisEnabled, state.RedisPort)
	}
	if !state.TSDBVerifySSL.ValueBool() {
		t.Error("expected tsdb_verify_ssl true")
	}
	if state.CaddyAdminAPIURL.ValueString() != "http://localhost:2019" {
		t.Errorf("expected caddy_admin_api_url to map, got %v", state.CaddyAdminAPIURL)
	}
	if !state.ZFSEnabled.IsNull() {
		t.Errorf("expected zfs_enabled to stay null, got %v", state.ZFSEnabled)
	}

	// The _set booleans carry the only readable truth about the credentials;
	// the credentials themselves are never overwritten by a read.
	if !state.RedisPasswordSet.ValueBool() {
		t.Error("expected redis_password_set true")
	}
	if state.ProxmoxTokenSecretSet.ValueBool() {
		t.Error("expected proxmox_token_secret_set false")
	}
	if state.RedisPassword.ValueString() != "s3cret" {
		t.Errorf("expected the configured redis_password to survive a read, got %v", state.RedisPassword)
	}
	if !state.MySQLPassword.IsNull() {
		t.Errorf("expected an unconfigured secret to stay null, got %v", state.MySQLPassword)
	}
	// proxmox_token_secret_set is false: the server holds no secret, so the
	// stale configured value must clear — that null is what makes the next
	// plan re-send the credential instead of reporting "No changes".
	if !state.ProxmoxTokenSecret.IsNull() {
		t.Errorf("expected a secret the API reports unstored to clear, got %v", state.ProxmoxTokenSecret)
	}
}

// The provider's half of the blank-means-keep contract: a secret the plan
// holds no value for is OMITTED from the request body — never sent as "" (the
// server would keep the stored value while state claims empty) and never as
// null. host_group_id is the deliberate exception: Optional-only, so a null
// plan value marshals as an explicit null that removes the host from its
// group.
func TestInstanceSettingsFromPlan_OmitsWhatItDoesNotManage(t *testing.T) {
	plan := &instanceModel{
		DisplayName:   types.StringValue("web-1"),
		RedisPassword: types.StringValue("s3cret"),
		DockerEnabled: types.BoolValue(true),
		// A fresh plan holds unknown for every unresolved Computed setting;
		// those must be omitted too, not sent as zero values.
		CephEnabled: types.BoolUnknown(),
		RedisPort:   types.Int64Unknown(),
	}

	raw, err := json.Marshal(client.UpdateInstanceInput{
		DisplayName:           stringPtr(plan.DisplayName),
		InstanceSettingsInput: instanceSettingsFromPlan(plan),
	})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}

	if got := string(body["redis_password"]); got != `"s3cret"` {
		t.Errorf("expected the configured secret on the wire, got %s", got)
	}
	if got, present := body["postgresql_password"]; present {
		t.Errorf("an unmanaged secret must be omitted, got %s", got)
	}
	if got := string(body["docker_enabled"]); got != "true" {
		t.Errorf("expected docker_enabled true, got %s", got)
	}
	for _, key := range []string{"ceph_enabled", "redis_port", "cluster_name", "logs_enabled"} {
		if got, present := body[key]; present {
			t.Errorf("expected unset %s to be omitted so the server keeps its value, got %s", key, got)
		}
	}
	if got, present := body["host_group_id"]; !present || string(got) != "null" {
		t.Errorf("host_group_id must be an explicit null when unset, got %s (present=%t)", got, present)
	}
}

// instanceSecretAttrs is the canonical list of the ten write-only
// credentials; attribute names and wire names coincide for this resource.
var instanceSecretAttrs = client.InstanceSecretKeys

// instanceModelFieldsByTag indexes instanceModel's fields by tfsdk tag, which
// for this resource is also the wire name of every settings field.
func instanceModelFieldsByTag(t *testing.T) map[string]int {
	t.Helper()
	modelType := reflect.TypeOf(instanceModel{})
	fields := make(map[string]int, modelType.NumField())
	for i := 0; i < modelType.NumField(); i++ {
		tag := modelType.Field(i).Tag.Get("tfsdk")
		if tag == "" {
			t.Fatalf("instanceModel.%s carries no tfsdk tag", modelType.Field(i).Name)
		}
		fields[tag] = i
	}
	return fields
}

// One probe per field: set exactly one attribute on the plan, build the input,
// and require the body to carry exactly that key (plus the ever-present
// host_group_id null) with the probe's value. Sampling cannot see a crossed
// copy — plan.RedisPort feeding memcached_port survives every check that does
// not isolate the field — and across 79 hand-written copy lines, crossed and
// missing lines are the realistic failure.
func TestInstanceSettingsFromPlan_EveryFieldReachesItsOwnKey(t *testing.T) {
	modelFields := instanceModelFieldsByTag(t)

	inputType := reflect.TypeOf(client.InstanceSettingsInput{})
	for i := 0; i < inputType.NumField(); i++ {
		field := inputType.Field(i)
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")

		idx, ok := modelFields[key]
		if !ok {
			t.Errorf("InstanceSettingsInput.%s (json %q) has no instanceModel attribute of that name", field.Name, key)
			continue
		}

		plan := instanceModel{}
		fv := reflect.ValueOf(&plan).Elem().Field(idx)
		var want string
		switch fv.Interface().(type) {
		case types.String:
			fv.Set(reflect.ValueOf(types.StringValue("probe-" + key)))
			want = `"probe-` + key + `"`
		case types.Bool:
			fv.Set(reflect.ValueOf(types.BoolValue(true)))
			want = "true"
		case types.Int64:
			fv.Set(reflect.ValueOf(types.Int64Value(4242)))
			want = "4242"
		default:
			t.Fatalf("unhandled model type %T for %s", fv.Interface(), key)
		}

		raw, err := json.Marshal(client.UpdateInstanceInput{InstanceSettingsInput: instanceSettingsFromPlan(&plan)})
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}

		if got := string(body[key]); got != want {
			t.Errorf("%s: expected %s on the wire, got %s", key, want, got)
		}
		delete(body, key)
		delete(body, "host_group_id") // always present, null when unset
		for stray := range body {
			t.Errorf("%s: probe leaked into %q — a crossed copy in instanceSettingsFromPlan", key, stray)
		}
	}
}

// The read-side mirror: one probe per API field must land in exactly its own
// attribute and move nothing else — with every write-only credential seeded in
// state throughout, because the API cannot echo one back: a read keeps the
// configured value exactly while the paired `_set` boolean reports it stored,
// and clears it to null when the API reports none (see storedSecret).
func TestMapInstanceToState_EveryFieldLandsInItsOwnAttribute(t *testing.T) {
	modelFields := instanceModelFieldsByTag(t)
	modelType := reflect.TypeOf(instanceModel{})

	secretAttr := make(map[string]bool, len(instanceSecretAttrs))
	for _, name := range instanceSecretAttrs {
		secretAttr[name] = true
	}

	// A state as Read receives it: the configured credentials present, since
	// the API cannot echo them back.
	seed := func() *instanceModel {
		m := &instanceModel{}
		mv := reflect.ValueOf(m).Elem()
		for _, name := range instanceSecretAttrs {
			mv.Field(modelFields[name]).Set(reflect.ValueOf(types.StringValue("configured-" + name)))
		}
		return m
	}

	// An empty Instance reports every _set boolean false — no secret stored —
	// so a read must clear the configured credentials rather than leave state
	// claiming values the server does not hold.
	baseline := seed()
	mapInstanceToState(&client.Instance{}, baseline)
	baseValue := reflect.ValueOf(*baseline)
	for _, name := range instanceSecretAttrs {
		got := baseValue.Field(modelFields[name]).Interface().(types.String)
		if !got.IsNull() {
			t.Fatalf("mapInstanceToState must clear %s when its _set boolean reports no stored secret, got %v", name, got)
		}
	}

	// The converse: while the API reports one stored, the configured value
	// survives the read exactly as written.
	kept := seed()
	mapInstanceToState(&client.Instance{
		RedisPasswordSet: true, PostgreSQLPasswordSet: true, MySQLPasswordSet: true,
		RabbitMQPasswordSet: true, HAProxyPasswordSet: true, ProxmoxTokenSecretSet: true,
		TSDBAuthHeaderValueSet: true, TSDBBasicAuthPasswordSet: true,
		VLLMAuthHeaderValueSet: true, SGLangAuthHeaderValueSet: true,
	}, kept)
	keptValue := reflect.ValueOf(*kept)
	for _, name := range instanceSecretAttrs {
		got := keptValue.Field(modelFields[name]).Interface().(types.String)
		if got.ValueString() != "configured-"+name {
			t.Fatalf("mapInstanceToState must keep %s while its _set boolean reports a stored secret, got %v", name, got)
		}
	}

	instType := reflect.TypeOf(client.Instance{})
	for i := 0; i < instType.NumField(); i++ {
		field := instType.Field(i)
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if _, ok := modelFields[key]; !ok {
			t.Errorf("Instance.%s (json %q) has no instanceModel attribute of that name", field.Name, key)
			continue
		}

		inst := client.Instance{}
		fv := reflect.ValueOf(&inst).Elem().Field(i)
		var want attr.Value
		switch fv.Interface().(type) {
		case *string:
			fv.Set(reflect.ValueOf(ptr("probe-" + key)))
			want = types.StringValue("probe-" + key)
		case string:
			fv.SetString("probe-" + key)
			want = types.StringValue("probe-" + key)
		case *bool:
			fv.Set(reflect.ValueOf(ptr(true)))
			want = types.BoolValue(true)
		case bool:
			fv.SetBool(true)
			want = types.BoolValue(true)
		case *int64:
			fv.Set(reflect.ValueOf(ptr(int64(4242))))
			want = types.Int64Value(4242)
		default:
			t.Fatalf("unhandled Instance field type %T for %s", fv.Interface(), key)
		}

		state := seed()
		mapInstanceToState(&inst, state)

		sv := reflect.ValueOf(*state)
		for j := 0; j < modelType.NumField(); j++ {
			tag := modelType.Field(j).Tag.Get("tfsdk")
			got := sv.Field(j).Interface().(attr.Value)
			if tag == key {
				if !got.Equal(want) {
					t.Errorf("%s: expected the probe to land as %s, got %s", key, want, got)
				}
				continue
			}
			// A true `_set` probe legitimately moves one more attribute: its
			// paired credential survives instead of being cleared.
			if paired := strings.TrimSuffix(key, "_set"); paired != key && tag == paired && secretAttr[paired] {
				if want := types.StringValue("configured-" + paired); !got.Equal(want) {
					t.Errorf("%s: expected the paired %s to survive the read, got %s", key, paired, got)
				}
				continue
			}
			if base := baseValue.Field(j).Interface().(attr.Value); !got.Equal(base) {
				t.Errorf("%s: probe leaked into %q — a crossed assignment in mapInstanceToState (got %s, baseline %s)", key, tag, got, base)
			}
		}
	}
}

// --- instance schema ---

// The ten credentials must be Sensitive, Optional-only and blank-rejecting,
// each paired with a Computed-only `_set` presence boolean — and nothing else
// in the schema may look like a credential without being registered as one,
// or it ships un-Sensitive and un-guarded.
func TestInstanceSchema_SecretsAreSensitiveAndRejectBlank(t *testing.T) {
	ctx := context.Background()
	s := instanceSchemaForTest(t)

	secretSet := make(map[string]bool, len(instanceSecretAttrs))
	for _, name := range instanceSecretAttrs {
		secretSet[name] = true

		a, ok := s.Attributes[name].(rschema.StringAttribute)
		if !ok {
			t.Errorf("%s: expected a StringAttribute, got %T", name, s.Attributes[name])
			continue
		}
		if !a.Sensitive {
			t.Errorf("%s must be Sensitive — it is a credential", name)
		}
		if a.Computed {
			t.Errorf("%s must not be Computed: the API never returns it, so it would stay unknown forever", name)
		}
		if !a.Optional {
			t.Errorf("%s must be Optional", name)
		}
		if len(a.Validators) == 0 {
			t.Errorf("%s must reject the empty string: the API reads \"\" as \"keep\", which would "+
				"leave state claiming \"\" while the server still holds a credential", name)
		} else {
			// The server drops a secret via Rails `blank?`, so whitespace-only
			// counts as blank too — including the Unicode spaces (NBSP,
			// vertical tab) that Go's ASCII-only \s does not cover. Every
			// spelling must die at plan time.
			for _, blank := range []string{"", "  \t", " ", "\v  "} {
				resp := &validator.StringResponse{}
				a.Validators[0].ValidateString(ctx, validator.StringRequest{
					Path:        path.Root(name),
					ConfigValue: types.StringValue(blank),
				}, resp)
				if !resp.Diagnostics.HasError() {
					t.Errorf("%s: expected the blank %q to be rejected at plan time", name, blank)
				}
			}
		}

		presence, ok := s.Attributes[name+"_set"].(rschema.BoolAttribute)
		if !ok {
			t.Errorf("%s_set: expected a BoolAttribute, got %T", name, s.Attributes[name+"_set"])
			continue
		}
		if !presence.Computed || presence.Optional {
			t.Errorf("%s_set must be Computed-only — it is the API's answer, not the practitioner's", name)
		}
	}

	for name := range s.Attributes {
		if strings.HasSuffix(name, "_password") || strings.HasSuffix(name, "_secret") ||
			strings.HasSuffix(name, "_auth_header_value") {
			if !secretSet[name] {
				t.Errorf("%s looks like a credential but is not in instanceSecretAttrs — register it there "+
					"(and in the API's blank-means-keep list) or rename it", name)
			}
		}
	}
}

// Every settings attribute is Optional+Computed with NO provider-side default
// — the server's defaults are not uniform, and a provider default would fight
// them on every host. host_group_id is the deliberate Optional-only exception
// that makes clearing possible.
func TestInstanceSchema_SettingsFollowTheServerNotProviderDefaults(t *testing.T) {
	s := instanceSchemaForTest(t)

	hg, ok := s.Attributes["host_group_id"].(rschema.Int64Attribute)
	if !ok || !hg.Optional || hg.Computed {
		t.Error("host_group_id must be Optional-only: as Optional+Computed a dropped reference would " +
			"stick to the prior group instead of clearing the membership")
	}

	secretSet := make(map[string]bool, len(instanceSecretAttrs))
	for _, name := range instanceSecretAttrs {
		secretSet[name] = true
	}

	// Writable on the wire ⇒ configurable in the schema, and settings must be
	// Optional+Computed so an unmanaged one follows the server.
	inputKeys := make(map[string]bool)
	inputType := reflect.TypeOf(client.InstanceSettingsInput{})
	for i := 0; i < inputType.NumField(); i++ {
		key, _, _ := strings.Cut(inputType.Field(i).Tag.Get("json"), ",")
		inputKeys[key] = true
		if key == "host_group_id" || secretSet[key] {
			continue
		}
		a, present := s.Attributes[key]
		if !present {
			t.Errorf("%s is writable in InstanceSettingsInput but missing from the schema", key)
			continue
		}
		if !a.IsOptional() || !a.IsComputed() {
			t.Errorf("%s is a server-defaulted setting, so it must be Optional+Computed; got optional=%t computed=%t",
				key, a.IsOptional(), a.IsComputed())
		}
	}

	for name, a := range s.Attributes {
		// Configurable in the schema ⇒ actually sent: an Optional attribute
		// outside the input would accept configuration and silently drop it.
		if a.IsOptional() && !inputKeys[name] &&
			name != "display_name" && name != "enabled" && name != "maintenance_mode" {
			t.Errorf("%s is Optional in the schema but absent from InstanceSettingsInput — configuring it would do nothing", name)
		}

		if name == "enabled" || name == "maintenance_mode" {
			continue // deliberate provider defaults, pre-dating the settings
		}
		var hasDefault bool
		switch at := a.(type) {
		case rschema.BoolAttribute:
			hasDefault = at.Default != nil
		case rschema.StringAttribute:
			hasDefault = at.Default != nil
		case rschema.Int64Attribute:
			hasDefault = at.Default != nil
		}
		if hasDefault {
			t.Errorf("%s carries a provider-side default; the server owns these defaults, so a provider "+
				"default would fight the API on every host that differs", name)
		}
	}
}

// --- mapTaskToState ---

func TestMapTaskToState_Active(t *testing.T) {
	task := &client.Task{
		ID:           "task-uuid",
		Name:         "health-check",
		ScheduleType: "interval",
		Status:       "active",
		PingKey:      "pk_123",
		PingURL:      "https://fivenines.io/ping/pk_123",
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if state.Paused.ValueBool() != false {
		t.Error("expected paused=false for active task")
	}
	if state.PingKey.ValueString() != "pk_123" {
		t.Errorf("expected ping_key pk_123, got %s", state.PingKey.ValueString())
	}
}

func TestMapTaskToState_Paused(t *testing.T) {
	task := &client.Task{
		ID:           "task-uuid",
		Name:         "paused-task",
		ScheduleType: "cron",
		Schedule:     ptr("0 * * * *"),
		Status:       "paused",
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if state.Paused.ValueBool() != true {
		t.Error("expected paused=true for paused task")
	}
}

func TestMapTaskToState_IntervalSeconds(t *testing.T) {
	interval := int64(300)
	task := &client.Task{
		ID:              "task-uuid",
		Name:            "interval-task",
		ScheduleType:    "interval",
		Status:          "active",
		IntervalSeconds: &interval,
		CreatedAt:       "2026-01-01T00:00:00Z",
		UpdatedAt:       "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if state.IntervalSeconds.ValueInt64() != 300 {
		t.Errorf("expected interval_seconds 300, got %d", state.IntervalSeconds.ValueInt64())
	}
}

func TestMapTaskToState_NilIntervalSeconds(t *testing.T) {
	task := &client.Task{
		ID:           "task-uuid",
		Name:         "cron-task",
		ScheduleType: "cron",
		Status:       "active",
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if !state.IntervalSeconds.IsNull() {
		t.Error("expected interval_seconds to be null for cron task")
	}
}

func TestMapTaskToState_EmptyScheduleIsNull(t *testing.T) {
	interval := int64(300)
	task := &client.Task{
		ID:              "task-uuid",
		Name:            "interval-task",
		ScheduleType:    "interval",
		Status:          "active",
		Schedule:        ptr(""),
		IntervalSeconds: &interval,
		CreatedAt:       "2026-01-01T00:00:00Z",
		UpdatedAt:       "2026-01-01T00:00:00Z",
	}

	state := &taskModel{}
	mapTaskToState(task, state)

	if !state.Schedule.IsNull() {
		t.Errorf("expected schedule to be null for interval task, got %v", state.Schedule)
	}
}

// --- validateTaskSchedule ---

func TestValidateTaskSchedule_CronRequiresSchedule(t *testing.T) {
	diags := validateTaskSchedule(taskModel{
		ScheduleType: types.StringValue("cron"),
		Schedule:     types.StringNull(),
	})

	if !diags.HasError() {
		t.Fatal("expected an error when schedule_type is cron without schedule")
	}
	if got := diags.Errors()[0].Detail(); got != `"schedule" is required when "schedule_type" is "cron".` {
		t.Errorf("unexpected detail: %s", got)
	}
}

func TestValidateTaskSchedule_IntervalRequiresIntervalSeconds(t *testing.T) {
	diags := validateTaskSchedule(taskModel{
		ScheduleType:    types.StringValue("interval"),
		IntervalSeconds: types.Int64Null(),
	})

	if !diags.HasError() {
		t.Fatal("expected an error when schedule_type is interval without interval_seconds")
	}
}

func TestValidateTaskSchedule_Valid(t *testing.T) {
	cron := validateTaskSchedule(taskModel{
		ScheduleType: types.StringValue("cron"),
		Schedule:     types.StringValue("0 2 * * *"),
	})
	if cron.HasError() {
		t.Errorf("expected no error for a valid cron task, got %v", cron.Errors())
	}

	interval := validateTaskSchedule(taskModel{
		ScheduleType:    types.StringValue("interval"),
		IntervalSeconds: types.Int64Value(300),
	})
	if interval.HasError() {
		t.Errorf("expected no error for a valid interval task, got %v", interval.Errors())
	}
}

func TestValidateTaskSchedule_NullScheduleType(t *testing.T) {
	diags := validateTaskSchedule(taskModel{})
	if diags.HasError() {
		t.Errorf("expected no error for a null schedule_type, got %v", diags.Errors())
	}
}

// A schedule sourced from another resource is unknown at validate time; the API
// gets the last word rather than the plan failing outright.
func TestValidateTaskSchedule_UnknownValuesDeferred(t *testing.T) {
	unknownType := validateTaskSchedule(taskModel{
		ScheduleType: types.StringUnknown(),
		Schedule:     types.StringNull(),
	})
	if unknownType.HasError() {
		t.Errorf("expected no error for unknown schedule_type, got %v", unknownType.Errors())
	}

	unknownSchedule := validateTaskSchedule(taskModel{
		ScheduleType: types.StringValue("cron"),
		Schedule:     types.StringUnknown(),
	})
	if unknownSchedule.HasError() {
		t.Errorf("expected no error for unknown schedule, got %v", unknownSchedule.Errors())
	}

	unknownInterval := validateTaskSchedule(taskModel{
		ScheduleType:    types.StringValue("interval"),
		IntervalSeconds: types.Int64Unknown(),
	})
	if unknownInterval.HasError() {
		t.Errorf("expected no error for unknown interval_seconds, got %v", unknownInterval.Errors())
	}
}

// --- mapWorkflowToState ---

func TestMapWorkflowToState(t *testing.T) {
	interval := int64(60)
	versionID := int64(5)
	wf := &client.Workflow{
		ID:                 42,
		Name:               "CPU Alert",
		Description:        ptr("Alerts on high CPU"),
		Status:             "active",
		IntervalSeconds:    &interval,
		TriggerType:        ptr("metric_threshold"),
		TriggerTypeLabel:   ptr("Instance Metric"),
		PublishedVersionID: &versionID,
		CreatedAt:          "2026-01-01T00:00:00Z",
		UpdatedAt:          "2026-01-01T00:00:00Z",
	}

	state := &workflowModel{}
	mapWorkflowToState(wf, state)

	if state.ID.ValueInt64() != 42 {
		t.Errorf("expected ID 42, got %d", state.ID.ValueInt64())
	}
	if state.IntervalSeconds.ValueInt64() != 60 {
		t.Errorf("expected interval_seconds 60, got %d", state.IntervalSeconds.ValueInt64())
	}
	if state.PublishedVersionID.ValueInt64() != 5 {
		t.Errorf("expected published_version_id 5, got %d", state.PublishedVersionID.ValueInt64())
	}
}

func TestMapWorkflowToState_NilOptionals(t *testing.T) {
	wf := &client.Workflow{
		ID:        1,
		Name:      "Draft WF",
		Status:    "draft",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}

	state := &workflowModel{}
	mapWorkflowToState(wf, state)

	if !state.IntervalSeconds.IsNull() {
		t.Error("expected interval_seconds to be null")
	}
	if !state.PublishedVersionID.IsNull() {
		t.Error("expected published_version_id to be null")
	}
	if !state.NextEvaluationAt.IsNull() {
		t.Error("expected next_evaluation_at to be null")
	}
}

// Verify types.String null behavior (framework contract test)
func TestTypesStringNull(t *testing.T) {
	s := types.StringNull()
	if !s.IsNull() {
		t.Error("expected IsNull() to be true")
	}
}

// --- uptime monitor: protocol cross-field validation ---

func TestMissingProtocolAttributes(t *testing.T) {
	tests := []struct {
		name   string
		config uptimeMonitorModel
		want   []string
	}{
		{
			name:   "https without url",
			config: uptimeMonitorModel{Protocol: types.StringValue("https")},
			want:   []string{"url"},
		},
		{
			name: "https with url",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("https"),
				URL:      types.StringValue("https://example.com"),
			},
		},
		{
			name:   "tcp without hostname or port",
			config: uptimeMonitorModel{Protocol: types.StringValue("tcp")},
			want:   []string{"hostname", "port"},
		},
		{
			name: "tcp with hostname but no port",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("tcp"),
				Hostname: types.StringValue("db.example.com"),
			},
			want: []string{"port"},
		},
		{
			name:   "icmp without hostname",
			config: uptimeMonitorModel{Protocol: types.StringValue("icmp")},
			want:   []string{"hostname"},
		},
		{
			name:   "dns without hostname or record type",
			config: uptimeMonitorModel{Protocol: types.StringValue("dns")},
			want:   []string{"hostname", "dns_record_type"},
		},
		{
			name: "dns with record type but no hostname",
			config: uptimeMonitorModel{
				Protocol:      types.StringValue("dns"),
				DNSRecordType: types.StringValue("A"),
			},
			want: []string{"hostname"},
		},
		{
			name: "dns fully configured",
			config: uptimeMonitorModel{
				Protocol:      types.StringValue("dns"),
				Hostname:      types.StringValue("example.com"),
				DNSRecordType: types.StringValue("A"),
			},
		},
		{
			name: "unknown value is not treated as missing",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("https"),
				URL:      types.StringUnknown(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := missingProtocolAttributes(protocolScopedValues(tt.config), tt.config.Protocol)
			if len(got) != len(tt.want) {
				t.Fatalf("expected missing %v, got %v", tt.want, got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("expected missing %v, got %v", tt.want, got)
				}
			}
		})
	}
}

// --- uptime monitor: mapToState ---

func mapUptimeMonitor(t *testing.T, m *client.UptimeMonitor, state *uptimeMonitorModel) {
	t.Helper()
	var diags diag.Diagnostics
	(&uptimeMonitorResource{}).mapToState(context.Background(), m, state, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestMapUptimeMonitorToState_PausedFromStatus(t *testing.T) {
	for status, wantPaused := range map[string]bool{
		"paused": true, "up": false, "down": false, "unknown": false, "recovering": false,
	} {
		state := &uptimeMonitorModel{}
		mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Status: status}, state)

		if state.Paused.ValueBool() != wantPaused {
			t.Errorf("status %q: expected paused %v, got %v", status, wantPaused, state.Paused.ValueBool())
		}
		if state.Status.ValueString() != status {
			t.Errorf("expected status %q, got %q", status, state.Status.ValueString())
		}
	}
}

func TestMapUptimeMonitorToState_DNSExpectedRecords(t *testing.T) {
	emptyList := types.ListValueMust(types.StringType, []attr.Value{})

	tests := []struct {
		name      string
		apiValue  []string
		prior     types.List
		wantNull  bool
		wantElems int
	}{
		{
			name:     "unset stays null",
			prior:    types.ListNull(types.StringType),
			wantNull: true,
		},
		{
			// The API normalises a pinned [] to "no expectation" and reads it back
			// as [], so a config of [] must not flip to null and diff forever.
			name:      "explicit empty list is preserved",
			prior:     emptyList,
			wantNull:  false,
			wantElems: 0,
		},
		{
			name:      "records are read back",
			apiValue:  []string{"1.2.3.4", "5.6.7.8"},
			prior:     types.ListNull(types.StringType),
			wantNull:  false,
			wantElems: 2,
		},
		{
			name:      "records replace a previously empty list",
			apiValue:  []string{"1.2.3.4"},
			prior:     emptyList,
			wantNull:  false,
			wantElems: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &uptimeMonitorModel{DNSExpectedRecords: tt.prior}
			mapUptimeMonitor(t, &client.UptimeMonitor{
				ID:                 "mon-uuid",
				Protocol:           "dns",
				DNSExpectedRecords: tt.apiValue,
			}, state)

			if state.DNSExpectedRecords.IsNull() != tt.wantNull {
				t.Fatalf("expected null=%v, got %v", tt.wantNull, state.DNSExpectedRecords)
			}
			if !tt.wantNull && len(state.DNSExpectedRecords.Elements()) != tt.wantElems {
				t.Errorf("expected %d elements, got %v", tt.wantElems, state.DNSExpectedRecords.Elements())
			}
		})
	}
}

func TestIsEmptyList(t *testing.T) {
	if isEmptyList(types.ListNull(types.StringType)) {
		t.Error("null list is not an empty list")
	}
	if isEmptyList(types.ListUnknown(types.StringType)) {
		t.Error("unknown list is not an empty list")
	}
	if !isEmptyList(types.ListValueMust(types.StringType, []attr.Value{})) {
		t.Error("expected empty list")
	}
	if isEmptyList(types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")})) {
		t.Error("populated list is not empty")
	}
}

// --- uptime monitor: the inverse protocol table ---

func TestForbiddenProtocolAttributes(t *testing.T) {
	tests := []struct {
		name   string
		config uptimeMonitorModel
		want   []string
	}{
		{
			name: "https with a dns attribute left in config",
			config: uptimeMonitorModel{
				Protocol:      types.StringValue("https"),
				DNSRecordType: types.StringValue("A"),
			},
			want: []string{"dns_record_type"},
		},
		{
			// The switch that motivated the table: leaving headers on a dns monitor
			// plans a known map that the apply nulls out.
			name: "dns with http attributes left in config",
			config: uptimeMonitorModel{
				Protocol:      types.StringValue("dns"),
				CustomHeaders: types.MapValueMust(types.StringType, map[string]attr.Value{"X": types.StringValue("y")}),
				Keyword:       types.StringValue("ok"),
			},
			want: []string{"keyword", "custom_headers"},
		},
		{
			name: "tcp with a keyword left in config",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("tcp"),
				Keyword:  types.StringValue("ok"),
			},
			want: []string{"keyword"},
		},
		{
			name:   "a clean https config forbids nothing",
			config: uptimeMonitorModel{Protocol: types.StringValue("https"), URL: types.StringValue("https://example.com")},
		},
		{
			// Unknown comes from an unresolved reference, not from the user setting
			// a value the protocol cannot use.
			name: "unknown values are not flagged",
			config: uptimeMonitorModel{
				Protocol: types.StringValue("tcp"),
				Keyword:  types.StringUnknown(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forbiddenProtocolAttributes(protocolScopedValues(tt.config), tt.config.Protocol)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("expected %v, got %v", tt.want, got)
				}
			}
		})
	}
}

// Every name in either protocol table must resolve in protocolScopedValues, or
// the lookup hands .IsNull() a nil interface and panics mid-plan.
func TestProtocolTablesResolve(t *testing.T) {
	values := protocolScopedValues(uptimeMonitorModel{})
	for _, table := range []map[string][]string{protocolRequirements, protocolForbidden} {
		for protocol, names := range table {
			for _, name := range names {
				if _, ok := values[name]; !ok {
					t.Errorf("protocol %q references %q, which protocolScopedValues does not provide", protocol, name)
				}
			}
		}
	}
	// postOnly is a third table indexed against the same map. The comma-ok
	// guarding that lookup is unreachable while the two agree, so nothing else
	// would catch them drifting apart before a nil interface panics mid-plan.
	for _, name := range postOnly {
		if _, ok := values[name]; !ok {
			t.Errorf("postOnly references %q, which protocolScopedValues does not provide", name)
		}
	}
}

func TestMapUptimeMonitorToState_KeywordEmptyVsNull(t *testing.T) {
	// An explicitly configured "" must survive; an absent keyword must be null.
	state := &uptimeMonitorModel{Keyword: types.StringValue("")}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Keyword: ""}, state)
	if state.Keyword.IsNull() {
		t.Error("an explicitly empty keyword must stay an empty string, not become null")
	}

	state = &uptimeMonitorModel{Keyword: types.StringNull()}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Keyword: ""}, state)
	if !state.Keyword.IsNull() {
		t.Errorf("an unset keyword must be null, got %q", state.Keyword.ValueString())
	}

	state = &uptimeMonitorModel{Keyword: types.StringNull()}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", Keyword: "healthy"}, state)
	if state.Keyword.ValueString() != "healthy" {
		t.Errorf("expected healthy, got %q", state.Keyword.ValueString())
	}
}

func TestMapUptimeMonitorToState_CustomBodyEmptyVsNull(t *testing.T) {
	// `custom_body = ""` is a legal config, and CreateUptimeMonitorInput drops it
	// via omitempty, so the API answers null on the very first apply. Without the
	// carve-out the plan holds "" and state gets null — "Provider produced
	// inconsistent result after apply" before the resource ever exists.
	state := &uptimeMonitorModel{CustomBody: types.StringValue("")}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", CustomBody: ""}, state)
	if state.CustomBody.IsNull() {
		t.Error("an explicitly empty custom_body must stay an empty string, not become null")
	}

	state = &uptimeMonitorModel{CustomBody: types.StringNull()}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", CustomBody: ""}, state)
	if !state.CustomBody.IsNull() {
		t.Errorf("an unset custom_body must be null, got %q", state.CustomBody.ValueString())
	}

	state = &uptimeMonitorModel{CustomBody: types.StringNull()}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", CustomBody: `{"ping":true}`}, state)
	if state.CustomBody.ValueString() != `{"ping":true}` {
		t.Errorf(`expected {"ping":true}, got %q`, state.CustomBody.ValueString())
	}
}

func TestMapUptimeMonitorToState_ProbeRegionIDsPinnedEmpty(t *testing.T) {
	// Sending [] is only half of clearable; the read side has to preserve it too,
	// or the plan re-proposes [] and every apply fails.
	state := &uptimeMonitorModel{ProbeRegionIDs: types.ListValueMust(types.Int64Type, []attr.Value{})}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid"}, state)
	if state.ProbeRegionIDs.IsNull() {
		t.Error("an explicitly empty probe_region_ids must stay [], not become null")
	}

	state = &uptimeMonitorModel{ProbeRegionIDs: types.ListNull(types.Int64Type)}
	mapUptimeMonitor(t, &client.UptimeMonitor{ID: "mon-uuid", ProbeRegionIDs: []int64{1, 2}}, state)
	if len(state.ProbeRegionIDs.Elements()) != 2 {
		t.Errorf("expected 2 regions, got %v", state.ProbeRegionIDs.Elements())
	}
}

// --- normalizeScopes ---

func TestNormalizeScopes(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		// The API folds "read" into every token, so the provider has to plan the
		// same set the server will store.
		{"write implies read", []string{"write"}, []string{"read", "write"}},
		{"nil is read-only", nil, []string{"read"}},
		{"empty is read-only", []string{}, []string{"read"}},
		{"already normalized", []string{"read", "write"}, []string{"read", "write"}},
		// The server returns ["write", "read"] for a write token; sorting is what
		// keeps state and plan holding the same rendering.
		{"api ordering is sorted", []string{"write", "read"}, []string{"read", "write"}},
		{"case and padding", []string{" WRITE ", "Read"}, []string{"read", "write"}},
		{"duplicates collapse", []string{"write", "write", "read"}, []string{"read", "write"}},
		{"blanks dropped", []string{"", "write"}, []string{"read", "write"}},
		// The vocabulary has two entries today, and folding the floor in first
		// happens to leave those two already ordered — so this hypothetical third
		// scope is the only input that can tell a sorted result from an
		// insertion-ordered one. It pins the contract the resource relies on:
		// state and plan must hold the SAME rendering of the same set, whatever
		// order the API sends and whatever order the configuration is written in.
		{"deterministic order beyond the current vocabulary",
			[]string{"write", "admin"}, []string{"admin", "read", "write"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeScopes(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalizeScopes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- tokenMatchesPrefix ---

func TestTokenMatchesPrefix(t *testing.T) {
	tests := []struct {
		apiKey, prefix string
		want           bool
	}{
		{"fn_a1b2c3d4e5f6", "fn_a1b2c", true},
		{"fn_a1b2c3d4e5f6", "fn_9999f", false},
		{"", "fn_a1b2c", false},
		{"fn_a1b2c3d4e5f6", "", false},
		{"fn_a1b2c", "fn_a1b2c", true},
	}

	for _, tt := range tests {
		if got := tokenMatchesPrefix(tt.apiKey, tt.prefix); got != tt.want {
			t.Errorf("tokenMatchesPrefix(%q, %q) = %v, want %v", tt.apiKey, tt.prefix, got, tt.want)
		}
	}
}

// --- validateExpiresAt ---

func TestValidateExpiresAt(t *testing.T) {
	valid := []string{
		"2026-12-01",
		"2026-12-01T00:00:00Z",
		"2026-12-01T00:00:00+01:00",
		"2026-12-01T00:00Z",
		// A zero fraction names the instant the API will echo, so it round-trips.
		"2026-12-01T00:00:00.000Z",
	}
	for _, v := range valid {
		if diags := validateExpiresAt(types.StringValue(v)); diags.HasError() {
			t.Errorf("expected %q to be accepted, got %v", v, diags.Errors())
		}
	}

	for _, v := range []string{
		"next friday", "01/12/2026", "",
		// The serializer renders expires_at with no fractional digits, so a
		// sub-second value cannot survive the round trip: accepted here it would
		// fail the apply with "inconsistent result after apply".
		"2026-12-01T00:00:00.500Z",
	} {
		if diags := validateExpiresAt(types.StringValue(v)); !diags.HasError() {
			t.Errorf("expected %q to be rejected", v)
		}
	}

	// Unknown only resolves at apply time; null means "never expires".
	if diags := validateExpiresAt(types.StringNull()); diags.HasError() {
		t.Errorf("expected null to be accepted, got %v", diags.Errors())
	}
	if diags := validateExpiresAt(types.StringUnknown()); diags.HasError() {
		t.Errorf("expected unknown to be deferred, got %v", diags.Errors())
	}
}

// --- mapAPITokenToState ---

func TestMapAPITokenToState(t *testing.T) {
	lastUsed := "2026-09-01T11:00:00Z"
	token := &client.APIToken{
		ID:          7,
		Name:        "CI deploy key",
		TokenPrefix: "fn_a1b2c",
		Scopes:      []string{"write", "read"},
		LastUsedAt:  &lastUsed,
		Active:      true,
		CreatedAt:   "2026-09-01T10:00:00Z",
		UpdatedAt:   "2026-09-01T10:00:00Z",
		Token:       "fn_a1b2c3d4e5f6",
	}

	state := &apiTokenModel{}
	if diags := mapAPITokenToState(context.Background(), token, state); diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags.Errors())
	}

	if state.ID.ValueInt64() != 7 {
		t.Errorf("expected id 7, got %d", state.ID.ValueInt64())
	}
	if state.Token.ValueString() != "fn_a1b2c3d4e5f6" {
		t.Errorf("expected the create value to be stored, got %q", state.Token.ValueString())
	}
	if state.TokenPrefix.ValueString() != "fn_a1b2c" {
		t.Errorf("expected prefix fn_a1b2c, got %q", state.TokenPrefix.ValueString())
	}
	if !state.Active.ValueBool() {
		t.Error("expected active true")
	}
	if state.LastUsedAt.ValueString() != lastUsed {
		t.Errorf("expected last_used_at %q, got %q", lastUsed, state.LastUsedAt.ValueString())
	}
	if !state.ExpiresAt.IsNull() {
		t.Error("expected expires_at to be null when the token never expires")
	}
	if state.AllowSelfRevoke.ValueBool() {
		t.Error("expected allow_self_revoke to default to false")
	}

	var scopes []string
	state.Scopes.ElementsAs(context.Background(), &scopes, false)
	if !reflect.DeepEqual(scopes, []string{"read", "write"}) {
		t.Errorf("expected sorted scopes, got %q", scopes)
	}
}

// The stale-expiry branch. A zero-value model's expires_at is ALREADY null, so
// the assertion in TestMapAPITokenToState cannot see this assignment; seeding a
// known prior value is what makes it observable.
func TestMapAPITokenToState_ClearsAnExpiryTheAPINoLongerReports(t *testing.T) {
	token := &client.APIToken{ID: 1, Scopes: []string{"read"}} // ExpiresAt nil

	state := &apiTokenModel{ExpiresAt: types.StringValue("2026-12-01")}
	mapAPITokenToState(context.Background(), token, state)

	if !state.ExpiresAt.IsNull() {
		t.Errorf("expected the stale expiry to be cleared, got %q", state.ExpiresAt.ValueString())
	}
}

// The API re-renders expires_at in its own ISO 8601 form. Storing that instead
// of what the practitioner wrote fails Terraform's "inconsistent result after
// apply" check, and proposes replacing a live credential on every plan after.
func TestMapAPITokenToState_KeepsConfiguredExpiryRendering(t *testing.T) {
	apiValue := "2026-12-01T00:00:00Z"
	token := &client.APIToken{ID: 1, Scopes: []string{"read"}, ExpiresAt: &apiValue}

	state := &apiTokenModel{ExpiresAt: types.StringValue("2026-12-01")}
	mapAPITokenToState(context.Background(), token, state)

	if state.ExpiresAt.ValueString() != "2026-12-01" {
		t.Errorf("expected the configured rendering to survive, got %q", state.ExpiresAt.ValueString())
	}
}

func TestMapAPITokenToState_TakesAPIExpiryWhenItDiffers(t *testing.T) {
	apiValue := "2027-01-01T00:00:00Z"
	token := &client.APIToken{ID: 1, Scopes: []string{"read"}, ExpiresAt: &apiValue}

	state := &apiTokenModel{ExpiresAt: types.StringValue("2026-12-01")}
	mapAPITokenToState(context.Background(), token, state)

	if state.ExpiresAt.ValueString() != apiValue {
		t.Errorf("expected drift to surface as %q, got %q", apiValue, state.ExpiresAt.ValueString())
	}
}

// Read never carries a token value — only create does — so it must not wipe the
// one already in state.
func TestMapAPITokenToState_PreservesStoredTokenOnRead(t *testing.T) {
	token := &client.APIToken{ID: 1, Scopes: []string{"read"}}

	state := &apiTokenModel{Token: types.StringValue("fn_a1b2c3d4e5f6")}
	mapAPITokenToState(context.Background(), token, state)

	if state.Token.ValueString() != "fn_a1b2c3d4e5f6" {
		t.Errorf("expected the stored value to survive a read, got %q", state.Token.ValueString())
	}
}

// An imported token has no value anywhere to read back, so the unknown has to
// resolve to null rather than being left dangling.
func TestMapAPITokenToState_UnknownTokenBecomesNull(t *testing.T) {
	token := &client.APIToken{ID: 1, Scopes: []string{"read"}}

	state := &apiTokenModel{Token: types.StringUnknown()}
	mapAPITokenToState(context.Background(), token, state)

	if !state.Token.IsNull() {
		t.Errorf("expected null, got %v", state.Token)
	}
}

// --- retainEquivalentScopesModifier ---

// The whole point of the modifier: a config of ["write"] and a state of
// ["read", "write"] describe one token, so the plan has to keep the prior value.
// Otherwise scopes — which forces replacement — diverges on every refresh and
// proposes destroying a live credential to mint an identical one.
func TestRetainEquivalentScopesModifier(t *testing.T) {
	ctx := context.Background()

	prior, _ := types.SetValueFrom(ctx, types.StringType, []string{"read", "write"})
	planned, _ := types.SetValueFrom(ctx, types.StringType, []string{"write"})

	resp := &planmodifier.SetResponse{PlanValue: planned}
	retainEquivalentScopesModifier{}.PlanModifySet(ctx, planmodifier.SetRequest{
		StateValue: prior,
		PlanValue:  planned,
	}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}

	if !resp.PlanValue.Equal(prior) {
		t.Errorf("expected the prior value to be retained, got %v", resp.PlanValue)
	}
}

// A real narrowing has to survive, so that it plans the replacement it needs.
func TestRetainEquivalentScopesModifier_KeepsGenuineChange(t *testing.T) {
	ctx := context.Background()

	prior, _ := types.SetValueFrom(ctx, types.StringType, []string{"read", "write"})
	planned, _ := types.SetValueFrom(ctx, types.StringType, []string{"read"})

	resp := &planmodifier.SetResponse{PlanValue: planned}
	retainEquivalentScopesModifier{}.PlanModifySet(ctx, planmodifier.SetRequest{
		StateValue: prior,
		PlanValue:  planned,
	}, resp)

	if !resp.PlanValue.Equal(planned) {
		t.Errorf("expected the planned value to stand, got %v", resp.PlanValue)
	}
}

// On create there is no prior value to retain, and Terraform forbids planning
// anything but the config value there.
func TestRetainEquivalentScopesModifier_LeavesNullAndUnknown(t *testing.T) {
	ctx := context.Background()

	planned, _ := types.SetValueFrom(ctx, types.StringType, []string{"write"})

	for name, prior := range map[string]types.Set{
		"null state":    types.SetNull(types.StringType),
		"unknown state": types.SetUnknown(types.StringType),
	} {
		t.Run(name, func(t *testing.T) {
			resp := &planmodifier.SetResponse{PlanValue: planned}
			retainEquivalentScopesModifier{}.PlanModifySet(ctx, planmodifier.SetRequest{
				StateValue: prior,
				PlanValue:  planned,
			}, resp)
			if !resp.PlanValue.Equal(planned) {
				t.Errorf("expected the config value to stand, got %v", resp.PlanValue)
			}
		})
	}
}

// --- preserveScopes ---

// Create plans ["write"] and the API answers ["read", "write"]. Storing the
// API's rendering would fail Terraform's "inconsistent result after apply"
// check, so the planned one has to survive.
func TestMapAPITokenToState_KeepsConfiguredScopeRendering(t *testing.T) {
	ctx := context.Background()
	token := &client.APIToken{ID: 1, Scopes: []string{"write", "read"}}

	configured, _ := types.SetValueFrom(ctx, types.StringType, []string{"write"})
	state := &apiTokenModel{Scopes: configured}
	mapAPITokenToState(ctx, token, state)

	var got []string
	state.Scopes.ElementsAs(ctx, &got, false)
	if !reflect.DeepEqual(got, []string{"write"}) {
		t.Errorf("expected the configured rendering to survive, got %q", got)
	}
}

// Scopes that genuinely differ are drift and belong in state as the API has them.
func TestMapAPITokenToState_TakesAPIScopesWhenTheyDiffer(t *testing.T) {
	ctx := context.Background()
	token := &client.APIToken{ID: 1, Scopes: []string{"read"}}

	configured, _ := types.SetValueFrom(ctx, types.StringType, []string{"write"})
	state := &apiTokenModel{Scopes: configured}
	mapAPITokenToState(ctx, token, state)

	var got []string
	state.Scopes.ElementsAs(ctx, &got, false)
	if !reflect.DeepEqual(got, []string{"read"}) {
		t.Errorf("expected the API scopes to surface, got %q", got)
	}
}

// --- retainPriorModifier ---

// A never-used token has a null last_used_at, which UseStateForUnknown declines
// to retain — leaving it "known after apply" on an update that touches nothing.
func TestRetainPriorModifier_RetainsNullPriorValue(t *testing.T) {
	resp := &planmodifier.StringResponse{PlanValue: types.StringUnknown()}
	retainPriorModifier{}.PlanModifyString(context.Background(), planmodifier.StringRequest{
		State:      tfsdk.State{Raw: knownObject()},
		Plan:       tfsdk.Plan{Raw: knownObject()},
		StateValue: types.StringNull(),
		PlanValue:  types.StringUnknown(),
	}, resp)

	if !resp.PlanValue.IsNull() {
		t.Errorf("expected the null prior value to be retained, got %v", resp.PlanValue)
	}
}

// Create — and the create half of a replacement, which Terraform re-plans
// against a null prior state — has to keep its unknown, or the new token would
// be planned as the old one's value.
func TestRetainPriorModifier_LeavesCreateUnknown(t *testing.T) {
	resp := &planmodifier.StringResponse{PlanValue: types.StringUnknown()}
	retainPriorModifier{}.PlanModifyString(context.Background(), planmodifier.StringRequest{
		State:      tfsdk.State{Raw: nullObject()},
		Plan:       tfsdk.Plan{Raw: knownObject()},
		StateValue: types.StringNull(),
		PlanValue:  types.StringUnknown(),
	}, resp)

	if !resp.PlanValue.IsUnknown() {
		t.Errorf("expected unknown to stand on create, got %v", resp.PlanValue)
	}
}

func TestRetainPriorModifier_LeavesKnownPlanAlone(t *testing.T) {
	resp := &planmodifier.StringResponse{PlanValue: types.StringValue("planned")}
	retainPriorModifier{}.PlanModifyString(context.Background(), planmodifier.StringRequest{
		State:      tfsdk.State{Raw: knownObject()},
		Plan:       tfsdk.Plan{Raw: knownObject()},
		StateValue: types.StringValue("prior"),
		PlanValue:  types.StringValue("planned"),
	}, resp)

	if resp.PlanValue.ValueString() != "planned" {
		t.Errorf("expected the known plan value to stand, got %v", resp.PlanValue)
	}
}

func knownObject() tftypes.Value {
	return tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, map[string]tftypes.Value{})
}

func nullObject() tftypes.Value {
	return tftypes.NewValue(tftypes.Object{AttributeTypes: map[string]tftypes.Type{}}, nil)
}

// --- apiTokenResource.Delete ---

// apiTokenState builds a state object holding one token, so the Delete tests can
// drive the real method rather than the guard predicate underneath it.
func apiTokenState(t *testing.T, prefix string, allowSelfRevoke bool) tfsdk.State {
	t.Helper()
	ctx := context.Background()

	schemaResp := &resource.SchemaResponse{}
	(&apiTokenResource{}).Schema(ctx, resource.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("building schema: %v", schemaResp.Diagnostics.Errors())
	}

	scopes, diags := types.SetValueFrom(ctx, types.StringType, []string{"read", "write"})
	if diags.HasError() {
		t.Fatalf("building scopes: %v", diags.Errors())
	}

	state := tfsdk.State{Schema: schemaResp.Schema, Raw: tftypes.Value{}}
	diags = state.Set(ctx, apiTokenModel{
		ID:              types.Int64Value(7),
		Name:            types.StringValue("CI deploy key"),
		Scopes:          scopes,
		ExpiresAt:       types.StringNull(),
		AllowSelfRevoke: types.BoolValue(allowSelfRevoke),
		Token:           types.StringNull(),
		TokenPrefix:     types.StringValue(prefix),
		Active:          types.BoolValue(true),
		LastUsedAt:      types.StringNull(),
		CreatedAt:       types.StringValue("2026-01-01T00:00:00Z"),
		UpdatedAt:       types.StringValue("2026-01-01T00:00:00Z"),
	})
	if diags.HasError() {
		t.Fatalf("populating state: %v", diags.Errors())
	}
	return state
}

// Destroying the credential the provider is authenticated with locks Terraform
// out of the API, so it is refused. The client points at an unroutable address:
// if the guard ever stops firing, the test fails on a connection error rather
// than passing quietly.
func TestAPITokenDelete_RefusesSelfRevocation(t *testing.T) {
	ctx := context.Background()
	r := &apiTokenResource{client: client.NewClient("http://127.0.0.1:1", "fn_00001deadbeef")}

	state := apiTokenState(t, "fn_00001", false)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the destroy to be refused")
	}
	summary := resp.Diagnostics.Errors()[0].Summary()
	if !strings.Contains(summary, "Refusing to revoke") {
		t.Errorf("expected the self-revocation refusal, got %q", summary)
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "allow_self_revoke") {
		t.Errorf("the refusal must name its escape hatch, got %q", detail)
	}
}

// allow_self_revoke is the deliberate case — a leaked credential you want gone
// even though it is the one in use.
func TestAPITokenDelete_AllowSelfRevokeOverridesTheGuard(t *testing.T) {
	ctx := context.Background()
	var revoked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		revoked = req.Method == http.MethodDelete && req.URL.Path == "/api/v1/api_tokens/7"
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_token": map[string]interface{}{"id": 7, "revoked_at": "2026-01-02T00:00:00Z", "active": false},
		})
	}))
	defer srv.Close()

	r := &apiTokenResource{client: client.NewClient(srv.URL, "fn_00001deadbeef")}
	state := apiTokenState(t, "fn_00001", true)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !revoked {
		t.Error("expected the token to be revoked once the override is set")
	}
}

// The guard must not stand between a practitioner and every other token: only a
// prefix that matches the key in use is the one that locks Terraform out.
func TestAPITokenDelete_RevokesATokenItIsNotUsing(t *testing.T) {
	ctx := context.Background()
	var revoked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		revoked = req.Method == http.MethodDelete
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_token": map[string]interface{}{"id": 7, "revoked_at": "2026-01-02T00:00:00Z", "active": false},
		})
	}))
	defer srv.Close()

	r := &apiTokenResource{client: client.NewClient(srv.URL, "fn_99999deadbeef")}
	state := apiTokenState(t, "fn_00001", false)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics.Errors())
	}
	if !revoked {
		t.Error("expected a token the provider is not using to be revoked")
	}
}

// A revoke the API refuses is not a destroy that succeeded. Swallowing it drops
// a live credential out of state — valid, unmanaged, and invisible.
func TestAPITokenDelete_SurfacesAServerError(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing write scope"})
	}))
	defer srv.Close()

	r := &apiTokenResource{client: client.NewClient(srv.URL, "fn_99999deadbeef")}
	state := apiTokenState(t, "fn_00001", false)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("a refused revoke must raise: the token is still live")
	}
}

// S-1: an unreadable prefix means the provider cannot tell whether this is its
// own credential, and it refuses rather than guessing.
func TestAPITokenDelete_RefusesWhenThePrefixIsMissing(t *testing.T) {
	ctx := context.Background()
	r := &apiTokenResource{client: client.NewClient("http://127.0.0.1:1", "fn_00001deadbeef")}

	state := apiTokenState(t, "", false)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the destroy to be refused when the token cannot be identified")
	}
	if summary := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "Cannot tell whether") {
		t.Errorf("expected the cannot-identify refusal, got %q", summary)
	}
}

// C5: 8 characters is `fn_` plus 5 hex, so two unrelated tokens can share a
// prefix. When Terraform holds the value it knows exactly, and a colliding
// prefix must not block revoking a token that is demonstrably not the one in use.
func TestAPITokenDelete_ExactValueBeatsACollidingPrefix(t *testing.T) {
	ctx := context.Background()
	var revoked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		revoked = req.Method == http.MethodDelete
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_token": map[string]interface{}{"id": 7, "revoked_at": "2026-01-02T00:00:00Z", "active": false},
		})
	}))
	defer srv.Close()

	// Same published prefix, different token.
	r := &apiTokenResource{client: client.NewClient(srv.URL, "fn_00001aaaa")}
	state := apiTokenState(t, "fn_00001", false)
	if diags := state.SetAttribute(ctx, path.Root("token"), types.StringValue("fn_00001bbbb")); diags.HasError() {
		t.Fatalf("seeding the value: %v", diags.Errors())
	}

	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a token the provider demonstrably is not using must be revocable: %v", resp.Diagnostics.Errors())
	}
	if !revoked {
		t.Error("expected the revoke to be issued")
	}
}

// And the converse: the value matching is the strongest possible proof that
// this IS the credential in use.
func TestAPITokenDelete_ExactValueRefusesSelfRevocation(t *testing.T) {
	ctx := context.Background()
	r := &apiTokenResource{client: client.NewClient("http://127.0.0.1:1", "fn_00001bbbb")}

	// A prefix that does NOT match, so only the value can catch this.
	state := apiTokenState(t, "fn_99999", false)
	if diags := state.SetAttribute(ctx, path.Root("token"), types.StringValue("fn_00001bbbb")); diags.HasError() {
		t.Fatalf("seeding the value: %v", diags.Errors())
	}

	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected the destroy to be refused")
	}
	if summary := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "Refusing to revoke") {
		t.Errorf("expected the self-revocation refusal, got %q", summary)
	}
}

// T3: the Update guard is the tripwire for a dropped RequiresReplace. Arm it.
func TestAPITokenUpdate_RefusesAnImmutableChange(t *testing.T) {
	ctx := context.Background()
	state := apiTokenState(t, "fn_00001", false)

	var planModel apiTokenModel
	if diags := state.Get(ctx, &planModel); diags.HasError() {
		t.Fatalf("reading state: %v", diags.Errors())
	}
	planModel.Name = types.StringValue("renamed")

	plan := tfsdk.Plan{Schema: state.Schema, Raw: state.Raw}
	if diags := plan.Set(ctx, planModel); diags.HasError() {
		t.Fatalf("building plan: %v", diags.Errors())
	}

	resp := &resource.UpdateResponse{State: state}
	(&apiTokenResource{}).Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("an immutable change reaching Update must raise, not be written to state")
	}
	if summary := resp.Diagnostics.Errors()[0].Summary(); !strings.Contains(summary, "cannot be updated in place") {
		t.Errorf("expected the immutability refusal, got %q", summary)
	}
}

// A token already gone from the API is not an error to destroy — the desired
// end state is the one that already holds.
func TestAPITokenDelete_TreatsMissingTokenAsGone(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	r := &apiTokenResource{client: client.NewClient(srv.URL, "fn_99999deadbeef")}
	state := apiTokenState(t, "fn_00001", false)
	resp := &resource.DeleteResponse{State: state}
	r.Delete(ctx, resource.DeleteRequest{State: state}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("a 404 on revoke is success, got %v", resp.Diagnostics.Errors())
	}
}

// The trimmed validator mirrors Ruby String#strip — ASCII whitespace plus
// vertical tab and NUL, the two Go's \s misses. A padded value would apply,
// read back stripped, and fail as an inconsistent result; internal
// whitespace is untouched by the server and passes.
func TestInstanceTrimmedValidator(t *testing.T) {
	ctx := context.Background()
	s := instanceSchemaForTest(t)
	a, ok := s.Attributes["tsdb_url"].(rschema.StringAttribute)
	if !ok || len(a.Validators) == 0 {
		t.Fatal("tsdb_url must carry the trimmed validator — the server strips it on assignment")
	}

	probe := func(value string) bool {
		resp := &validator.StringResponse{}
		a.Validators[0].ValidateString(ctx, validator.StringRequest{
			Path:        path.Root("tsdb_url"),
			ConfigValue: types.StringValue(value),
		}, resp)
		return resp.Diagnostics.HasError()
	}

	for _, ok := range []string{"", "http://x", "http://x/a b", "a\nb"} {
		if probe(ok) {
			t.Errorf("expected %q to pass — the server would store it unchanged", ok)
		}
	}
	for _, padded := range []string{" http://x", "http://x ", "\vhttp://x", "http://x\x00", "\thttp://x\n"} {
		if !probe(padded) {
			t.Errorf("expected %q to be rejected — the server strips it and the read-back would differ", padded)
		}
	}
}
