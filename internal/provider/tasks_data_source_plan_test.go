package provider_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The tasks index enumerates status, schedule_type, order and direction
// server-side, and an unknown value is a 400 rather than an empty list. The
// OneOf validators move that rejection to plan time — and a validator is
// invisible to the unit tests, which drive Read directly and never let Terraform
// validate the config at all. These are the only tests that can tell a wired
// validator from an unwired one.

func tasksPlanHandler(gotQuery *url.Values) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		*gotQuery = r.URL.Query()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []map[string]interface{}{{
				"id": "3cac0e44-0000-4000-8000-000000000001", "name": "nightly backup",
				"schedule_type": "cron", "schedule": "0 3 * * *", "interval_seconds": nil,
				"time_zone": "Europe/Paris", "grace_period_minutes": 10,
				"status": "paused", "monitoring_status": "paused",
				"ping_key": "pk_live_abc", "ping_url": "https://ping.fivenines.io/pk_live_abc",
				"host_id": nil, "expected_ping_at": nil, "last_ping_at": nil,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			}},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	}
}

// The filters have to survive the round trip into state and reach the API: an
// argument that is silently dropped re-plans forever, and one that is not mapped
// at all returns an unfiltered fleet the practitioner never asked for.
func TestTasksDataSourcePlan_FiltersRoundTripAndFieldsMap(t *testing.T) {
	var gotQuery url.Values
	planTest(t, tasksPlanHandler(&gotQuery))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  status        = "paused"
  schedule_type = "cron"
  query         = "backup"
  updated_since = "2026-01-01T00:00:00Z"
  order         = "name"
  direction     = "asc"
}

output "paused_names" {
  value = join(",", [for t in data.fivenines_tasks.test.tasks : t.name if t.paused])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "status", "paused"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "schedule_type", "cron"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "query", "backup"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "updated_since", "2026-01-01T00:00:00Z"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "order", "name"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "direction", "asc"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "tasks.0.name", "nightly backup"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "tasks.0.schedule", "0 3 * * *"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "tasks.0.grace_period_minutes", "10"),
				// paused is derived from status, so a config can branch on it
				// the same way it does on fivenines_task.paused.
				resource.TestCheckResourceAttr("data.fivenines_tasks.test", "tasks.0.paused", "true"),
				// A null host_id has to read as absent, not as "".
				resource.TestCheckNoResourceAttr("data.fivenines_tasks.test", "tasks.0.host_id"),
				resource.TestCheckOutput("paused_names", "nightly backup"),
			),
		}},
	})

	// query is the schema's spelling of the API's `q`; the rename has to happen.
	for key, want := range map[string]string{
		"status": "paused", "schedule_type": "cron", "q": "backup",
		"updated_since": "2026-01-01T00:00:00Z", "order": "name", "direction": "asc",
	} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("expected %s=%q to reach the API, got %q", key, want, got)
		}
	}
}

// The task status vocabulary is active|paused. A config written against the
// uptime monitor's vocabulary ("down") has to fail at plan time with the real
// values rather than 400 mid-refresh.
func TestTasksDataSourcePlan_RejectsForeignStatusVocabulary(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  status = "down"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute status value must be one of.*"paused"`),
		}},
	})
}

func TestTasksDataSourcePlan_RejectsInvalidOrderAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  order = "last_ping_at"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute order value must be one of.*"name"`),
		}},
	})
}

// P1: the two remaining enumerated filters. Without a plan-tier rejection test
// their Validators block could be deleted with the whole suite green, which is
// exactly what this file exists to prevent.
func TestTasksDataSourcePlan_RejectsInvalidScheduleTypeAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  schedule_type = "heartbeat"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute schedule_type value must be one of.*"interval"`),
		}},
	})
}

func TestTasksDataSourcePlan_RejectsInvalidDirectionAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  direction = "descending"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute direction value must be one of.*"desc"`),
		}},
	})
}

// Zero matches is the normal case for a filtered read, and the empty list has to
// survive all the way into HCL: length()/for_each/toset over a null list fail a
// plan, so asserting [] at the framework-state level alone would not catch a
// regression that lets a null through.
func TestTasksDataSourcePlan_EmptyResultStillPlans(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []map[string]interface{}{},
			"meta":  map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "none" {
  status = "paused"
}

output "names" {
  value = join(",", [for t in data.fivenines_tasks.none.tasks : t.name])
}

output "count" {
  value = tostring(length(data.fivenines_tasks.none.tasks))
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_tasks.none", "tasks.#", "0"),
				resource.TestCheckOutput("names", ""),
				// length() over a null list is a plan error, so this output
				// resolving at all is the assertion.
				resource.TestCheckOutput("count", "0"),
			),
		}},
	})
}

