package provider_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The filter arguments #21 added are enumerated server-side, and an unknown
// value is a 400 listing the accepted vocabulary. The OneOf validators move
// that rejection to plan time — but a validator is invisible to the unit tests,
// which drive Read directly and never let Terraform validate the config at all.
// These are the only tests that can tell a wired validator from an unwired one.

func incidentsPlanHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"incidents": []map[string]interface{}{{
			"id": 1, "title": "High CPU", "summary": "over 90%", "status": "open",
			"public":            true,
			"host_id":           "3cac0e44-0000-4000-8000-000000000001",
			"uptime_monitor_id": "3cac0e44-0000-4000-8000-000000000003",
			"workflow_id":       7,
			"duration_seconds":  3600,
			"created_at":        "2026-01-01T00:00:00Z",
			"updated_at":        "2026-01-01T00:00:00Z",
		}},
		"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
	})
}

// The filters have to survive the round trip into state: an argument that is
// silently dropped re-plans forever, and one that is not mapped at all reaches
// the API as an unfiltered list the practitioner never asked for.
func TestIncidentsDataSourcePlan_FiltersRoundTripAndFieldsMap(t *testing.T) {
	planTest(t, incidentsPlanHandler)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_incidents" "test" {
  status            = "open"
  q                 = "CPU"
  host_id           = "3cac0e44-0000-4000-8000-000000000001"
  uptime_monitor_id = "3cac0e44-0000-4000-8000-000000000003"
  workflow_id       = 7
  from              = "2026-08-29T00:00:00Z"
  to                = "2026-08-30T00:00:00Z"
  order             = "updated_at"
  direction         = "asc"
}

output "public_titles" {
  value = join(",", [for i in data.fivenines_incidents.test.incidents : i.title if i.public])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "status", "open"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "q", "CPU"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "workflow_id", "7"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "from", "2026-08-29T00:00:00Z"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "to", "2026-08-30T00:00:00Z"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "order", "updated_at"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "direction", "asc"),
				// The two fields #21 added to the response.
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "incidents.0.public", "true"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "incidents.0.uptime_monitor_id", "3cac0e44-0000-4000-8000-000000000003"),
				resource.TestCheckResourceAttr("data.fivenines_incidents.test", "incidents.0.duration_seconds", "3600"),
				resource.TestCheckOutput("public_titles", "High CPU"),
			),
		}},
	})
}

// The old schema documented "triggered, acknowledged, muted, resolved", which
// was never the API's vocabulary. A config written against that doc has to fail
// at plan time with the real values rather than 400 mid-apply.
func TestIncidentsDataSourcePlan_RejectsStaleStatusVocabulary(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_incidents" "test" {
  status = "triggered"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute status value must be one of.*"acknowledged"`),
		}},
	})
}

func TestIncidentsDataSourcePlan_RejectsInvalidOrderAtPlanTime(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_incidents" "test" {
  order = "startd_at"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute order value must be one of.*"title"`),
		}},
	})
}

// With no filters every Optional argument stays null rather than becoming an
// unknown Terraform refuses to converge.
func TestIncidentsDataSourcePlan_NoFilters(t *testing.T) {
	planTest(t, incidentsPlanHandler)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_incidents" "all" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_incidents.all", "incidents.#", "1"),
				resource.TestCheckNoResourceAttr("data.fivenines_incidents.all", "status"),
				resource.TestCheckNoResourceAttr("data.fivenines_incidents.all", "workflow_id"),
			),
		}},
	})
}

// A filter that matches nothing has to plan as an empty list. If it came back
// null the `for` expression below would fail at plan time, which is exactly the
// shape practitioners write now that filters exist.
func TestIncidentsDataSourcePlan_EmptyResultStillPlans(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"incidents": []map[string]interface{}{},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_incidents" "none" {
  status = "muted"
}

output "titles" {
  value = join(",", [for i in data.fivenines_incidents.none.incidents : i.title])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_incidents.none", "incidents.#", "0"),
				resource.TestCheckOutput("titles", ""),
			),
		}},
	})
}

// --- integrations ---

func integrationsPlanHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"integrations": []map[string]interface{}{{
			"id": 42, "type": "SlackIntegration", "name": "Ops", "provider": "Slack",
			"enabled": true, "verified": true,
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T00:00:00Z",
		}},
		"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
	})
}

func TestIntegrationsDataSourcePlan_FiltersRoundTrip(t *testing.T) {
	planTest(t, integrationsPlanHandler)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_integrations" "slack" {
  type      = "SlackIntegration"
  enabled   = true
  q         = "ops"
  order     = "type"
  direction = "asc"
}

output "channel_id" {
  value = one([for i in data.fivenines_integrations.slack.integrations : i.id if i.verified])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_integrations.slack", "type", "SlackIntegration"),
				resource.TestCheckResourceAttr("data.fivenines_integrations.slack", "enabled", "true"),
				resource.TestCheckResourceAttr("data.fivenines_integrations.slack", "order", "type"),
				resource.TestCheckResourceAttr("data.fivenines_integrations.slack", "integrations.0.updated_at", "2026-01-02T00:00:00Z"),
				resource.TestCheckOutput("channel_id", "42"),
			),
		}},
	})
}

