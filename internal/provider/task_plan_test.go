package provider_test

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// #8's headline claim is that schedule_type updates IN PLACE: the API permits it
// on PATCH, so the RequiresReplace plan modifier that used to guard it is gone.
// Only a plan test can hold that line. The unit tests drive Create and Update
// directly, so a RequiresReplace reintroduced on the attribute would leave every
// one of them green while every practitioner switching a task from cron to
// interval silently destroys it — and a task replacement issues a NEW ping_key,
// so every deployed job keeps pinging a URL that no longer exists.

// taskAPI is a fake tasks endpoint that remembers the last PATCH body, so the
// tests can assert both the plan Terraform produced and the request it caused.
type taskAPI struct {
	mu sync.Mutex

	name            string
	scheduleType    string
	schedule        interface{}
	intervalSeconds interface{}
	status          string

	patches []map[string]interface{}
	deletes int
}

func newTaskAPI(scheduleType string, schedule interface{}, intervalSeconds interface{}) *taskAPI {
	return &taskAPI{
		name:            "nightly backup",
		scheduleType:    scheduleType,
		schedule:        schedule,
		intervalSeconds: intervalSeconds,
		status:          "active",
	}
}

// monitoringStatus mirrors Task#monitoring_status server-side: it is DERIVED, not
// stored, and `paused` there is just the lifecycle column showing through. A fake
// that reports a fixed value would let the resource read a health it cannot get.
func (a *taskAPI) monitoringStatus() string {
	if a.status == "paused" {
		return "paused"
	}
	return "ok"
}

func (a *taskAPI) body() map[string]interface{} {
	return map[string]interface{}{"task": map[string]interface{}{
		"id": "3cac0e44-0000-4000-8000-000000000001", "name": a.name,
		"schedule_type": a.scheduleType, "schedule": a.schedule,
		"interval_seconds": a.intervalSeconds, "time_zone": "UTC",
		"grace_period_minutes": 5, "status": a.status, "monitoring_status": a.monitoringStatus(),
		"ping_key": "pk_live_abc", "ping_url": "https://ping.fivenines.io/pk_live_abc",
		"host_id": nil, "expected_ping_at": "2026-01-02T03:00:00Z", "last_ping_at": nil,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
	}}
}

func (a *taskAPI) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case r.Method == http.MethodDelete:
		a.deletes++
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause"):
		a.status = "paused"
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resume"):
		a.status = "active"
	case r.Method == http.MethodPatch:
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Task map[string]interface{} `json:"task"`
		}
		json.Unmarshal(raw, &payload)
		a.patches = append(a.patches, payload.Task)
		if v, ok := payload.Task["name"].(string); ok {
			a.name = v
		}
		if v, ok := payload.Task["schedule_type"].(string); ok {
			a.scheduleType = v
		}
		if v, ok := payload.Task["schedule"]; ok {
			a.schedule = v
		}
		if v, ok := payload.Task["interval_seconds"]; ok {
			a.intervalSeconds = v
		}
	case r.Method == http.MethodPost:
		w.Header().Set("ETag", `"task"`)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(a.body())
		return
	}

	w.Header().Set("ETag", `"task"`)
	json.NewEncoder(w).Encode(a.body())
}

// replaced reports whether the task was destroyed mid-run. Every test case ends
// with a teardown destroy, so exactly one DELETE is the clean outcome and a
// second one is the replacement this resource must not plan.
func (a *taskAPI) replaced() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deletes > 1
}

func (a *taskAPI) lastPatch() map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.patches) == 0 {
		return nil
	}
	return a.patches[len(a.patches)-1]
}

// Switching cron -> interval must plan an Update, reach the API as a PATCH
// carrying the new schedule_type, and never destroy the task.
func TestTaskPlan_ScheduleTypeSwitchesInPlace(t *testing.T) {
	api := newTaskAPI("cron", "0 3 * * *", nil)
	planTest(t, api.handler)

	cron := providerConfig + `
resource "fivenines_task" "test" {
  name          = "nightly backup"
  schedule_type = "cron"
  schedule      = "0 3 * * *"
}`
	interval := providerConfig + `
resource "fivenines_task" "test" {
  name             = "nightly backup"
  schedule_type    = "interval"
  interval_seconds = 3600
}`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cron,
				Check:  resource.TestCheckResourceAttr("fivenines_task.test", "schedule_type", "cron"),
			},
			{
				Config: interval,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_task.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "schedule_type", "interval"),
					resource.TestCheckResourceAttr("fivenines_task.test", "interval_seconds", "3600"),
					// The ping key survives an in-place switch. Under the old
					// RequiresReplace it would not have, which is the whole cost
					// of getting this wrong.
					resource.TestCheckResourceAttr("fivenines_task.test", "ping_key", "pk_live_abc"),
				),
			},
		},
	})

	if api.replaced() {
		t.Error("switching schedule_type destroyed the task instead of updating it in place")
	}
	patch := api.lastPatch()
	if patch == nil {
		t.Fatal("expected the switch to reach the API as a PATCH")
	}
	if got := patch["schedule_type"]; got != "interval" {
		t.Errorf("expected schedule_type=interval in the PATCH body, got %v", got)
	}
	if got := patch["interval_seconds"]; got != float64(3600) {
		t.Errorf("expected interval_seconds=3600 in the PATCH body, got %v", got)
	}
	// schedule keeps its omitempty: it is Optional+Computed, and the API holds on
	// to the cron expression it already stored. Sending an explicit null here
	// would clear a value the practitioner only stopped mentioning.
	if _, ok := patch["schedule"]; ok {
		t.Errorf("expected schedule to be omitted once it is unset, got %v", patch["schedule"])
	}
}

