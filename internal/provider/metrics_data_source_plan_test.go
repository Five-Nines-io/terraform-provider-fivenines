package provider_test

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The five metrics data sources are read-only and re-read on every plan, so the
// unit suite can drive Read directly and stay green while the provider is
// structurally unable to produce a valid plan. Three things only Terraform
// itself checks:
//
//   - Consistency. Every metrics data source builds its state by copying the
//     config struct (`state := config`) and filling the computed fields. An
//     Optional attribute that comes back different from the configuration is a
//     "Provider produced inconsistent result" error — which is exactly what
//     would happen if the client-side `format` / `aggregation` defaults were
//     written into state instead of kept in a local.
//   - Validators. OneOf on metric, format, group_by and aggregation is invisible
//     to a direct Read call; only a plan tells a wired validator from an unwired
//     one.
//   - The request the API actually receives. The unit tests assert the client's
//     body; these assert the body the provider builds from real HCL.

// metricsCapture records the last request body a plan sent, so a test can assert
// what HCL turned into on the wire.
type metricsCapture struct {
	mu    sync.Mutex
	paths []string
	last  map[string]interface{}
}

func (c *metricsCapture) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var parsed map[string]interface{}
	json.Unmarshal(body, &parsed)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.paths = append(c.paths, r.URL.Path)
	c.last = parsed
}

func (c *metricsCapture) body() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *metricsCapture) sawPath(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.paths {
		if p == path {
			return true
		}
	}
	return false
}

// TestUptimeDataSourcePlan_ItemsSeriesAndAvailability is the SLA path end to
// end: monitor targets come back keyed monitor_id (not host_id), the timeline
// materialises, and `availability` reduces to the worst entity so a
// precondition can gate on one number.
func TestUptimeDataSourcePlan_ItemsSeriesAndAvailability(t *testing.T) {
	capture := &metricsCapture{}
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "uptime",
				"items": []map[string]interface{}{
					{"monitor_id": "3cac0e44-0000-4000-8000-000000000003", "name": "API", "value": 97.5, "formatted": "97.5%"},
					{"monitor_id": "3cac0e44-0000-4000-8000-000000000004", "name": "Web", "value": 99.98, "formatted": "99.98%"},
				},
				"series": []map[string]interface{}{{
					"monitor_id": "3cac0e44-0000-4000-8000-000000000003",
					"name":       "API",
					"blocks": []map[string]interface{}{
						{"from": 1781000000, "to": 1781003600, "status": "down", "uptime": 0.0},
						{"from": 1781003600, "to": 1781007200, "status": "up", "uptime": 100.0},
					},
				}},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_uptime" "test" {
  monitors       = ["3cac0e44-0000-4000-8000-000000000003", "3cac0e44-0000-4000-8000-000000000004"]
  from           = "2026-08-01T00:00:00Z"
  to             = "2026-09-01T00:00:00Z"
  collapse_scope = false
  aggregation    = "min"
}

# The gate the data source exists for: one number, usable in a condition.
output "meets_sla" {
  value = coalesce(data.fivenines_uptime.test.availability, 0) >= 99.9
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// Optional arguments must survive the round trip byte for byte,
				// or every plan re-plans forever.
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "from", "2026-08-01T00:00:00Z"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "to", "2026-09-01T00:00:00Z"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "aggregation", "min"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "collapse_scope", "false"),
				// monitor_id, not host_id — the id key varies by target kind and
				// the provider folds all three into one attribute.
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "items.0.id", "3cac0e44-0000-4000-8000-000000000003"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "items.0.value", "97.5"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "items.0.formatted", "97.5%"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "items.1.value", "99.98"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "series.0.id", "3cac0e44-0000-4000-8000-000000000003"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "series.0.blocks.0.status", "down"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "series.0.blocks.0.from", "1781000000"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "series.0.blocks.1.uptime", "100"),
				// The worst entity, not the first or the last.
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "availability", "97.5"),
				resource.TestCheckOutput("meets_sla", "false"),
			),
		}},
	})

	if !capture.sawPath("/api/v1/metrics/uptime") {
		t.Errorf("expected POST /api/v1/metrics/uptime, saw %v", capture.paths)
	}
	body := capture.body()
	if body["aggregation"] != "min" {
		t.Errorf("aggregation not forwarded: %v", body["aggregation"])
	}
	tr, _ := body["time_range"].(map[string]interface{})
	if tr == nil || tr["from"] != "2026-08-01T00:00:00Z" {
		t.Errorf("time_range not forwarded: %v", body["time_range"])
	}
	// collapse_scope = false is a meaningful value, not an unset one.
	if got, ok := body["collapse_scope"]; !ok || got != false {
		t.Errorf("expected collapse_scope false to be sent, got %v (present=%v)", got, ok)
	}
}

