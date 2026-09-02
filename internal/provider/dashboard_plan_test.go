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
)

// The dashboard resources are the provider's first with three nested write
// shapes — `layout`, `targets` and `options` — and every one of them is
// Optional+Computed or carries a clear-on-null contract. That combination is
// exactly what unit tests cannot see: driving Create and Update directly skips
// the step where Terraform compares the applied result to the plan, and every
// mistake in this file's subject matter surfaces there rather than in a mapper.
//
//	Error: Provider produced inconsistent result after apply
//	.options: was null, but now cty.ObjectVal(...)
//
// So these drive REAL Terraform against a faithful fake API: one that applies
// the same write semantics the server documents (an option key sent as null is
// cleared, a target kind that is omitted is left alone, an omitted layout is
// placed by the grid) and answers in the same shape (all five target kinds
// always present, all eight option keys always present).

// fakeDashboardAPI is a small stateful stand-in for /api/v1/dashboards. It
// stores what it is told and renders the API's published shape back, which is
// what makes a round trip through it meaningful.
type fakeDashboardAPI struct {
	mu sync.Mutex

	// dedupeName reproduces the template endpoint's "PostgreSQL (2)" rename, the
	// one place the API answers with a name other than the one it was given.
	dedupeName bool

	name        string
	description interface{}

	sectionName      string
	sectionCollapsed bool
	sectionPosition  int64
	// sectionPositionOverride is what GET reports regardless of what was
	// written, standing in for a sibling's resequencing.
	sectionPositionOverride *int64
	sectionDeleted          bool

	panel map[string]interface{}
}

func newFakeDashboardAPI() *fakeDashboardAPI {
	return &fakeDashboardAPI{name: "Fleet health", sectionName: "Compute"}
}

func (f *fakeDashboardAPI) dashboardBody() map[string]interface{} {
	sections := []interface{}{}
	if !f.sectionDeleted {
		position := f.sectionPosition
		if f.sectionPositionOverride != nil {
			position = *f.sectionPositionOverride
		}
		sections = append(sections, map[string]interface{}{
			"id": 41, "name": f.sectionName, "position": position,
			"collapsed": f.sectionCollapsed,
		})
	}
	panels := []interface{}{}
	if f.panel != nil {
		panels = append(panels, f.panel)
	}
	return map[string]interface{}{
		"id": 12, "name": f.name, "description": f.description,
		"shared": false, "share_url": nil,
		"section_count": len(sections), "visualization_count": len(panels),
		"sections": sections, "visualizations": panels,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
	}
}

func (f *fakeDashboardAPI) sectionBody() map[string]interface{} {
	position := f.sectionPosition
	if f.sectionPositionOverride != nil {
		position = *f.sectionPositionOverride
	}
	return map[string]interface{}{
		"id": 41, "name": f.sectionName, "position": position,
		"collapsed": f.sectionCollapsed,
	}
}

