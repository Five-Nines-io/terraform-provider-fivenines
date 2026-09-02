package provider_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// The twenty collector inventories (#22) are generated from a table, so a
// mistake in the table is a mistake in twenty data sources at once and the unit
// tests cannot see any of it: they drive Read directly, which skips config
// validation entirely. Only a plan test can tell a wired OneOf validator from
// an unwired one, or catch a filter that never round-trips into state.

func systemdUnitsPlanHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"systemd_units": []map[string]interface{}{{
			"id": 1, "host_id": "3cac0e44-0000-4000-8000-000000000001",
			"name": "nginx.service", "active_state": "failed", "failed": true,
			"memory_current": 8388608,
			// The three that must not arrive as zeros.
			"oom_kill_count": nil, "cpu_usec": nil, "reverse_deps": nil,
			"journal_tail": []string{"boom"},
			"stale":        false,
			"created_at":   "2026-01-01T00:00:00Z",
			"updated_at":   "2026-01-01T00:00:00Z",
		}},
		"collector": map[string]interface{}{
			"name": "systemd", "enabled": true, "supported": true, "pending": false,
			"unavailable_reason": nil, "blocked_reason": nil,
			"last_reported_at": "2026-01-01T00:00:00Z",
		},
		"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
	})
}

// Filters have to survive the round trip into state: an argument silently
// dropped re-plans forever, and one never mapped reaches the API as an
// unfiltered list the practitioner never asked for.
func TestSystemdUnitsDataSourcePlan_FiltersRoundTripAndNullsSurvive(t *testing.T) {
	var got url.Values
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		systemdUnitsPlanHandler(w, r)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_systemd_units" "test" {
  instance_id  = "3cac0e44-0000-4000-8000-000000000001"
  active_state = "failed"
  stale        = false
  q            = "nginx"
}

output "failed_names" {
  value = join(",", [for u in data.fivenines_systemd_units.test.systemd_units : u.name if u.failed])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "active_state", "failed"),
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "stale", "false"),
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "q", "nginx"),
				// The collector block is the whole point of #22: without it an
				// empty list cannot be told from a switched-off collector.
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "collector.name", "systemd"),
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "collector.enabled", "true"),
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "collector.supported", "true"),
				resource.TestCheckNoResourceAttr("data.fivenines_systemd_units.test", "collector.unavailable_reason"),
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "systemd_units.0.name", "nginx.service"),
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "systemd_units.0.memory_current", "8388608"),
				// A null cgroup counter must reach state as null. As 0 it would
				// read as "nothing was OOM killed", which is the opposite claim.
				resource.TestCheckNoResourceAttr("data.fivenines_systemd_units.test", "systemd_units.0.oom_kill_count"),
				resource.TestCheckNoResourceAttr("data.fivenines_systemd_units.test", "systemd_units.0.cpu_usec"),
				// null reverse_deps means "systemd too old to report them";
				// [] would mean "nothing depends on this unit".
				resource.TestCheckNoResourceAttr("data.fivenines_systemd_units.test", "systemd_units.0.reverse_deps"),
				resource.TestCheckResourceAttr("data.fivenines_systemd_units.test", "systemd_units.0.journal_tail.0", "boom"),
				resource.TestCheckOutput("failed_names", "nginx.service"),
			),
		}},
	})

	if got.Get("active_state") != "failed" {
		t.Errorf("active_state did not reach the API: %q", got.Get("active_state"))
	}
	if got.Get("stale") != "false" {
		t.Errorf("stale did not reach the API: %q", got.Get("stale"))
	}
	if got.Get("q") != "nginx" {
		t.Errorf("q did not reach the API: %q", got.Get("q"))
	}
}

// A generated OneOf is invisible to every other test in the repo. If the table's
// oneOf slice were dropped, or filterAttribute stopped wiring it, this is the
// only place that notices.
func TestSystemdUnitsDataSourcePlan_RejectsUnknownActiveState(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_systemd_units" "test" {
  instance_id  = "3cac0e44-0000-4000-8000-000000000001"
  active_state = "broken"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute active_state value must be one of.*"failed"`),
		}},
	})
}

func TestDockerImagesDataSourcePlan_RejectsUnknownState(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_docker_images" "test" {
  instance_id = "3cac0e44-0000-4000-8000-000000000001"
  state       = "clean"
}`,
			ExpectError: regexp.MustCompile(`(?s)Attribute state value must be one of.*"unscannable"`),
		}},
	})
}

// instance_id is Required on all twenty. Omitting it must fail at plan time,
// not produce a request to /api/v1/instances//systemd_units.
func TestInventoryDataSourcePlan_InstanceIDIsRequired(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("the API must not be called for a config that fails validation")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_zfs_pools" "test" {}`,
			ExpectError: regexp.MustCompile(`(?s)The argument "instance_id" is required`),
		}},
	})
}

