package provider_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"testing"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/client"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Instance plan tests: real Terraform against a fake API, like the MQTT ones.
// The instance resource has two hazards the unit tests cannot see:
//
//   - Ten write-only credentials that the API never returns, each paired with
//     a Computed `<name>_set` boolean. Get the pairing wrong and the failure
//     is "inconsistent result after apply", which only plan validation hits.
//   - Sixty-odd Optional+Computed settings with server-owned defaults. On
//     create they are unknown; the server's defaults have to land in state
//     without planning a diff forever after.

// instanceSecretKeys is the canonical secret list, one place for all tiers.
var instanceSecretKeys = client.InstanceSecretKeys

// freshInstanceBody is a host as the API creates it: agent-reported fields
// null, collector settings at their server-side defaults — which are not
// uniform (toggles false, tsdb/vllm/sglang verify SSL, several URLs
// pre-filled), which is exactly why the provider must not carry its own.
func freshInstanceBody() map[string]interface{} {
	return map[string]interface{}{
		"id": "instance-uuid", "display_name": "web-01",
		"description": nil, "hostname": nil, "host_group_id": nil, "cluster_name": nil,
		"enabled": true, "maintenance_mode": false,
		"operating_system_name": nil, "kernel_version": nil, "cpu_architecture": nil,
		"cpu_model": nil, "cpu_count": 1, "memory_size": nil, "ipv4": nil, "ipv6": nil,
		"source": "agent", "client_version": nil, "status": "waiting",
		"last_sync_at": nil, "first_sync_at": nil, "last_request_at": nil,
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",

		"smart_storage_health_enabled": false, "raid_storage_health_enabled": false,
		"zfs_enabled": false, "ceph_enabled": false,
		"qemu_enabled": false, "qemu_uri": nil,
		"proxmox_enabled": false, "proxmox_host": "localhost", "proxmox_port": 8006,
		"proxmox_token_id": nil, "proxmox_verify_ssl": false,
		"docker_enabled": false, "docker_socket_url": nil,
		"redis_enabled": false, "redis_port": nil,
		"memcached_enabled": false, "memcached_host": nil, "memcached_port": nil,
		"postgresql_enabled": false, "postgresql_host": "localhost", "postgresql_port": 5432,
		"postgresql_user": "postgres", "postgresql_database": "postgres",
		"mysql_enabled": false, "mysql_host": "localhost", "mysql_port": 3306,
		"mysql_user": "root", "mysql_database": nil, "mysql_socket": nil,
		"nginx_enabled": false, "nginx_status_page_url": nil,
		"apache_enabled": false, "apache_status_page_url": nil,
		"caddy_enabled": false, "caddy_admin_api_url": "http://localhost:2019",
		"php_fpm_enabled": false, "php_fpm_status_page_url": nil,
		"haproxy_enabled": false, "haproxy_stats_url": nil, "haproxy_stats_socket": nil,
		"haproxy_username": nil,
		"rabbitmq_enabled": false, "rabbitmq_management_url": nil,
		"rabbitmq_username": nil, "rabbitmq_vhost_filter": nil,
		"tsdb_enabled": false, "tsdb_url": "http://127.0.0.1:9090",
		"tsdb_auth_header_name": nil, "tsdb_basic_auth_username": nil, "tsdb_verify_ssl": true,
		"vllm_enabled": false, "vllm_metrics_url": "http://127.0.0.1:8000/metrics",
		"vllm_auth_header_name": nil, "vllm_verify_ssl": true,
		"sglang_enabled": false, "sglang_metrics_url": "http://127.0.0.1:30000/metrics",
		"sglang_auth_header_name": nil, "sglang_verify_ssl": true,
		"wireguard_enabled": false, "tailscale_enabled": false, "systemd_enabled": false,
		"fail2ban_enabled": false, "nvidia_gpu_enabled": false, "ipv6_enabled": nil,
		"logs_enabled": false, "logs_units_csv": nil,

		"redis_password_set": false, "postgresql_password_set": false,
		"mysql_password_set": false, "rabbitmq_password_set": false,
		"haproxy_password_set": false, "proxmox_token_secret_set": false,
		"tsdb_auth_header_value_set": false, "tsdb_basic_auth_password_set": false,
		"vllm_auth_header_value_set": false, "sglang_auth_header_value_set": false,
	}
}