// applyPanel is the faithful half: it applies the documented write semantics so
// a round trip actually tests them rather than echoing the request back whole.
func (f *fakeDashboardAPI) applyPanel(body map[string]interface{}, create bool) {
	if create || f.panel == nil {
		f.panel = map[string]interface{}{
			"id": 88, "title": nil, "description": nil, "metric": "cpu_usage",
			"target_kind": "hosts", "chart_type": "line", "section": nil,
			"layout": map[string]interface{}{"x": int64(0), "y": int64(0), "w": int64(12), "h": int64(6)},
			"targets": map[string]interface{}{
				"hosts": []interface{}{}, "uptime_monitors": []interface{}{},
				"tasks": []interface{}{}, "network_devices": []interface{}{},
				"ceph_clusters": []interface{}{},
			},
			"options": map[string]interface{}{
				"reducer": nil, "group_by": nil, "dimensions": nil, "limit": nil,
				"stacked": nil, "incident_overlay": nil, "sparkline": nil, "max": nil,
			},
			"query_resources": []interface{}{"cpu_usage"},
			"created_at":      "2026-01-01T00:00:00Z",
			"updated_at":      "2026-01-01T00:00:00Z",
		}
	}

	// Scalars: a key that is sent wins, including an explicit null.
	for _, key := range []string{"title", "description", "metric", "chart_type", "section"} {
		if v, ok := body[key]; ok {
			f.panel[key] = v
		}
	}

	// Layout: only the coordinates that were sent move. An omitted layout leaves
	// the grid's own placement in place, which is the whole point of omitting it.
	if raw, ok := body["layout"].(map[string]interface{}); ok {
		layout := f.panel["layout"].(map[string]interface{})
		for _, key := range []string{"x", "y", "w", "h"} {
			if v, ok := raw[key]; ok {
				layout[key] = v
			}
		}
	}

	// Targets: a kind that is listed is REPLACED, a kind that is omitted is left
	// alone. Getting this backwards is how an unrelated update detaches entities.
	if raw, ok := body["targets"].(map[string]interface{}); ok {
		targets := f.panel["targets"].(map[string]interface{})
		for _, kind := range []string{"hosts", "uptime_monitors", "tasks", "network_devices", "ceph_clusters"} {
			if v, ok := raw[kind]; ok {
				if v == nil {
					targets[kind] = []interface{}{}
					continue
				}
				targets[kind] = v
			}
		}
	}

	// Options: an explicit null CLEARS the setting back to the metric's default,
	// and the response always carries all eight keys.
	if raw, ok := body["options"].(map[string]interface{}); ok {
		options := f.panel["options"].(map[string]interface{})
		for key := range options {
			if v, ok := raw[key]; ok {
				options[key] = v
			}
		}
	}
}

func (f *fakeDashboardAPI) handler() func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		var body map[string]interface{}
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
		}
		path := r.URL.Path
		w.Header().Set("ETag", `"dash"`)

		switch {
		case r.Method == http.MethodDelete:
			if strings.Contains(path, "/sections/") {
				f.sectionDeleted = true
			}
			w.WriteHeader(http.StatusNoContent)
			return

		case strings.HasSuffix(path, "/dashboards/templates") && r.Method == http.MethodPost:
			// The template endpoint names the dashboard itself, deduplicating
			// against what the organisation already has.
			f.name, _ = body["name"].(string)
			if f.dedupeName {
				f.name += " (2)"
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"dashboard": f.dashboardBody(), "section": nil, "created_count": 4,
				"skipped": []interface{}{
					map[string]interface{}{"title": "Replication lag", "reason": "no instance runs PostgreSQL yet"},
				},
				"skip_summary": []interface{}{"1 panel skipped, no instance runs PostgreSQL yet"},
			})
			return

		case strings.Contains(path, "/visualizations"):
			if panel, ok := body["visualization"].(map[string]interface{}); ok {
				f.applyPanel(panel, r.Method == http.MethodPost)
			}
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"visualization": f.panel})
			return

		case strings.Contains(path, "/sections"):
			if section, ok := body["section"].(map[string]interface{}); ok {
				if v, ok := section["name"].(string); ok {
					f.sectionName = v
				}
				if v, ok := section["collapsed"].(bool); ok {
					f.sectionCollapsed = v
				}
				if v, ok := section["position"].(float64); ok {
					f.sectionPosition = int64(v)
				}
			}
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"section": f.sectionBody()})
			return

		default:
			if dashboard, ok := body["dashboard"].(map[string]interface{}); ok {
				if v, ok := dashboard["name"].(string); ok {
					f.name = v
				}
				if v, ok := dashboard["description"]; ok {
					f.description = v
				}
			}
			if r.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"dashboard": f.dashboardBody()})
		}
	}
}

// --- Dashboard ---

// A dashboard with no description must round-trip without the attribute
// flipping between null and "" — the drift that Optional-only attributes make
// an outright apply failure rather than a diff.
func TestDashboardPlan_NoDescriptionIsStable(t *testing.T) {
	planTest(t, newFakeDashboardAPI().handler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_dashboard.test", "name", "Fleet health"),
				resource.TestCheckNoResourceAttr("fivenines_dashboard.test", "description"),
				resource.TestCheckResourceAttr("fivenines_dashboard.test", "shared", "false"),
			),
		}, {
			// Same configuration, second plan: nothing to do.
			Config:   providerConfig + "\nresource \"fivenines_dashboard\" \"test\" {\n  name = \"Fleet health\"\n}",
			PlanOnly: true,
		}},
	})
}