// Omitting `vanished` must send NO vanished parameter. That default is what
// returns the tombstones, and it is the only deletion signal in this family --
// a data source that quietly defaulted to false would hide the removal an
// inventory sync most needs to see.
func TestQemuVmsDataSourcePlan_TombstonesAreReturnedByDefault(t *testing.T) {
	var sawVanishedParam bool
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, sawVanishedParam = r.URL.Query()["vanished"]
		json.NewEncoder(w).Encode(map[string]interface{}{
			"qemu_vms": []map[string]interface{}{
				{
					"id": "3cac0e44-0000-4000-8000-00000000000a", "host_id": "3cac0e44-0000-4000-8000-000000000001",
					"vm_uuid": "u1", "vm_name": "web-01", "status": "running",
					"vanished": false, "metrics_fresh": true, "cpu_percent": 42.5,
					"stale": false, "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
				},
				{
					"id": "3cac0e44-0000-4000-8000-00000000000b", "host_id": "3cac0e44-0000-4000-8000-000000000001",
					"vm_uuid": "u2", "vm_name": "old-01", "status": "shutoff",
					"vanished": true, "vanished_at": "2026-01-01T00:00:00Z",
					// Freshness-gated: a stale sample is null, never the last
					// known number.
					"metrics_fresh": false, "cpu_percent": nil,
					"stale": true, "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
				},
			},
			"collector": map[string]interface{}{"name": "qemu", "enabled": true, "supported": true, "pending": false},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_qemu_vms" "test" {
  instance_id = "3cac0e44-0000-4000-8000-000000000001"
}

output "vanished_names" {
  value = join(",", [for v in data.fivenines_qemu_vms.test.qemu_vms : v.vm_name if v.vanished])
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_qemu_vms.test", "qemu_vms.#", "2"),
				resource.TestCheckResourceAttr("data.fivenines_qemu_vms.test", "qemu_vms.0.cpu_percent", "42.5"),
				resource.TestCheckNoResourceAttr("data.fivenines_qemu_vms.test", "qemu_vms.1.cpu_percent"),
				resource.TestCheckOutput("vanished_names", "old-01"),
			),
		}},
	})

	if sawVanishedParam {
		t.Error("an unset vanished argument must not be sent: omitting it is what returns the tombstones")
	}
}

// A free-form column with no pinned shape is exposed as its JSON encoding,
// because Terraform has no type for it. jsondecode() has to work on the result.
func TestZfsPoolsDataSourcePlan_VdevTreeIsDecodableJSON(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"zfs_pools": []map[string]interface{}{{
				"id": 1, "host_id": "3cac0e44-0000-4000-8000-000000000001",
				"name": "tank", "health": "DEGRADED", "problem": true,
				// Null, not 0: "nobody has ever checked" vs "a scrub was clean".
				"scrub_errors": nil, "degraded_vdevs": nil,
				"vdev_tree":  map[string]interface{}{"type": "mirror", "state": "DEGRADED"},
				"stale":      false,
				"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
			}},
			"collector": map[string]interface{}{"name": "zfs", "enabled": true, "supported": true, "pending": false},
			"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 1, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_zfs_pools" "test" {
  instance_id = "3cac0e44-0000-4000-8000-000000000001"
}

output "tank_vdev_state" {
  value = jsondecode(data.fivenines_zfs_pools.test.zfs_pools[0].vdev_tree).state
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckOutput("tank_vdev_state", "DEGRADED"),
				resource.TestCheckNoResourceAttr("data.fivenines_zfs_pools.test", "zfs_pools.0.scrub_errors"),
				resource.TestCheckNoResourceAttr("data.fivenines_zfs_pools.test", "zfs_pools.0.degraded_vdevs"),
			),
		}},
	})
}

// An empty list with the collector switched off is the case the block exists
// for. The rows were deleted by the toggle, so "no containers" is a lie the
// practitioner has to be able to detect from the config.
func TestDockerContainersDataSourcePlan_EmptyListCarriesCollectorBlock(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"docker_containers": []interface{}{},
			"collector": map[string]interface{}{
				"name": "docker", "enabled": false, "supported": false, "pending": false,
				"unavailable_reason": "agent_outdated",
				"blocked_reason":     "docker socket not readable",
				"last_reported_at":   "2026-01-01T00:00:00Z",
			},
			"meta": map[string]int{"current_page": 1, "total_pages": 1, "total_count": 0, "per_page": 100},
		})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_docker_containers" "test" {
  instance_id = "3cac0e44-0000-4000-8000-000000000001"
}