// instanceHandler serves one host whose settings are whatever the last write
// said, mirroring HostSettingsParams: an absent key leaves the stored value
// alone, and secrets are stored as a flipped `_set` boolean, never echoed.
// The provider's side of the blank-means-keep contract is to OMIT a secret it
// is not managing — sending "" or null instead is a bug this handler fails.
func instanceHandler(t *testing.T) func(http.ResponseWriter, *http.Request) {
	var mu sync.Mutex
	inst := freshInstanceBody()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		mu.Lock()
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			var body map[string]map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			for key, value := range body["instance"] {
				if slices.Contains(instanceSecretKeys, key) {
					if value == nil || value == "" {
						t.Errorf("provider sent secret %q as %#v; an unmanaged secret must be omitted", key, value)
						continue
					}
					inst[key+"_set"] = true
					continue
				}
				inst[key] = value
			}
		}
		resp := maps.Clone(inst)
		mu.Unlock()

		w.Header().Set("ETag", `"instance"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"instance": resp})
	}
}

// Configured settings and credentials have to survive plan validation, land in
// state, flip their `_set` booleans — and then plan clean, both on an
// unchanged config and across a credential rotation.
func TestInstancePlan_ConfiguredSettingsAndSecretsAreStable(t *testing.T) {
	planTest(t, instanceHandler(t))

	cfg := func(password string) string {
		return providerConfig + `
resource "fivenines_instance" "test" {
  display_name   = "web-01"
  description    = "primary web tier"
  docker_enabled = true
  redis_enabled  = true
  redis_port     = 6390
  redis_password = "` + password + `"

  proxmox_enabled      = true
  proxmox_token_id     = "monitoring@pve!fivenines"
  proxmox_token_secret = "pve-secret"
}`
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("s3cret"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "description", "primary web tier"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "docker_enabled", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_port", "6390"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password", "s3cret"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "proxmox_token_secret_set", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "mysql_password_set", "false"),
				),
			},
			{Config: cfg("s3cret"), PlanOnly: true},
			{
				ResourceName:      "fivenines_instance.test",
				ImportState:       true,
				ImportStateVerify: true,
				// Write-only: an import cannot know the credentials.
				ImportStateVerifyIgnore: []string{"redis_password", "proxmox_token_secret"},
			},
			{
				// Rotation is just a new value on the wire; `_set` stays true.
				Config: cfg("rotated"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password", "rotated"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
				),
			},
			{Config: cfg("rotated"), PlanOnly: true},
		},
	})
}

// A bare instance must read the server's defaults into state — including the
// ones that are not false/null (tsdb_verify_ssl, the pre-filled URLs) — and
// then plan clean. Provider-side defaults would fight these; their absence is
// the contract under test.
func TestInstancePlan_UnconfiguredSettingsFollowTheServer(t *testing.T) {
	planTest(t, instanceHandler(t))

	cfg := providerConfig + `
resource "fivenines_instance" "test" {
  display_name = "web-01"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "tsdb_verify_ssl", "true"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "caddy_admin_api_url", "http://localhost:2019"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "proxmox_port", "8006"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "docker_enabled", "false"),
					resource.TestCheckNoResourceAttr("fivenines_instance.test", "ipv6_enabled"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "false"),
					resource.TestCheckNoResourceAttr("fivenines_instance.test", "redis_password"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// Dropping a credential from the configuration is an unrelated edit as far as
// the wire is concerned: the PATCH omits the key (the handler fails on "" or
// null), the stored secret survives, and `_set` keeps reporting it.
func TestInstancePlan_DroppedSecretLeavesTheStoredValueAlone(t *testing.T) {
	planTest(t, instanceHandler(t))

	withPassword := providerConfig + `
resource "fivenines_instance" "test" {
  display_name   = "web-01"
  redis_password = "s3cret"
}`
	withoutPassword := providerConfig + `
resource "fivenines_instance" "test" {
  display_name = "web-01"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: withPassword,
				Check:  resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
			},
			{
				Config: withoutPassword,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("fivenines_instance.test", "redis_password"),
					// Still stored server-side: omission preserves.
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
				),
			},
			{Config: withoutPassword, PlanOnly: true},
		},
	})
}

// A blank means "keep the stored value" to the API — Rails `blank?`, so
// whitespace-only counts — and state would claim a credential the server
// silently discarded. Both spellings are refused at validation, before
// anything reaches the wire.
func TestInstancePlan_BlankSecretIsRejected(t *testing.T) {
	planTest(t, instanceHandler(t))

	cfg := func(password string) string {
		return providerConfig + `
resource "fivenines_instance" "test" {
  display_name   = "web-01"
  redis_password = "` + password + `"
}`
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      cfg(""),
				ExpectError: regexp.MustCompile(`must contain a non-whitespace character`),
			},
			{
				Config:      cfg("  "),
				ExpectError: regexp.MustCompile(`must contain a non-whitespace character`),
			},
		},
	})
}

// The server strips surrounding whitespace on nine of the endpoint fields at
// assignment. A padded value would be accepted, stored stripped, and read
// back different from the plan — a hard "inconsistent result after apply" —
// so the padding dies at validation instead.
func TestInstancePlan_PaddedURLIsRejected(t *testing.T) {
	planTest(t, instanceHandler(t))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_instance" "test" {
  display_name = "web-01"
  tsdb_url     = " http://127.0.0.1:9090"
}`,
				ExpectError: regexp.MustCompile(`must not start or end with whitespace`),
			},
		},
	})
}