// A padded name is refused at PLAN time, because the API stores it stripped and
// the apply would otherwise fail several steps later with a message about an
// inconsistent result rather than about the whitespace that caused it.
func TestDashboardPlan_PaddedNameIsRefusedAtPlanTime(t *testing.T) {
	planTest(t, newFakeDashboardAPI().handler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "  Fleet health  "
}`,
			ExpectError: regexp.MustCompile(`must not (be empty and must not )?start or end with whitespace`),
		}},
	})
}

// The template endpoint deduplicates the name it is given, so the resource has
// to PATCH the configured name back afterwards. Without that follow-up the
// apply fails outright: `name` is Required, and Terraform refuses a result that
// differs from the configuration.
func TestDashboardPlan_TemplateDeduplicatedNameIsForcedBack(t *testing.T) {
	api := newFakeDashboardAPI()
	api.dedupeName = true
	planTest(t, api.handler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_dashboard" "test" {
  name          = "PostgreSQL"
  template_slug = "postgresql"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_dashboard.test", "name", "PostgreSQL"),
				resource.TestCheckResourceAttr("fivenines_dashboard.test", "template_slug", "postgresql"),
			),
		}},
	})
}

// --- Sections ---

// A configured position must survive plan validation and produce a completed
// apply even when the API's resequencing answers a different number. The same
// shape as the host group, and for the same reason: the alternative designs
// abort at plan time or at apply time instead of reporting the difference.
func TestDashboardSectionPlan_ConfiguredPositionSurvivesResequencing(t *testing.T) {
	api := newFakeDashboardAPI()
	clamped := int64(0)
	api.sectionPositionOverride = &clamped // config asks 2, the API answers 0
	planTest(t, api.handler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_section" "compute" {
  dashboard_id = fivenines_dashboard.test.id
  name         = "Compute"
  position     = 2
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_dashboard_section.compute", "position", "2"),
				resource.TestCheckResourceAttr("fivenines_dashboard_section.compute", "collapsed", "false"),
			),
			ExpectNonEmptyPlan: true,
		}},
	})
}

// With no configured position the API's answer is the only one, and it has to
// land in state rather than leaving an unknown behind.
func TestDashboardSectionPlan_UnconfiguredPositionTakesTheAPIValue(t *testing.T) {
	api := newFakeDashboardAPI()
	appended := int64(3)
	api.sectionPositionOverride = &appended
	planTest(t, api.handler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_section" "compute" {
  dashboard_id = fivenines_dashboard.test.id
  name         = "Compute"
}`,
			Check: resource.TestCheckResourceAttr("fivenines_dashboard_section.compute", "position", "3"),
		}},
	})
}

// --- Visualizations ---