// The mirror image. #8's claim is bidirectional, and the two directions do not
// share a code path in the plan: interval -> cron drops interval_seconds and
// supplies schedule, so a RequiresReplace or an omitempty mistake could bite one
// direction and not the other.
func TestTaskPlan_IntervalToCronSwitchesInPlace(t *testing.T) {
	api := newTaskAPI("interval", nil, float64(3600))
	planTest(t, api.handler)

	interval := providerConfig + `
resource "fivenines_task" "test" {
  name             = "nightly backup"
  schedule_type    = "interval"
  interval_seconds = 3600
}`
	cron := providerConfig + `
resource "fivenines_task" "test" {
  name          = "nightly backup"
  schedule_type = "cron"
  schedule      = "0 3 * * *"
}`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: interval,
				Check:  resource.TestCheckResourceAttr("fivenines_task.test", "interval_seconds", "3600"),
			},
			{
				Config: cron,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_task.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "schedule_type", "cron"),
					resource.TestCheckResourceAttr("fivenines_task.test", "schedule", "0 3 * * *"),
					resource.TestCheckResourceAttr("fivenines_task.test", "ping_key", "pk_live_abc"),
				),
			},
		},
	})

	if api.replaced() {
		t.Error("switching schedule_type destroyed the task instead of updating it in place")
	}
	patch := api.lastPatch()
	if patch == nil {
		t.Fatal("expected the switch to reach the API as a PATCH")
	}
	if got := patch["schedule_type"]; got != "cron" {
		t.Errorf("expected schedule_type=cron in the PATCH body, got %v", got)
	}
	if got := patch["schedule"]; got != "0 3 * * *" {
		t.Errorf("expected the cron expression in the PATCH body, got %v", got)
	}
	if _, ok := patch["interval_seconds"]; ok {
		t.Errorf("expected interval_seconds to be omitted once it is unset, got %v", patch["interval_seconds"])
	}
	// host_id carries NO omitempty, so dropping it from the config has to reach
	// the API as an explicit null that clears the association — an omitted key
	// would silently leave a stale host attached.
	if v, ok := patch["host_id"]; !ok || v != nil {
		t.Errorf("expected an explicit null host_id in the PATCH body, got %v (present=%v)", v, ok)
	}
}

// name is Required and NOT Computed, yet mapTaskToState writes the API's value
// back over it. That is the hazard TODOS.md files under "Required name
// attributes are overwritten from the API response", and it names tasks. So long
// as the server echoes the name it was sent, a rename is an ordinary in-place
// update — this pins that, and pins that it does not go through a replacement
// (which would issue a new ping_key for a change of label).
func TestTaskPlan_RenameInPlace(t *testing.T) {
	api := newTaskAPI("cron", "0 3 * * *", nil)
	planTest(t, api.handler)

	cfg := func(name string) string {
		return providerConfig + `
resource "fivenines_task" "test" {
  name          = "` + name + `"
  schedule_type = "cron"
  schedule      = "0 3 * * *"
}`
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("nightly backup"),
				Check:  resource.TestCheckResourceAttr("fivenines_task.test", "name", "nightly backup"),
			},
			{
				Config: cfg("nightly database backup"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_task.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "name", "nightly database backup"),
					resource.TestCheckResourceAttr("fivenines_task.test", "ping_key", "pk_live_abc"),
				),
			},
		},
	})

	if api.replaced() {
		t.Error("renaming the task destroyed it instead of updating it in place")
	}
	if got := api.lastPatch()["name"]; got != "nightly database backup" {
		t.Errorf("expected the new name in the PATCH body, got %v", got)
	}
}

// The cross-field validation #8 kept has to fire at PLAN time, before the API is
// called: a cron task with no schedule is a 422 the practitioner would otherwise
// only see mid-apply.
func TestTaskPlan_CronRequiresSchedule(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_task" "test" {
  name          = "nightly backup"
  schedule_type = "cron"
}`,
			ExpectError: regexp.MustCompile(`(?s)"schedule" is required when "schedule_type" is "cron"`),
		}},
	})
}

func TestTaskPlan_IntervalRequiresIntervalSeconds(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_task" "test" {
  name          = "nightly backup"
  schedule_type = "interval"
}`,
			ExpectError: regexp.MustCompile(`(?s)"interval_seconds" is required when "schedule_type" is "interval"`),
		}},
	})
}

// paused is Optional+Computed and driven by the pause/resume endpoints rather
// than by the PATCH body. Toggling it must plan an update and leave state
// agreeing with the status the API reports back.
func TestTaskPlan_PauseAndResumeInPlace(t *testing.T) {
	api := newTaskAPI("cron", "0 3 * * *", nil)
	planTest(t, api.handler)

	cfg := func(paused string) string {
		return providerConfig + `
resource "fivenines_task" "test" {
  name          = "nightly backup"
  schedule_type = "cron"
  schedule      = "0 3 * * *"
  paused        = ` + paused + `
}`
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "paused", "true"),
					resource.TestCheckResourceAttr("fivenines_task.test", "status", "paused"),
				),
			},
			{
				Config: cfg("false"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_task.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "paused", "false"),
					resource.TestCheckResourceAttr("fivenines_task.test", "status", "active"),
				),
			},
			// Back again. Update has two pause branches and they are separate
			// code: the step above only exercises resume, so pausing an
			// already-created task would otherwise never run offline.
			{
				Config: cfg("true"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("fivenines_task.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_task.test", "paused", "true"),
					resource.TestCheckResourceAttr("fivenines_task.test", "status", "paused"),
				),
			},
		},
	})

	if api.replaced() {
		t.Error("pausing or resuming destroyed the task instead of updating it in place")
	}
}