// No data must never read as 0: an SLA gate would take that for a total outage
// and block an apply that should have been allowed to ask a human instead.
func TestUptimeDataSourcePlan_NoDataIsNullNotZero(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{"type": "uptime", "items": []interface{}{}, "series": []interface{}{}},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_uptime" "test" {
  hosts = ["3cac0e44-0000-4000-8000-000000000001"]
}

output "availability_is_null" {
  value = data.fivenines_uptime.test.availability == null
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckNoResourceAttr("data.fivenines_uptime.test", "availability"),
				resource.TestCheckResourceAttr("data.fivenines_uptime.test", "items.#", "0"),
				resource.TestCheckOutput("availability_is_null", "true"),
			),
		}},
	})
}

// Availability is measured per entity. An empty target set would read the whole
// window as "nothing to report" — indistinguishable from a healthy fleet.
func TestUptimeDataSourcePlan_RequiresATarget(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config with no targets")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerConfig + `data "fivenines_uptime" "test" {}`,
			ExpectError: regexp.MustCompile(`No uptime targets`),
		}},
	})
}

func TestUptimeDataSourcePlan_RejectsUnknownAggregation(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_uptime" "test" {
  hosts       = ["3cac0e44-0000-4000-8000-000000000001"]
  aggregation = "median"
}`,
			ExpectError: regexp.MustCompile(`Invalid Attribute Value Match`),
		}},
	})
}

// A monitor with no certificate data is omitted rather than reported as 0 days,
// so soonest_expiry_days must be null when nothing came back — an alert that
// reads a missing certificate as a healthy one is the failure this guards.
func TestSSLStatusDataSourcePlan_SoonestExpiryAndExpiredSign(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"items": []map[string]interface{}{
					{"monitor_id": "3cac0e44-0000-4000-8000-000000000003", "name": "API", "value": -2.5, "formatted": "Expired"},
					{"monitor_id": "3cac0e44-0000-4000-8000-000000000004", "name": "Web", "value": 42.0, "formatted": "42 days"},
				},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_ssl_status" "test" {
  monitors = ["3cac0e44-0000-4000-8000-000000000003", "3cac0e44-0000-4000-8000-000000000004"]
}

output "renew_now" {
  value = join(",", [for c in data.fivenines_ssl_status.test.items : c.name if c.value < 21])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_ssl_status.test", "items.0.id", "3cac0e44-0000-4000-8000-000000000003"),
				resource.TestCheckResourceAttr("data.fivenines_ssl_status.test", "items.0.formatted", "Expired"),
				// A negative value has to survive as a negative number.
				resource.TestCheckResourceAttr("data.fivenines_ssl_status.test", "soonest_expiry_days", "-2.5"),
				resource.TestCheckOutput("renew_now", "API"),
			),
		}},
	})
}

func TestSSLStatusDataSourcePlan_NoCertificateDataIsNull(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{"type": "aggregated", "items": []interface{}{}},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_ssl_status" "test" {
  monitors = ["3cac0e44-0000-4000-8000-000000000003"]
}`,
			Check: resource.TestCheckNoResourceAttr("data.fivenines_ssl_status.test", "soonest_expiry_days"),
		}},
	})
}