// The reconcile half of the `_set` contract. A credential cleared in the
// dashboard flips its `_set` boolean false on refresh, which clears the
// configured value out of state (storedSecret) — so the next plan re-sends
// the credential instead of reporting "No changes" against a host that holds
// none. Applying then restores it and the plan settles.
func TestInstancePlan_DashboardClearedSecretPlansResend(t *testing.T) {
	var mu sync.Mutex
	inst := freshInstanceBody()
	dashboardCleared := false
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mu.Lock()
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			var body map[string]map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			for key, value := range body["instance"] {
				if slices.Contains(instanceSecretKeys, key) {
					if value == nil || value == "" {
						t.Errorf("provider sent secret %q as %#v; an unmanaged secret must be omitted", key, value)
						continue
					}
					inst[key+"_set"] = true
					continue
				}
				inst[key] = value
			}
		}
		if dashboardCleared {
			inst["redis_password_set"] = false
		}
		resp := maps.Clone(inst)
		mu.Unlock()
		w.Header().Set("ETag", `"instance"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"instance": resp})
	})

	cfg := providerConfig + `
resource "fivenines_instance" "test" {
  display_name   = "web-01"
  redis_password = "s3cret"
}`
	setCleared := func(v bool) func() {
		return func() {
			mu.Lock()
			dashboardCleared = v
			mu.Unlock()
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check:  resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
			},
			{
				// The operator clears the credential in the dashboard: the
				// refresh must surface a diff, not report "No changes".
				PreConfig:          setCleared(true),
				Config:             cfg,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				// Applying re-sends the credential and the plan settles.
				PreConfig: setCleared(false),
				Config:    cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password", "s3cret"),
					resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

// The port attributes carry the 1-65535 range check; an out-of-range value
// dies at validation, before anything reaches the wire.
func TestInstancePlan_PortOutOfRangeIsRejected(t *testing.T) {
	planTest(t, instanceHandler(t))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_instance" "test" {
  display_name = "web-01"
  redis_port   = 0
}`,
				ExpectError: regexp.MustCompile(`value must be between 1 and 65535`),
			},
		},
	})
}