output "safe_to_call_it_clean" {
  value = tostring(
    data.fivenines_docker_containers.test.collector.enabled &&
    data.fivenines_docker_containers.test.collector.supported &&
    length(data.fivenines_docker_containers.test.docker_containers) == 0
  )
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.fivenines_docker_containers.test", "docker_containers.#", "0"),
				resource.TestCheckResourceAttr("data.fivenines_docker_containers.test", "collector.enabled", "false"),
				resource.TestCheckResourceAttr("data.fivenines_docker_containers.test", "collector.unavailable_reason", "agent_outdated"),
				resource.TestCheckResourceAttr("data.fivenines_docker_containers.test", "collector.blocked_reason", "docker socket not readable"),
				resource.TestCheckOutput("safe_to_call_it_clean", "false"),
			),
		}},
	})
}

// The published examples are documentation, and a doc example that errors on
// real data is worse than none. Terraform refuses to compare or interpolate a
// null, so the two examples whose columns are nullable by contract -- an
// unscanned image's counts, an unclassified socket's protocol -- are replayed
// here against exactly those nulls.
func TestInventoryDataSourcePlan_DocExamplesSurviveNullColumns(t *testing.T) {
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case regexp.MustCompile(`/docker_images$`).MatchString(r.URL.Path):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"docker_images": []map[string]interface{}{
					{
						"id": "3cac0e44-0000-4000-8000-00000000000c", "image_id": "sha256:aaa",
						"display_name": "never-scanned", "state": "pending", "countable": false,
						// The honesty contract: null, not 0.
						"vulnerability_count": nil, "critical_vulnerability_count": nil,
						"packages_truncated": false, "finding_count_is_floor": false,
						"tags": []string{}, "repo_digests": []string{},
						"last_seen_at": "2026-01-01T00:00:00Z",
						"created_at":   "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
					},
					{
						"id": "3cac0e44-0000-4000-8000-00000000000d", "image_id": "sha256:bbb",
						"display_name": "nginx:1.27", "state": "scanned", "countable": true,
						"vulnerability_count": 12, "critical_vulnerability_count": 3,
						"packages_truncated": true, "finding_count_is_floor": true,
						"tags": []string{"nginx:1.27"}, "repo_digests": []string{},
						"last_seen_at": "2026-01-01T00:00:00Z",
						"created_at":   "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
					},
				},
				"collector": map[string]interface{}{"name": "docker", "enabled": true, "supported": true, "pending": false},
				"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
			})
		default: // listening_ports
			json.NewEncoder(w).Encode(map[string]interface{}{
				"listening_ports": []map[string]interface{}{
					{"port": 443, "protocol": "tcp", "address": "0.0.0.0", "stack": "dual-stack", "loopback": false},
					// An agent that classified nothing: every string null.
					{"port": 9000, "protocol": nil, "address": nil, "stack": nil, "loopback": false},
				},
				"collector": map[string]interface{}{"name": "listening_ports", "enabled": true, "supported": true, "pending": false},
				"meta":      map[string]int{"current_page": 1, "total_pages": 1, "total_count": 2, "per_page": 100},
			})
		}
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: providerConfig + `
data "fivenines_docker_images" "test" {
  instance_id = "3cac0e44-0000-4000-8000-000000000001"
}

data "fivenines_listening_ports" "test" {
  instance_id = "3cac0e44-0000-4000-8000-000000000001"
  loopback    = false
}

# Verbatim from examples/data-sources/fivenines_docker_images: the split into
# countable / not-countable is what keeps a null out of the > comparison.
locals {
  scanned_images   = [for i in data.fivenines_docker_images.test.docker_images : i if i.countable]
  unscanned_images = [for i in data.fivenines_docker_images.test.docker_images : i if !i.countable]
}

output "images_with_critical_cves" {
  value = join(",", [for i in local.scanned_images : i.display_name if i.critical_vulnerability_count > 0])
}

output "unscanned_images" {
  value = join(",", [for i in local.unscanned_images : i.display_name])
}

output "images_with_floor_counts" {
  value = join(",", [for i in data.fivenines_docker_images.test.docker_images : i.display_name if i.finding_count_is_floor])
}

# Verbatim from examples/data-sources/fivenines_listening_ports: the object
# constructor keeps nullable columns as attributes instead of interpolating
# them into a string, which Terraform rejects on a null.
output "exposed_port_count" {
  value = tostring(length([
    for p in data.fivenines_listening_ports.test.listening_ports :
    ({ port = p.port, protocol = p.protocol, address = p.address })
  ]))
}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckOutput("images_with_critical_cves", "nginx:1.27"),
				resource.TestCheckOutput("unscanned_images", "never-scanned"),
				resource.TestCheckOutput("images_with_floor_counts", "nginx:1.27"),
				resource.TestCheckOutput("exposed_port_count", "2"),
				resource.TestCheckNoResourceAttr("data.fivenines_listening_ports.test", "listening_ports.1.protocol"),
			),
		}},
	})
}