// format and aggregation are required by the API but optional in the schema,
// defaulted client-side. The default must NOT be written back into state: an
// Optional attribute that changes between config and state fails Terraform's
// consistency check. This is the test that catches it.
func TestMetricQueryDataSourcePlan_DefaultsSentButNotWrittenToState(t *testing.T) {
	capture := &metricsCapture{}
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"items": []map[string]interface{}{
					{"name": "web-01", "host_id": "3cac0e44-0000-4000-8000-000000000001", "value": 42.5, "formatted": "42.5%"},
				},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_metric_query" "test" {
  hosts    = ["3cac0e44-0000-4000-8000-000000000001"]
  resource = "cpu_usage"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				// Left unset in config, so they must stay unset in state.
				resource.TestCheckNoResourceAttr("data.fivenines_metric_query.test", "format"),
				resource.TestCheckNoResourceAttr("data.fivenines_metric_query.test", "aggregation"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "items.0.id", "3cac0e44-0000-4000-8000-000000000001"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "items.0.value", "42.5"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "series.#", "0"),
			),
		}},
	})

	body := capture.body()
	query, _ := body["query"].(map[string]interface{})
	if query == nil {
		t.Fatalf("no query object sent: %v", body)
	}
	// The API requires all three, so the defaults have to reach the wire even
	// though they never reach state.
	if query["resource"] != "cpu_usage" || query["format"] != "aggregated" || query["aggregation"] != "avg" {
		t.Errorf("defaults not applied on the wire: %v", query)
	}
	// time_range is a required key on this endpoint even when empty.
	if _, ok := body["time_range"]; !ok {
		t.Errorf("time_range key missing from the request body: %v", body)
	}
}

func TestMetricQueryDataSourcePlan_TimeSeriesFiltersAndPoints(t *testing.T) {
	capture := &metricsCapture{}
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "time_series",
				"series": []map[string]interface{}{{
					"name":    "web-01 sda",
					"host_id": "3cac0e44-0000-4000-8000-000000000001",
					"data":    [][]float64{{1781000000, 12.5}, {1781000060, 13.5}},
				}},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_metric_query" "test" {
  hosts       = ["3cac0e44-0000-4000-8000-000000000001"]
  resource    = "partition_percent"
  format      = "time_series"
  aggregation = "max"
  group_by    = "instance_device"
  limit       = 5
  filters     = { device = ["sda", "sdb"] }
  exclude     = { device = ["loop0"] }
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "format", "time_series"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "filters.device.0", "sda"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "exclude.device.0", "loop0"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "series.0.id", "3cac0e44-0000-4000-8000-000000000001"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "series.0.points.#", "2"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "series.0.points.0.timestamp", "1781000000"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "series.0.points.1.value", "13.5"),
				resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "items.#", "0"),
			),
		}},
	})

	query, _ := capture.body()["query"].(map[string]interface{})
	if query["group_by"] != "instance_device" || query["limit"] != float64(5) {
		t.Errorf("group_by/limit not forwarded: %v", query)
	}
	filters, _ := query["filters"].(map[string]interface{})
	if filters == nil || len(filters["device"].([]interface{})) != 2 {
		t.Errorf("label filters not forwarded: %v", query["filters"])
	}
}

func TestMetricQueryDataSourcePlan_RequiresATarget(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config with no targets")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerConfig + `data "fivenines_metric_query" "test" { resource = "cpu_usage" }`,
			ExpectError: regexp.MustCompile(`No query targets`),
		}},
	})
}