// The no-clobber contract on the wire: an update that changes one attribute
// must PATCH only the managed keys, never the unconfigured Optional+Computed
// settings with their prior state values — those are unknown at plan time
// and omitted, so a dashboard edit racing between refresh and apply is never
// silently overwritten. This is the framework behavior the whole
// server-owned-defaults design leans on; if a framework upgrade changes it,
// this fails before a practitioner's host does.
func TestInstancePlan_UnrelatedUpdateOmitsUnmanagedSettings(t *testing.T) {
	var mu sync.Mutex
	inst := freshInstanceBody()
	var patchBodies []map[string]interface{}
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mu.Lock()
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			var body map[string]map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method == http.MethodPatch {
				patchBodies = append(patchBodies, body["instance"])
			}
			for key, value := range body["instance"] {
				if slices.Contains(instanceSecretKeys, key) {
					inst[key+"_set"] = true
					continue
				}
				inst[key] = value
			}
		}
		resp := maps.Clone(inst)
		mu.Unlock()
		w.Header().Set("ETag", `"instance"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"instance": resp})
	})

	cfg := func(name string) string {
		return providerConfig + `
resource "fivenines_instance" "test" {
  display_name   = "` + name + `"
  docker_enabled = true
}`
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg("web-01")},
			{Config: cfg("web-01-renamed")},
		},
	})

	mu.Lock()
	defer mu.Unlock()
	if len(patchBodies) == 0 {
		t.Fatal("expected the rename to PATCH")
	}
	for _, body := range patchBodies {
		for _, key := range []string{"tsdb_verify_ssl", "caddy_admin_api_url", "proxmox_port", "zfs_enabled", "postgresql_host"} {
			if v, present := body[key]; present {
				t.Errorf("update PATCH carried unmanaged %q = %v — prior state written back would clobber concurrent dashboard edits", key, v)
			}
		}
		if _, present := body["docker_enabled"]; !present {
			t.Error("update PATCH must still carry the configured docker_enabled")
		}
	}
}

// Adding a secret to an instance that exists without one flips the paired
// Computed `_set` boolean false → true across an UPDATE — the one transition
// where its value changes after the plan. It works because the framework
// marks computed-with-null-config attributes unknown on update; a future
// plan modifier on the `_set` attributes (say UseStateForUnknown to quiet
// plans) would break exactly this path while every other test stayed green.
func TestInstancePlan_AddingASecretToAnExistingInstance(t *testing.T) {
	planTest(t, instanceHandler(t))

	without := providerConfig + `
resource "fivenines_instance" "test" {
  display_name = "web-01"
}`
	with := providerConfig + `
resource "fivenines_instance" "test" {
  display_name   = "web-01"
  redis_password = "s3cret"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: without,
				Check:  resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "false"),
			},
			{
				Config: with,
				Check:  resource.TestCheckResourceAttr("fivenines_instance.test", "redis_password_set", "true"),
			},
			{Config: with, PlanOnly: true},
		},
	})
}

// A server that silently DROPS a written setting — the real API's documented
// behavior for Linux-only collectors on a confirmed Windows host. The apply
// must fail (Terraform's consistency check) rather than record a value the
// server refused; this pins the failure shape the resource description
// promises ("setting one there fails the apply").
func TestInstancePlan_ServerDroppedSettingFailsTheApply(t *testing.T) {
	var mu sync.Mutex
	inst := freshInstanceBody()
	planTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mu.Lock()
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			var body map[string]map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			for key, value := range body["instance"] {
				if key == "zfs_enabled" {
					continue // Windows host: the API drops the Linux-only key
				}
				inst[key] = value
			}
		}
		resp := maps.Clone(inst)
		mu.Unlock()
		w.Header().Set("ETag", `"instance"`)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"instance": resp})
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
resource "fivenines_instance" "test" {
  display_name = "web-01"
  zfs_enabled  = true
}`,
				ExpectError: regexp.MustCompile(`inconsistent result after apply`),
			},
		},
	})
}

// host_group_id is Optional-only: unlike the settings, removing it from the
// configuration is a change — the PATCH carries an explicit null and the host
// leaves its group. Optional+Computed here would make the membership sticky,
// with no way to unassign through Terraform at all.
func TestInstancePlan_RemovingHostGroupClearsIt(t *testing.T) {
	planTest(t, instanceHandler(t))

	grouped := providerConfig + `
resource "fivenines_instance" "test" {
  display_name  = "web-01"
  host_group_id = 7
}`
	ungrouped := providerConfig + `
resource "fivenines_instance" "test" {
  display_name = "web-01"
}`
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: grouped,
				Check:  resource.TestCheckResourceAttr("fivenines_instance.test", "host_group_id", "7"),
			},
			{
				Config: ungrouped,
				Check:  resource.TestCheckNoResourceAttr("fivenines_instance.test", "host_group_id"),
			},
			{Config: ungrouped, PlanOnly: true},
		},
	})
}