// The shape that has no good default: `options` is Optional-only, so a
// configuration that declares no block must read back as no block. The API
// answers with all eight keys present and null, and mapping that to an empty
// object instead would fail the apply outright.
func TestDashboardVisualizationPlan_AbsentOptionsStaysAbsent(t *testing.T) {
	planTest(t, newFakeDashboardAPI().handler())

	config := providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.test.id
  metric       = "cpu_usage"

  targets = {
    hosts = ["11111111-1111-1111-1111-111111111111"]
  }
}`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckNoResourceAttr("fivenines_dashboard_visualization.cpu", "options"),
				// chart_type defaults, target_kind and query_resources are the API's.
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "chart_type", "line"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "target_kind", "hosts"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "query_resources.0", "cpu_usage"),
				// The four kinds the configuration never mentions come back empty,
				// not null, because the API always sends all five.
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "targets.hosts.0", "11111111-1111-1111-1111-111111111111"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "targets.tasks.#", "0"),
				// An omitted layout is placed by the grid.
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "layout.w", "12"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "layout.h", "6"),
			),
		}, {
			Config:   config,
			PlanOnly: true,
		}},
	})
}

// A declared options block round-trips, and the settings it does not name read
// back as null rather than as the metric's materialized default.
func TestDashboardVisualizationPlan_DeclaredOptionsRoundTrip(t *testing.T) {
	planTest(t, newFakeDashboardAPI().handler())

	config := providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.test.id
  metric       = "cpu_usage"
  chart_type   = "gauge"
  title        = "CPU usage"

  layout = {
    x = 0
    w = 24
  }

  targets = {
    hosts = ["11111111-1111-1111-1111-111111111111"]
  }

  options = {
    reducer          = "avg"
    incident_overlay = true
  }
}`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "options.reducer", "avg"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "options.incident_overlay", "true"),
				// Unset settings stay null: the panel renders the metric's default,
				// and publishing the derived value would freeze today's default
				// into the configuration.
				resource.TestCheckNoResourceAttr("fivenines_dashboard_visualization.cpu", "options.limit"),
				resource.TestCheckNoResourceAttr("fivenines_dashboard_visualization.cpu", "options.group_by"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "chart_type", "gauge"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "title", "CPU usage"),
				// Coordinates the configuration pins are honoured; the rest keep
				// the grid's placement.
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "layout.w", "24"),
				resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "layout.h", "6"),
			),
		}, {
			Config:   config,
			PlanOnly: true,
		}},
	})
}

// Removing a setting from the configuration has to clear it back to the
// metric's default, not leave the old value on the panel. This is the whole
// reason VisualizationOptions carries no omitempty, and it is invisible to a
// unit test that never performs a second apply.
func TestDashboardVisualizationPlan_RemovingAnOptionClearsIt(t *testing.T) {
	api := newFakeDashboardAPI()
	planTest(t, api.handler())

	withReducer := providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.test.id
  metric       = "cpu_usage"
  options      = { reducer = "avg" }
}`

	withoutReducer := providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.test.id
  metric       = "cpu_usage"
}`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: withReducer,
			Check:  resource.TestCheckResourceAttr("fivenines_dashboard_visualization.cpu", "options.reducer", "avg"),
		}, {
			Config: withoutReducer,
			Check:  resource.TestCheckNoResourceAttr("fivenines_dashboard_visualization.cpu", "options"),
		}, {
			Config:   withoutReducer,
			PlanOnly: true,
		}},
	})
}

// A chart type the metric cannot render is the API's call, not the provider's,
// so the provider must surface the refusal rather than swallow it.
func TestDashboardVisualizationPlan_UnknownChartTypeIsRefusedAtPlanTime(t *testing.T) {
	planTest(t, newFakeDashboardAPI().handler())

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.test.id
  metric       = "cpu_usage"
  chart_type   = "sunburst"
}`,
			ExpectError: regexp.MustCompile(`(?s)chart_type.*value must be one of`),
		}},
	})
}

// A section deleted in the dashboard UI has to leave Terraform's state, so the
// next plan proposes re-creating it. Sections have no endpoint of their own, so
// Read reconstructs them from the dashboard definition — and "not in the
// definition" is the ONLY signal that the section is gone. A Read that failed
// to notice would report a section that no longer exists as healthy forever.
func TestDashboardSectionPlan_DeletedOutOfBandLeavesState(t *testing.T) {
	api := newFakeDashboardAPI()
	planTest(t, api.handler())

	config := providerConfig + `
resource "fivenines_dashboard" "test" {
  name = "Fleet health"
}

resource "fivenines_dashboard_section" "compute" {
  dashboard_id = fivenines_dashboard.test.id
  name         = "Compute"
}`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: config,
			Check:  resource.TestCheckResourceAttr("fivenines_dashboard_section.compute", "name", "Compute"),
		}, {
			// Someone removes the section from the dashboard by hand.
			PreConfig: func() {
				api.mu.Lock()
				defer api.mu.Unlock()
				api.sectionDeleted = true
			},
			Config:             config,
			PlanOnly:           true,
			ExpectNonEmptyPlan: true,
		}},
	})
}