// group_by is per-metric server-side vocabulary; an unknown value has to be
// refused at plan time rather than reaching the API as a 400 mid-apply.
func TestMetricQueryDataSourcePlan_RejectsUnknownGroupBy(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_metric_query" "test" {
  hosts    = ["3cac0e44-0000-4000-8000-000000000001"]
  resource = "cpu_usage"
  group_by = "hostname"
}`,
			ExpectError: regexp.MustCompile(`Invalid Attribute Value Match`),
		}},
	})
}

// MTTR is a mean in seconds; `value` is the one figure an ungrouped read
// answers with, and `unit` says how to read it.
func TestIncidentStatsDataSourcePlan_ValueAndUnit(t *testing.T) {
	capture := &metricsCapture{}
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"unit": "duration",
				"items": []map[string]interface{}{
					{"name": "MTTR", "value": 8100, "formatted": "2h 15m"},
				},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_incident_stats" "test" {
  metric = "incident_mttr"
  from   = "2026-08-01T00:00:00Z"
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_incident_stats.test", "value", "8100"),
				resource.TestCheckResourceAttr("data.fivenines_incident_stats.test", "unit", "duration"),
				resource.TestCheckResourceAttr("data.fivenines_incident_stats.test", "items.0.formatted", "2h 15m"),
				// An org-wide figure belongs to no entity.
				resource.TestCheckNoResourceAttr("data.fivenines_incident_stats.test", "items.0.id"),
			),
		}},
	})

	if !capture.sawPath("/api/v1/metrics/incident_stats") {
		t.Errorf("expected POST /api/v1/metrics/incident_stats, saw %v", capture.paths)
	}
	if capture.body()["metric"] != "incident_mttr" {
		t.Errorf("metric not forwarded: %v", capture.body())
	}
}

// A grouped read answers with a ranking, so the single-figure shortcut must be
// null rather than silently reporting the first bucket as the whole number.
func TestIncidentStatsDataSourcePlan_GroupedReadHasNoSingleValue(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"unit": "count",
				"items": []map[string]interface{}{
					{"name": "web-01", "value": 12, "formatted": "12"},
				},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_incident_stats" "test" {
  metric   = "incident_count"
  group_by = "entity"
  limit    = 10
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckNoResourceAttr("data.fivenines_incident_stats.test", "value"),
				resource.TestCheckResourceAttr("data.fivenines_incident_stats.test", "items.0.value", "12"),
			),
		}},
	})
}

func TestIncidentStatsDataSourcePlan_RejectsUnknownMetric(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerConfig + `data "fivenines_incident_stats" "test" { metric = "incident_p95" }`,
			ExpectError: regexp.MustCompile(`Invalid Attribute Value Match`),
		}},
	})
}

// The severity breakdown is the reporting shape, and the optional host filter
// has to reach the wire — a filter that is silently dropped answers with the
// organization-wide total under a name that says otherwise.
func TestCveStatsDataSourcePlan_SeverityBucketsAndHostFilter(t *testing.T) {
	capture := &metricsCapture{}
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"unit": "count",
				"items": []map[string]interface{}{
					{"name": "Critical", "value": 3, "formatted": "3"},
					{"name": "High", "value": 11, "formatted": "11"},
				},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_cve_stats" "test" {
  metric   = "cve_actionable_count"
  group_by = "severity"
  hosts    = ["3cac0e44-0000-4000-8000-000000000001"]
}

output "critical_count" {
  value = join(",", [for b in data.fivenines_cve_stats.test.items : tostring(b.value) if b.name == "Critical"])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_cve_stats.test", "items.#", "2"),
				resource.TestCheckResourceAttr("data.fivenines_cve_stats.test", "unit", "count"),
				resource.TestCheckNoResourceAttr("data.fivenines_cve_stats.test", "value"),
				resource.TestCheckOutput("critical_count", "3"),
			),
		}},
	})

	body := capture.body()
	hosts, _ := body["hosts"].([]interface{})
	if len(hosts) != 1 || hosts[0] != "3cac0e44-0000-4000-8000-000000000001" {
		t.Errorf("host filter not forwarded: %v", body["hosts"])
	}
	if body["group_by"] != "severity" {
		t.Errorf("group_by not forwarded: %v", body["group_by"])
	}
}

func TestCveStatsDataSourcePlan_RejectsUnknownGroupBy(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_cve_stats" "test" {
  metric   = "cve_count"
  group_by = "package"
}`,
			ExpectError: regexp.MustCompile(`Invalid Attribute Value Match`),
		}},
	})
}