// The type filter takes the backing class name, not the short create key. A
// config that sends "slack" has to be told so at plan time — the API's own 400
// arrives mid-apply and names no attribute.
func TestIntegrationsDataSourcePlan_RejectsShortCreateKeyForType(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_integrations" "test" {
  type = "slack"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute type value must be one of.*"SlackIntegration"`),
		}},
	})
}

// --- workflow runs ---

func workflowRunsPlanHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"runs": []map[string]interface{}{{
			"id": 10, "status": "failed", "resource_key": "web-1",
			"workflow_id": 3, "workflow_version_id": 9,
			"started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:01:00Z",
			"duration_seconds": 60,
			"created_at":       "2026-01-01T00:00:00Z",
			"updated_at":       "2026-01-01T00:01:00Z",
		}},
		"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
	})
}

func TestWorkflowRunsDataSourcePlan_FiltersRoundTripAndFieldsMap(t *testing.T) {
	planTest(t, workflowRunsPlanHandler)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_workflow_runs" "failures" {
  workflow_id = 3
  status      = "failed"
  order       = "completed_at"
  direction   = "asc"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_workflow_runs.failures", "status", "failed"),
				resource.TestCheckResourceAttr("data.fivenines_workflow_runs.failures", "order", "completed_at"),
				// The four fields #21 added to the run header.
				resource.TestCheckResourceAttr("data.fivenines_workflow_runs.failures", "runs.0.workflow_id", "3"),
				resource.TestCheckResourceAttr("data.fivenines_workflow_runs.failures", "runs.0.workflow_version_id", "9"),
				resource.TestCheckResourceAttr("data.fivenines_workflow_runs.failures", "runs.0.duration_seconds", "60"),
				resource.TestCheckResourceAttr("data.fivenines_workflow_runs.failures", "runs.0.updated_at", "2026-01-01T00:01:00Z"),
				resource.TestCheckResourceAttr("data.fivenines_workflow_runs.failures", "runs.0.resource_key", "web-1"),
			),
		}},
	})
}

// `pending` left the run status enum; a config carrying it has to fail at plan
// time rather than silently matching nothing.
func TestWorkflowRunsDataSourcePlan_RejectsRetiredPendingStatus(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_workflow_runs" "test" {
  workflow_id = 3
  status      = "pending"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute status value must be one of.*"cancelled"`),
		}},
	})
}

// --- workflow run (singular) ---

func TestWorkflowRunDataSourcePlan_ExposesStepDetail(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"run": map[string]interface{}{
				"id": 10, "status": "failed", "resource_key": nil,
				"workflow_id": 3, "workflow_version_id": 9,
				"started_at": "2026-01-01T00:00:00Z", "completed_at": "2026-01-01T00:01:00Z",
				"duration_seconds": 60,
				"created_at":       "2026-01-01T00:00:00Z",
				"updated_at":       "2026-01-01T00:01:00Z",
				"error":            "trigger raised",
				"trigger_output":   map[string]interface{}{"value": 91},
				"steps": []map[string]interface{}{{
					"id": 1, "node_id": "email_1", "node_type": "email_alert",
					"status": "failed", "error_message": "smtp timeout",
					"output_data":      map[string]interface{}{"delivered": false},
					"started_at":       "2026-01-01T00:00:30Z",
					"completed_at":     "2026-01-01T00:00:31Z",
					"duration_seconds": 1.25,
					"created_at":       "2026-01-01T00:00:30Z",
				}},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_workflow_run" "failure" {
  workflow_id = 3
  run_id      = 10
}

output "failed_step" {
  value = one([
    for s in data.fivenines_workflow_run.failure.steps :
    "${s.node_id}: ${s.error_message}" if s.status == "failed"
  ])
}

output "trigger_value" {
  value = tostring(jsondecode(data.fivenines_workflow_run.failure.trigger_output_json).value)
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_workflow_run.failure", "status", "failed"),
				resource.TestCheckResourceAttr("data.fivenines_workflow_run.failure", "error", "trigger raised"),
				resource.TestCheckResourceAttr("data.fivenines_workflow_run.failure", "steps.0.duration_seconds", "1.25"),
				// Null, not "": Terraform has to see the absence so a config can
				// tell a dispatch-once run from a fan-out over an empty key.
				resource.TestCheckNoResourceAttr("data.fivenines_workflow_run.failure", "resource_key"),
				// The point of the singular data source: naming the node that broke.
				resource.TestCheckOutput("failed_step", "email_1: smtp timeout"),
				// The JSON passthrough has to be decodable by jsondecode(), not
				// merely non-empty.
				resource.TestCheckOutput("trigger_value", "91"),
			),
		}},
	})
}
