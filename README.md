# Terraform Provider for FiveNines

Manage your [FiveNines](https://fivenines.io) monitoring infrastructure as code.

## Resources

| Resource | Description |
|----------|-------------|
| `fivenines_instance` | Server/host instances, and every collector setting on them |
| `fivenines_task` | Cron & heartbeat monitors |
| `fivenines_uptime_monitor` | HTTP/HTTPS/TCP/ICMP/DNS uptime checks |
| `fivenines_workflow` | Automation workflows with version management |
| `fivenines_network_device` | SNMP-monitored network devices |
| `fivenines_status_page` | Public status pages with items |
| `fivenines_status_page_maintenance_window` | Scheduled maintenance announcements on a status page |
| `fivenines_integration` | Notification channels (webhook, PagerDuty, Pushover) |
| `fivenines_host_group` | Named groups of hosts |
| `fivenines_mqtt_broker` | MQTT brokers watched by one of your agent hosts |
| `fivenines_mqtt_topic_monitor` | Per-topic freshness & payload checks on a broker |
| `fivenines_api_token` | API tokens for this provider, so key rotation is code |
| `fivenines_enrollment_token` | Bootstrap credentials so hosts self-enroll |
| `fivenines_dashboard` | Dashboards, empty or built from a gallery template |
| `fivenines_dashboard_section` | Collapsible sections (Grafana-style rows) on a dashboard |
| `fivenines_dashboard_visualization` | Panels: 10 chart types over instances, monitors, tasks, devices and org-wide metrics |
| `fivenines_organization` | Organization settings (singleton — rename only) |
| `fivenines_organization_member` | Member roles, and offboarding on destroy |
| `fivenines_organization_invitation` | Invitations to join the organization |

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `fivenines_probe_regions` | Available probe regions for uptime monitors |
| `fivenines_integrations` | Notification integrations, filterable by type, enabled flag, name and update time |
| `fivenines_workflow_runs` | Workflow execution history (run headers), filterable by status and update time |
| `fivenines_workflow_run` | A single workflow run with its per-step detail |
| `fivenines_workflow_templates` | Prebuilt workflow templates, instantiable by slug |
| `fivenines_workflow_node_types` | Node types available to workflow execution graphs |
| `fivenines_incidents` | Incidents, filterable by status, subject, active window and update time |
| `fivenines_uptime_monitors` | Uptime monitors, filterable by status, protocol, name and update time |
| `fivenines_tasks` | Cron & heartbeat tasks, filterable by status, schedule type, name and update time, with a `limit` |
| `fivenines_uptime_monitor_status` | Lightweight current status of a single uptime monitor |
| `fivenines_instance_capability_status` | What one agent can actually collect, as opposed to what is switched on |
| `fivenines_dashboard_templates` | The dashboard gallery, with an availability verdict per template |
| `fivenines_host_groups` | Host groups, filterable by name, for looking up a group ID |
| `fivenines_status_page_subscribers` | A status page's email subscribers (PII: needs the `status_pages: update` permission) |
| `fivenines_organization` | Organization identity, effective plan and seat accounting |
| `fivenines_organization_members` | Member roster with per-person two-factor state |
| `fivenines_organization_security` | Two-factor policy and enrollment counters (read-only by design) |
| `fivenines_organization_saml` | SAML SSO posture and IdP certificate expiry (read-only by design) |
| `fivenines_vulnerabilities` | CVE findings org-wide, or scoped to one instance or container image |
| `fivenines_organization_docker_images` | Org-wide container image inventory, scan posture and blast radius |
| `fivenines_uptime` | Availability over a window — the SLA number |
| `fivenines_ssl_status` | Days until each monitored TLS certificate expires |
| `fivenines_metric_query` | Any metric in the catalogue, for instances, monitors and network devices |
| `fivenines_incident_stats` | Organization-wide incident count, MTTR and MTTA |
| `fivenines_cve_stats` | Organization-wide vulnerability counts |

The five metrics data sources are re-read on every `terraform plan`, so their
values change between runs by design. Keep windows relative
(`timeadd(timestamp(), "-720h")`) and treat the results as ephemeral: they are
meant for `output`, `locals` and `precondition` / `check` gates, not for
resource arguments, which would take a permanent diff.

### Per-instance collector inventories

The state rows behind each host's dashboard tabs — what a metrics query can
count but never name. Every one of these takes an `instance_id` and returns a
`collector` block alongside the rows, because an empty list on its own cannot
distinguish "this host genuinely runs none" from "the collector is switched
off" (which deletes the rows) from "this agent is too old to report them".

| Data Source | Description |
|-------------|-------------|
| `fivenines_systemd_units` | Failed units, systemd's verdict, and the journal tail |
| `fivenines_docker_containers` | Container state, exit codes, restart loops |
| `fivenines_docker_images` | Deployed images and their scan verdict (this instance's slice) |
| `fivenines_listening_ports` | What is listening, loopback sockets included |
| `fivenines_smart_devices` | smartctl PASSED / FAILED verdicts per drive |
| `fivenines_zfs_pools` | Pool health, resilver progress, scrub results |
| `fivenines_raid_arrays` | mdadm arrays, failed members, rebuild progress |
| `fivenines_temperature_sensors` | Sensor inventory and declared thresholds |
| `fivenines_nvidia_gpus` | Cards, utilization, and compute processes |
| `fivenines_fail2ban_jails` | Jails, ban counts, and banned addresses |
| `fivenines_php_fpm_pools` | Pool process state and worker exhaustion |
| `fivenines_rabbitmq_queues` | Queue depth, consumers, starvation |
| `fivenines_rabbitmq_nodes` | Broker node resource alarms |
| `fivenines_haproxy_backends` | Per-backend status and member tallies |
| `fivenines_haproxy_servers` | Per-member status and health-check verdicts |
| `fivenines_wireguard_peers` | Per-peer tunnel state and handshake age |
| `fivenines_qemu_vms` | libvirt domains, including vanished tombstones |
| `fivenines_proxmox_guests` | Cluster guests and their current node |
| `fivenines_proxmox_nodes` | Cluster nodes the cluster can no longer see |
| `fivenines_proxmox_storages` | Cluster storages, per node |

```hcl
data "fivenines_systemd_units" "failed" {
  instance_id  = fivenines_instance.web.id
  active_state = "failed"
}

# "No failed units" is only an all-clear if we were actually looking.
output "units_are_healthy" {
  value = (
    data.fivenines_systemd_units.failed.collector.enabled &&
    data.fivenines_systemd_units.failed.collector.supported &&
    length(data.fivenines_systemd_units.failed.systemd_units) == 0
  )
}
```

Null is never zero on these rows: a null `scrub_errors` means nobody has ever
checked, a null `vulnerability_count` means never scanned, a null
`oom_kill_count` means cgroup v1 cannot see it. Rows the dashboard hides —
QEMU tombstones, stale Proxmox rows, loopback sockets — are returned and
flagged rather than filtered out.

### Ceph and Proxmox cluster inventory

Clusters are organization-owned, not host-owned: one cluster has one set of
nodes, guests and storages however many hosts report it, because only the
elected authoritative reporter writes them. The per-instance data sources above
reach a cluster *through a host you already know*; these enumerate the fleet.

| Data Source | Description |
|-------------|-------------|
| `fivenines_ceph_clusters` | Ceph clusters with health derived from their reporter set |
| `fivenines_ceph_cluster` | One cluster by fsid, plus the per-host reporter breakdown |
| `fivenines_proxmox_clusters` | Proxmox VE clusters with the quorum verdict and rollups |
| `fivenines_proxmox_cluster` | One cluster by id, plus the per-host reporter breakdown |
| `fivenines_proxmox_cluster_nodes` | One cluster's nodes, and which the cluster cannot see |
| `fivenines_proxmox_cluster_guests` | One cluster's VMs and containers |
| `fivenines_proxmox_cluster_storages` | One cluster's storages, keyed per node |
| `fivenines_organization_proxmox_guests` | Every guest in the fleet, across every cluster |

**The verdict always travels with its provenance, and you need all of it.**
Both `health` and `quorate` are recomputed at read from the *fresh* reporters —
never a stored winner, because a complete-but-old "healthy" scrape would
otherwise beat a fresh "it is down" one forever. So neither field alone can say
whether anyone is watching:

```hcl
data "fivenines_proxmox_clusters" "all" {}

# quorate is THREE-VALUED and the null is load-bearing: it means UNKNOWN, never
# "lost". A consumer that treats null as false pages on a monitoring outage.
output "lost_quorum" {
  value = [
    for c in data.fivenines_proxmox_clusters.all.proxmox_clusters :
    c.name if c.quorate == false
  ]
}
```

A Ceph cluster's `health` is `null` until it is promoted past the anti-phantom
gate — a single-reporter cluster is usually a stale `ceph.conf` on a cloned
image, so the product refuses to vouch for it and so does the provider. Neither
list offers a server-side `health` or `quorate` filter: the verdict is a fold
over the reporter set, and a second implementation of it in SQL is how such a
filter starts disagreeing with the field each row publishes.

## Quick Start

### 1. Get an API key

Go to **Settings > API** in your FiveNines dashboard and create an API key.

### 2. Configure the provider

```hcl
terraform {
  required_providers {
    fivenines = {
      source  = "Five-Nines-io/fivenines"
      version = "~> 0.6"
    }
  }
}

provider "fivenines" {
  api_key = var.fivenines_api_key  # or set FIVENINES_API_KEY env var
}
```

### 3. Define your monitoring

```hcl
# Monitor an API endpoint
resource "fivenines_uptime_monitor" "api" {
  name     = "Production API"
  url      = "https://api.example.com/health"
  protocol = "https"
  interval_seconds = 60
  probe_region_ids = [1, 2]
}

# Track a cron job
resource "fivenines_task" "backup" {
  name          = "Nightly DB Backup"
  schedule_type = "cron"
  schedule      = "0 2 * * *"
  grace_period_minutes = 5
}

# Notify a webhook — workflow notification nodes reference it by integration_id
resource "fivenines_integration" "ops_webhook" {
  type = "webhook"
  name = "Ops hook"
  url  = "https://example.com/hooks/fivenines"
}

# Create a workflow with execution graph
resource "fivenines_workflow" "alert" {
  name             = "API Down Alert"
  description      = "Notify team when API is unreachable"
  interval_seconds = 60
  active           = true

  execution_graph_json = file("workflow.json")
}

# Monitor a network switch via SNMP
resource "fivenines_network_device" "switch" {
  name           = "Core Switch"
  ip_address     = "192.168.1.1"
  snmp_version   = "v2c"
  snmp_community = var.snmp_community
}

# Group hosts by environment
resource "fivenines_host_group" "production" {
  name     = "Production"
  position = 1
}

# Watch an MQTT broker from an agent host inside the network
resource "fivenines_mqtt_broker" "factory" {
  name            = "Factory-floor Mosquitto"
  host            = "mqtt.internal"
  port            = 8883
  tls             = true
  username        = var.mqtt_username
  password        = var.mqtt_password
  watcher_host_id = fivenines_instance.edge_gateway.id
}

# Alert when a sensor topic goes quiet
resource "fivenines_mqtt_topic_monitor" "temperature" {
  mqtt_broker_id      = fivenines_mqtt_broker.factory.id
  topic_filter        = "sensors/+/temperature"
  stale_after_seconds = 300
}

# Create a public status page
resource "fivenines_status_page" "public" {
  name     = "Service Status"
  public   = true
  sections = ["Core services"]

  # items is a list attribute, not a block: assign it with `=`, and the list
  # order is the display order.
  items = [
    {
      item_type     = "UptimeMonitor"
      item_id       = fivenines_uptime_monitor.api.id
      display_label = "Public API"
      section       = "Core services"
    },
  ]
}

# Announce planned maintenance on that status page
resource "fivenines_status_page_maintenance_window" "db_upgrade" {
  status_page_id = fivenines_status_page.public.id
  title          = "Database upgrade"
  body           = "The API stays available in read-only mode."
  starts_at      = "2026-09-20T22:00:00Z"
  ends_at        = "2026-09-21T02:00:00Z"

  affected_items = [
    {
      item_type = "UptimeMonitor"
      item_id   = fivenines_uptime_monitor.api.id
    },
  ]
}

# Mint a bootstrap credential so hosts enroll themselves
resource "fivenines_enrollment_token" "web_fleet" {
  name = "web fleet"
}

# Build a dashboard. Sections and panels are their own resources: the API keeps
# them on separate endpoints so that renaming a dashboard can never silently
# delete a section it did not mention.
resource "fivenines_dashboard" "fleet" {
  name        = "Fleet health"
  description = "Everything on one screen"
}

resource "fivenines_dashboard_section" "compute" {
  dashboard_id = fivenines_dashboard.fleet.id
  name         = "Compute"
  position     = 0
}

resource "fivenines_dashboard_visualization" "cpu" {
  dashboard_id = fivenines_dashboard.fleet.id
  section      = fivenines_dashboard_section.compute.name
  title        = "CPU usage"
  metric       = "cpu_usage"
  chart_type   = "line"

  targets = {
    hosts = [fivenines_instance.web.id]
  }

  options = {
    incident_overlay = true
  }
}
```

See [`examples/golden-dashboard`](examples/golden-dashboard) for the same layout
packaged as a module and instantiated once per environment.

### 4. Manage the team

Onboarding is two resources. The invitation sends the email and holds a seat; the
member resource takes over once the person accepts, because the API has no endpoint
that creates a member.

```hcl
resource "fivenines_organization_invitation" "new_hire" {
  email = "newhire@acme.com"
  role  = "member"
}

# Adopts an existing membership and manages its role.
resource "fivenines_organization_member" "lead" {
  email = "lead@acme.com"
  role  = "admin"
}
```

**Destroying a `fivenines_organization_member` offboards the person.** The API
removes the membership and deletes the user account in one transaction, which also
destroys every API token that user owned — so the key this provider authenticates
with must not belong to the person being removed. Terraform pre-validates removals
and role changes with an `X-Dry-Run` request during `terraform plan`, so the API's
refusals (removing yourself, removing the owner, a read-only key) surface before an
apply starts. If you plan with a different key than you apply with, set
`skip_plan_validation = true` on the provider — or export
`FIVENINES_SKIP_PLAN_VALIDATION=true` — to turn that pre-flight off.

The two-factor policy and the SAML configuration are **read-only over the API by
design**: the server refuses those writes with a `403` on every plan and scope, so a
stolen token cannot disarm the control that makes a stolen password survivable, or
repoint the organization's identity provider. They are exposed as the
`fivenines_organization_security` and `fivenines_organization_saml` data sources —
`idp_certificate_expires_at` is worth an alert, since nothing else surfaces it and
every member is locked out at once when it passes. Change both in the dashboard.

### 5. Apply

```bash
terraform init    # download the provider
terraform plan    # preview changes
terraform apply   # create resources
```

## Authentication

The API key can be provided in three ways (in order of precedence):

1. Provider configuration: `api_key = "fn_..."`
2. Environment variable: `export FIVENINES_API_KEY="fn_..."`
3. Terraform variable: `var.fivenines_api_key`

**The token must be write-scoped.** Tokens carry `read` and/or `write` scopes,
and every create, update and delete this provider performs requires `write`. A
read-scoped token lets `terraform plan` and every data source succeed, then
fails the first `apply` with a 403.

`401` and `403` are not interchangeable:

- **401 — the credential is dead.** Membership is resolved from the *token's*
  organization, never the holder's latest one, and the API fails the token
  closed: once the user who minted it is no longer a member of that
  organization, every request answers 401. No permission change repairs it —
  mint a new token.
- **403 — the credential is valid, the action is refused.** A read-scoped token
  on a write, an authorization refusal, or a demo-restricted organization.
- **402 — the organization is suspended or unpaid.** Writes only; reads keep
  working, so `plan` and `refresh` still succeed.

### Rate limits

Limits are per organization, per minute, and plan-tiered (starter 300 →
enterprise 2400). The provider retries a `429` automatically, honouring
`Retry-After`. **The budget is shared with the FiveNines MCP server** — one
counter serves both — so a busy MCP client competes with a Terraform run on the
same organization. Lower `-parallelism` if you hit sustained 429s.

A second, tighter ceiling sits underneath it: `/api/v1/*` is **also throttled per
IP, at 20 requests/minute**, on every plan. From a single CI runner that is the
binding limit, not the plan tier. It bites hardest on unfiltered list data
sources, which walk one request per 100 rows — 5,000 tasks is 50 requests, so one
refresh crosses the per-IP ceiling twice and the provider sleeps out each
`Retry-After`. Filter server-side rather than in HCL, and cap the read where the
data source offers a `limit` (today, `fivenines_tasks`).

### Reporting a failure

Every diagnostic carries the API's request ID, which correlates your failed
apply with the server-side logs:

```
API error 422: [Display name can't be blank] (request_id: 9f1c4e2a-...)
```

Quote that `request_id` in support tickets.
### Rotating the key

API keys are themselves a resource, so rotation can be a scheduled apply instead
of a browser errand:

```hcl
resource "fivenines_api_token" "deploy" {
  name       = "deploy-${time_rotating.key.id}"
  scopes     = ["write"]
  expires_at = timeadd(time_rotating.key.rotation_rfc3339, "168h")

  lifecycle {
    create_before_destroy = true
  }
}
```

Three things to know before you use it:

- **The value is returned once.** `token` is readable in state and outputs, but
  the server keeps only a digest — nothing can hand it back later. An imported
  token has no value at all.
- **`create_before_destroy` is the rotation order.** Create the replacement,
  deploy it, then revoke the old one. Without it Terraform revokes first and
  everything holding the old key fails in the gap.
- **Managing tokens needs a `write`-scoped key**, and no token can grant a scope
  it does not hold itself. Destroying the token the provider is authenticated
  with is refused unless you set `allow_self_revoke = true`.

## Secrets in state

Terraform stores every attribute it manages in the state file, including the
sensitive ones. `Sensitive: true` only redacts a value in CLI output; it does
not keep it out of state. Treat the state file as a secret and use a backend
that encrypts it and restricts who can read it.

What ends up there:

| Attribute | Why it is in state |
|-----------|--------------------|
| `fivenines_task.ping_key` / `ping_url` | Server-generated; the API returns the key on every read, and `ping_url` embeds it. This is what you feed to the job that pings the task, so it has to be readable as an output. |
| `data.fivenines_tasks.<name>.tasks[*].ping_key` / `ping_url` | The same secret, for every task the filters matched — including tasks this configuration does not manage. Narrow the filters if you only need one. |
| `fivenines_network_device.snmp_community`, `snmp_auth_password`, `snmp_priv_password` | Write-only — never returned by the API, so state holds the value you configured. |
| `fivenines_instance.redis_password`, `postgresql_password`, `mysql_password`, `rabbitmq_password`, `haproxy_password`, `proxmox_token_secret`, `tsdb_auth_header_value`, `tsdb_basic_auth_password`, `vllm_auth_header_value`, `sglang_auth_header_value` | Write-only — the API never returns a collector credential, so state holds the value you configured. The paired `_set` booleans report whether the server holds one. |
| `fivenines_integration.url`, `secret`, `routing_key`, `user_key`, `app_token` | Write-only — the API never serializes an integration's metadata, so state holds the value you configured. |
| `fivenines_integration.webhook_signing_secret`, `webhook_verification_token` | Returned once, by the create call, and never readable again. State is the **only** copy: lose it and the signing key has to be rotated by replacing the channel. |
| `fivenines_api_token.token` | Returned once, by the create call. The server keeps only a SHA-256 digest, so state is the **only** copy — and unlike the rows above, it is a key to the API itself. Anyone who can read the state file can act as that token, up to its scopes. An imported token has no value here at all. |

### Upgrading: unset attributes are now null, not `""`

Attributes the FiveNines agent populates used to read back as `""` before the
agent had ever reported. They are `null` now, which is what the API actually
says. That is the point — `""` made every plan drift — but it changes what a
config that interpolates them sees:

| Resource | Attributes |
|---|---|
| `fivenines_instance` | `hostname`, `operating_system_name`, `kernel_version`, `cpu_architecture`, `cpu_model`, `cpu_count`, `memory_size`, `ipv4`, `ipv6`, `source`, `client_version` |
| `fivenines_network_device` | `status`, `vendor`, `model`, `sys_name` |
| `fivenines_task` | `schedule` on an interval task, `host_id` when unset |

An expression like `"${fivenines_instance.web.hostname}.internal"` used to
produce `".internal"` for a host that had never synced; it now fails the plan
with "Invalid template interpolation value". That is the honest answer, but if
you want the old behaviour, wrap it:

```hcl
coalesce(fivenines_instance.web.hostname, "")
```

Hosts that have synced are unaffected — the API returns real values and nothing
about them changes.

### Upgrading: `ping_url` is now sensitive

`fivenines_task.ping_url` embeds `ping_key` verbatim, so it is marked
`Sensitive` from this release on. Terraform refuses to expose a sensitive value
through a root-module output unless the output says so, so if you export it,
add one line:

```hcl
output "backup_ping_url" {
  value     = fivenines_task.backup.ping_url
  sensitive = true   # required from this release on
}
```

Nothing else changes: the value is identical and still readable by anything that
consumes the output.

`ping_key` is deliberately kept in state rather than made write-only or
ephemeral. It is a Computed value, and Terraform's write-only arguments apply to
values the practitioner supplies, not to ones the server generates. An ephemeral
resource could surface it without persisting it — that needs
terraform-plugin-framework v1.13+ and Terraform 1.10+, and this provider pins
v1.12 — but an ephemeral value cannot be referenced from an output, which is how
you actually get `ping_url` to the job that pings the task. The exposure is
bounded: the key authenticates heartbeat pings for a single task and grants no
read access and no ability to change configuration. If a key leaks, replace the
task to issue a new one.

### Upgrading: uptime monitor rules are checked at plan time

`fivenines_uptime_monitor` used to accept configurations the API then rejected
with a 422 or silently rewrote. From this release the provider mirrors the
server's own validations, so those configurations fail during `terraform plan`
with a message naming the attribute, instead of failing partway through an
apply. A valid monitor is unaffected; what changes is when an invalid one is
caught.

| Configuration | Why it is refused |
|---|---|
| `protocol = "dns"` without `hostname` | The hostname is the name being resolved, and the API requires it on every dns write. |
| `custom_body` or `content_type` beside a `GET` or `HEAD` | The API stores both only for a POST and clears them otherwise, so the value never survived the apply. Omitting `http_method` counts, because it defaults to `GET`. Set `http_method = "POST"`. |
| A blank or whitespace-padded entry in `dns_expected_records` | Stored verbatim and compared without trimming, so the entry can never match what DNS resolves. Wrap it in `trim()`; a trailing newline in a `split()` is the usual cause. |
| `timeout_seconds` outside 1-15, `recovery_count` outside 1-10, `interval_seconds` or `confirmation_count` below 1 | The API's own bounds, checked before the round trip. |
| More than 20 `custom_headers`, a header value over 4KB, or a header name outside `[A-Za-z0-9_-]` | The API's caps and its header-name rule. |

Two behaviour changes come with it.

**`dns_expected_records` and `custom_headers` now really clear.** Removing
either attribute used to send a JSON null that Rails strong params dropped, so
the stored value survived and the apply failed with "Provider produced
inconsistent result after apply". The provider sends an empty collection now,
which the API accepts, so the apply succeeds. For `dns_expected_records` that
also clears the baseline the probe auto-seeds after a monitor's first successful
check, and the seed is written once and never again. Pin the records explicitly
if you want them.

**A rejected `custom_headers` value is no longer printed.** That map is where an
`Authorization` header for the monitored endpoint goes, and Terraform redacts a
sensitive value in the plan diff but not inside a provider diagnostic. Its
validator reports the offending character and where it is, never the value, so a
bearer token with a trailing newline no longer echoes itself into `terraform
plan` output and CI logs.

## Importing existing resources

Import resources created outside of Terraform:

```bash
terraform import fivenines_instance.web <instance-uuid>
terraform import fivenines_task.backup <task-uuid>
terraform import fivenines_uptime_monitor.api <monitor-uuid>
terraform import fivenines_workflow.alert <workflow-id>
terraform import fivenines_network_device.switch <device-uuid>
terraform import fivenines_status_page.public <status-page-id>
terraform import fivenines_status_page_maintenance_window.db_upgrade <status-page-id>:<window-id>
terraform import fivenines_host_group.production <host-group-id>
terraform import fivenines_mqtt_broker.factory <broker-uuid>

# Topic monitors live under their broker, so both UUIDs are part of the ID
terraform import fivenines_mqtt_topic_monitor.temperature <broker-uuid>:<monitor-uuid>

# API tokens are the one numeric id here, and import brings the metadata only:
# `token` stays null, because the value was readable exactly once
terraform import fivenines_api_token.ci <api-token-id>

# Enrollment tokens are numeric too, and import is metadata-only for the same
# reason: `token` and `install_command` stay null, because the value was
# readable exactly once
terraform import fivenines_enrollment_token.fleet <enrollment-token-id>
terraform import fivenines_dashboard.fleet <dashboard-id>

# Sections and panels are addressed through their dashboard, so their import
# ids carry it.
terraform import fivenines_dashboard_section.compute <dashboard-id>:<section-id>
terraform import fivenines_dashboard_visualization.cpu <dashboard-id>:<panel-id>
terraform import fivenines_organization.this organization          # the ID is ignored
terraform import fivenines_organization_member.lead <membership-id-or-email>
terraform import fivenines_organization_invitation.new_hire <invitation-id>
```

`fivenines_integration` has no import: the API never returns an integration's
URL, routing key or tokens, so an imported channel would plan an immediate
destroy-and-recreate. Reference channels created elsewhere — including Slack,
Discord, Teams, Telegram and email, which cannot be created over the API at all —
with the `fivenines_integrations` data source.

An MQTT broker imports fine, but its `username` and `password` do not come with it:
the API never returns either, so they stay stored server-side and out of Terraform's
view. `username_set` and `password_set` report that a credential exists. Like the
SNMP secrets on `fivenines_network_device`, they are write-only and preserve on
omission — setting one rotates it, dropping it from the configuration leaves the
stored value alone. Clear a credential in the dashboard.

An instance imports its whole monitoring configuration, but not its ten collector
credentials: `redis_password`, `postgresql_password`, `mysql_password`,
`rabbitmq_password`, `haproxy_password`, `proxmox_token_secret`,
`tsdb_auth_header_value`, `tsdb_basic_auth_password`, `vllm_auth_header_value` and
`sglang_auth_header_value` are write-only on exactly those terms, each paired with a
`<name>_set` boolean reporting whether the server holds one. Collector settings you
do not put in the configuration keep their server-side values, so adopting an
instance does not flatten choices made in the dashboard.

## Development

```bash
make build      # compile the provider
make test       # run unit tests
make testacc    # run acceptance tests (requires API key, see below)
make install    # install locally for testing
make docs       # regenerate registry documentation
```

`make docs` regenerates `docs/` from the Go schema descriptions and `examples/`.
Never hand-edit `docs/` — CI regenerates it and fails on any diff. Use the make
target rather than calling `tfplugindocs generate` yourself: the target pins
`--provider-name fivenines --rendered-provider-name terraform-provider-fivenines`,
and without those flags tfplugindocs infers the provider name from the directory
name, so from a checkout not named `terraform-provider-fivenines` (a git worktree,
for example) the bare command deletes `docs/` and then fails mid-render.

### Acceptance tests

Unit tests pin the shapes the provider *believes* the API has. Only the
acceptance tests notice when the API changes underneath it, so they drive real
Terraform plans and applies against a live organisation:

```bash
TF_ACC=1 FIVENINES_API_KEY=fn_... make testacc
```

They need the `terraform` CLI on `PATH`, and they skip entirely without
`TF_ACC` — `make test` stays offline and fast. **Point them at a dedicated
staging organisation**: every test creates and destroys real instances,
monitors, tasks, devices, status pages and workflows. Resource names are
randomised with a `tf-acc-` prefix so parallel runs do not collide.

CI runs them nightly and on pushes to `main` (never on pull requests — a fork
must not see the key) via `.github/workflows/acceptance.yml`.

## Publishing

Releases are automated via GitHub Actions. The git tag *is* the version — there is no `VERSION` file and no changelog to update. To create a release:

```bash
git tag v0.6.0
git push origin v0.6.0
```

This triggers GoReleaser to build cross-platform binaries, sign checksums with GPG, and create a GitHub release, with release notes generated from the commit subjects since the last tag. The Terraform Registry picks up new releases automatically.

### Required GitHub Secrets

| Secret | Description |
|--------|-------------|
| `GPG_PRIVATE_KEY` | ASCII-armored GPG private key for signing releases |
| `GPG_PASSPHRASE` | Passphrase for the GPG key |
| `FIVENINES_API_KEY` | API key for the staging organisation the acceptance tests run against. Until it is set, the acceptance workflow reports that it skipped instead of failing. |

An optional `FIVENINES_BASE_URL` repository *variable* points the acceptance
tests at a non-production API.