// An API error has to surface as a plan-time diagnostic carrying the server's
// own words — the 422 target cap is the one a practitioner hits first — and the
// request_id with it, so a support ticket can name the exact request (#26).
func TestMetricsDataSourcePlan_APIErrorSurfacesInDiagnostics(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error":      "Too many targets: 51 (max 50 per query)",
			"request_id": "req-metrics-422",
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_uptime" "test" {
  hosts = ["3cac0e44-0000-4000-8000-000000000001"]
}`,
			ExpectError: regexp.MustCompile(`Too many targets: 51 \(max 50 per query\)[\s\S]*req-metrics-422`),
		}},
	})
}

// --- Empty target lists (Codex review, post-merge) ---
//
// An explicitly empty target list is never what a practitioner means, and it is
// not always safe. On cve_stats the API reads an ABSENT hosts filter as the
// whole organization, and `hosts = []` marshals away under omitempty — so a
// scoped CVE gate would silently answer with the org-wide total. The API cannot
// express "filter to nothing" (Rails folds [] and absent together), so the only
// correct provider behaviour is to refuse the config at plan time.

func TestCveStatsDataSourcePlan_RejectsEmptyHostFilter(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("an empty hosts filter must never reach the API: it reads as the whole organization")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_cve_stats" "test" {
  metric = "cve_count"
  hosts  = []
}`,
			ExpectError: regexp.MustCompile(`Invalid Attribute Value`),
		}},
	})
}

func TestSSLStatusDataSourcePlan_RejectsEmptyMonitorList(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for an empty monitor list")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      providerConfig + `data "fivenines_ssl_status" "test" { monitors = [] }`,
			ExpectError: regexp.MustCompile(`Invalid Attribute Value`),
		}},
	})
}

// uptime and metric_query take three independent target lists and guard the
// dangerous case (all of them empty) in Read, so a per-list SizeAtLeast(1) would
// reject a legal config: a `for` expression yielding [] for the kind that is not
// in use, while another list carries the real targets. These two pin that.
func TestUptimeDataSourcePlan_EmptyListBesideARealTargetIsAllowed(t *testing.T) {
	capture := &metricsCapture{}
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		capture.record(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "uptime",
				"items": []map[string]interface{}{
					{"monitor_id": "3cac0e44-0000-4000-8000-000000000003", "name": "API", "value": 99.9, "formatted": "99.9%"},
				},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_uptime" "test" {
  hosts    = []
  monitors = ["3cac0e44-0000-4000-8000-000000000003"]
}`,
			Check: resource.TestCheckResourceAttr("data.fivenines_uptime.test", "availability", "99.9"),
		}},
	})

	// The empty list must not reach the wire as an empty scope.
	if _, ok := capture.body()["hosts"]; ok {
		t.Errorf("an empty hosts list should be omitted, got %v", capture.body()["hosts"])
	}
}

func TestMetricQueryDataSourcePlan_EmptyListBesideARealTargetIsAllowed(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "aggregated",
				"items": []map[string]interface{}{
					{"name": "API", "monitor_id": "3cac0e44-0000-4000-8000-000000000003", "value": 120, "formatted": "120ms"},
				},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_metric_query" "test" {
  resource = "uptime_response_time_ms"
  hosts    = []
  monitors = ["3cac0e44-0000-4000-8000-000000000003"]
}`,
			Check: resource.TestCheckResourceAttr("data.fivenines_metric_query.test", "items.0.value", "120"),
		}},
	})
}

// All three empty is still refused — by the aggregate guard in Read, with a
// message that names the three arguments rather than a bare validator error.
func TestMetricQueryDataSourcePlan_AllTargetListsEmptyIsRefused(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called when every target list is empty")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_metric_query" "test" {
  resource = "cpu_usage"
  hosts    = []
  monitors = []
  devices  = []
}`,
			ExpectError: regexp.MustCompile(`No query targets`),
		}},
	})
}

// A truncated [timestamp, value] pair has to become a diagnostic, not a shorter
// series that still reads as a complete answer.
func TestMetricQueryDataSourcePlan_MalformedSeriesPointIsRefused(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"type": "time_series",
				"series": []map[string]interface{}{{
					"name": "web-01",
					"data": [][]float64{{1781000000, 12.5}, {1781000060}},
				}},
			},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_metric_query" "test" {
  hosts    = ["3cac0e44-0000-4000-8000-000000000001"]
  resource = "cpu_usage"
  format   = "time_series"
}`,
			ExpectError: regexp.MustCompile(`expected a \[timestamp, value\] pair`),
		}},
	})
}
