# Terraform Provider for FiveNines

Manage your [FiveNines](https://fivenines.io) monitoring infrastructure as code.

## Resources

| Resource | Description |
|----------|-------------|
| `fivenines_instance` | Server/host instances |
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

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `fivenines_probe_regions` | Available probe regions for uptime monitors |
| `fivenines_integrations` | Configured notification integrations |
| `fivenines_workflow_runs` | Workflow execution history |
| `fivenines_workflow_templates` | Prebuilt workflow templates, instantiable by slug |
| `fivenines_workflow_node_types` | Node types available to workflow execution graphs |
| `fivenines_incidents` | Incidents triggered by workflows |
| `fivenines_uptime_monitors` | Uptime monitors, filterable by status, protocol, search text and update time |
| `fivenines_uptime_monitor_status` | Lightweight current status of a single uptime monitor |

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
```

### 4. Apply

```bash
terraform init    # download the provider
terraform plan    # preview changes
terraform apply   # create resources
```

## Authentication

The API key can be provided in three ways (in order of precedence):

1. Provider configuration: `api_key = "fn_live_..."`
2. Environment variable: `export FIVENINES_API_KEY="fn_live_..."`
3. Terraform variable: `var.fivenines_api_key`

## Secrets in state

Terraform stores every attribute it manages in the state file, including the
sensitive ones. `Sensitive: true` only redacts a value in CLI output; it does
not keep it out of state. Treat the state file as a secret and use a backend
that encrypts it and restricts who can read it.

What ends up there:

| Attribute | Why it is in state |
|-----------|--------------------|
| `fivenines_task.ping_key` / `ping_url` | Server-generated; the API returns the key on every read, and `ping_url` embeds it. This is what you feed to the job that pings the task, so it has to be readable as an output. |
| `fivenines_network_device.snmp_community`, `snmp_auth_password`, `snmp_priv_password` | Write-only — never returned by the API, so state holds the value you configured. |
| `fivenines_integration.url`, `secret`, `routing_key`, `user_key`, `app_token` | Write-only — the API never serializes an integration's metadata, so state holds the value you configured. |
| `fivenines_integration.webhook_signing_secret`, `webhook_verification_token` | Returned once, by the create call, and never readable again. State is the **only** copy: lose it and the signing key has to be rotated by replacing the channel. |

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

## Development

```bash
make build      # compile the provider
make test       # run unit tests
make testacc    # run acceptance tests (requires API key, see below)
make install    # install locally for testing
make docs       # regenerate registry documentation
```

`make docs` regenerates `docs/` from the Go schema descriptions and `examples/`.
Never hand-edit `docs/` — CI regenerates it and fails on any diff. From a checkout
not named `terraform-provider-fivenines` (a git worktree, for example) run
`tfplugindocs generate --provider-name fivenines --rendered-provider-name terraform-provider-fivenines`
instead; the bare command infers the wrong provider name, deletes `docs/`, and fails.

### Acceptance tests

Unit tests pin the shapes the provider *believes* the API has. Only the
acceptance tests notice when the API changes underneath it, so they drive real
Terraform plans and applies against a live organisation:

```bash
TF_ACC=1 FIVENINES_API_KEY=fn_live_... make testacc
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