// The shipped example filters on monitoring_status with a for-if. Three doc
// examples in this provider have already shipped broken because an HCL guard was
// written against a nullable attribute (HCL's && does not short-circuit, and
// for-if hard-errors on null), so the example's own expressions get planned here
// rather than trusted. It also pins the four-value vocabulary the server derives
// — ok | late | waiting | paused — which the fixtures previously got wrong.
func TestTasksDataSourcePlan_MonitoringStatusVocabularyDrivesExample(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		row := func(id, name, monitoring string) map[string]interface{} {
			return map[string]interface{}{
				"id": id, "name": name,
				"schedule_type": "cron", "schedule": "0 3 * * *", "interval_seconds": nil,
				"time_zone": "UTC", "grace_period_minutes": 5,
				"status": "active", "monitoring_status": monitoring,
				"ping_key": "pk_" + id, "ping_url": "https://ping.fivenines.io/pk_" + id,
				"host_id": nil, "expected_ping_at": nil, "last_ping_at": nil,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
			}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": []map[string]interface{}{
				row("t1", "healthy job", "ok"),
				row("t2", "stalled job", "late"),
				row("t3", "never started", "waiting"),
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 3, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "all_active" {
  status = "active"
}

output "late_task_names" {
  value = join(",", [
    for t in data.fivenines_tasks.all_active.tasks : t.name
    if t.monitoring_status == "late"
  ])
}

output "never_pinged_task_names" {
  value = join(",", [
    for t in data.fivenines_tasks.all_active.tasks : t.name
    if t.monitoring_status == "waiting"
  ])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// "late" and "waiting" are different problems: one stopped, the
				// other never started. An example that conflated them would hide
				// a job that was never wired up at all.
				resource.TestCheckOutput("late_task_names", "stalled job"),
				resource.TestCheckOutput("never_pinged_task_names", "never started"),
			),
		}},
	})
}

// limit carries an AtLeast(1) validator: 0 and negatives are not caps, and a
// silently-accepted 0 would read as "no matches" rather than "bad config".
func TestTasksDataSourcePlan_RejectsNonPositiveLimit(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  limit = 0
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute limit value must be at least 1`),
		}},
	})
}

// The cursor guard has to fire through REAL Terraform: a ValidateConfig the
// framework never calls is invisible to every unit test, and the failure it
// prevents is permanent silent row loss rather than a visible error.
func TestTasksDataSourcePlan_RejectsUnsafeCursorPagination(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  updated_since = "2026-01-01T00:00:00Z"
  limit         = 100
}`,
			ExpectError: regexp.MustCompile(`(?s)Unsafe cursor pagination.*"limit" cannot be combined with "updated_since"`),
		}},
	})
}

// Sorting ascending is the pairing that LOOKS safe, so pin that it is refused
// too — an inclusive cursor cannot be truncated safely at any sort order.
func TestTasksDataSourcePlan_RejectsBoundedCursorEvenWhenSortedAscending(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {
  updated_since = "2026-01-01T00:00:00Z"
  limit         = 100
  order         = "updated_at"
  direction     = "asc"
}`,
			ExpectError: regexp.MustCompile(`(?s)Unsafe cursor pagination.*cannot be combined`),
		}},
	})
}

// Each argument alone still plans, or the guard would have taken the feature with it.
func TestTasksDataSourcePlan_LimitAndCursorAreFineSeparately(t *testing.T) {
	var gotQuery url.Values
	planTest(t, tasksPlanHandler(&gotQuery))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "bounded" {
  limit     = 10
  order     = "created_at"
  direction = "desc"
}

data "fivenines_tasks" "incremental" {
  updated_since = "2026-01-01T00:00:00Z"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_tasks.bounded", "limit", "10"),
				resource.TestCheckResourceAttr("data.fivenines_tasks.incremental", "updated_since", "2026-01-01T00:00:00Z"),
			),
		}},
	})

	// limit is a client-side cap; the API has no such key and 400s on unknown ones.
	if _, ok := gotQuery["limit"]; ok {
		t.Error("limit must not be sent as a query parameter")
	}
}

// With no filters every Optional argument stays null rather than becoming an
// unknown Terraform refuses to converge, and nothing reaches the query string.
func TestTasksDataSourcePlan_NoFilters(t *testing.T) {
	var gotQuery url.Values
	planTest(t, tasksPlanHandler(&gotQuery))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_tasks" "test" {}

output "count" {
  value = tostring(length(data.fivenines_tasks.test.tasks))
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckNoResourceAttr("data.fivenines_tasks.test", "status"),
				resource.TestCheckNoResourceAttr("data.fivenines_tasks.test", "query"),
				resource.TestCheckNoResourceAttr("data.fivenines_tasks.test", "limit"),
				resource.TestCheckOutput("count", "1"),
			),
		}},
	})

	for _, key := range []string{"status", "schedule_type", "q", "updated_since", "order", "direction"} {
		if _, ok := gotQuery[key]; ok {
			t.Errorf("expected %s to be omitted when unset, got %q", key, gotQuery.Get(key))
		}
	}
}
