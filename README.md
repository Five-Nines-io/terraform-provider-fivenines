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

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `fivenines_probe_regions` | Available probe regions for uptime monitors |
| `fivenines_integrations` | Configured notification integrations |
| `fivenines_workflow_runs` | Workflow execution history |
| `fivenines_incidents` | Incidents triggered by workflows |

## Quick Start

### 1. Get an API key

Go to **Settings > API** in your FiveNines dashboard and create an API key.

### 2. Configure the provider

```hcl
terraform {
  required_providers {
    fivenines = {
      source  = "Five-Nines-io/fivenines"
      version = "~> 0.3"
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

# Create a public status page
resource "fivenines_status_page" "public" {
  name   = "Service Status"
  public = true

  items {
    item_type = "UptimeMonitor"
    item_id   = fivenines_uptime_monitor.api.id
  }
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

`ping_key` is deliberately kept in state rather than made write-only or
ephemeral. It is a Computed value: Terraform's write-only arguments apply to
values the practitioner supplies, not to ones the server generates, so there is
no mechanism that would let the provider expose a usable `ping_url` without
persisting it. The exposure is bounded — the key authenticates heartbeat pings
for a single task and grants no read access and no ability to change
configuration. If a key leaks, replace the task to issue a new one.

## Importing existing resources

Import resources created outside of Terraform:

```bash
terraform import fivenines_instance.web <instance-uuid>
terraform import fivenines_task.backup <task-uuid>
terraform import fivenines_uptime_monitor.api <monitor-uuid>
terraform import fivenines_workflow.alert <workflow-id>
terraform import fivenines_network_device.switch <device-uuid>
terraform import fivenines_status_page.public <status-page-id>
```

## Development

```bash
make build      # compile the provider
make test       # run unit tests
make testacc    # run acceptance tests (requires API key, see below)
make install    # install locally for testing
make docs       # regenerate registry documentation
```

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

Releases are automated via GitHub Actions. To create a release:

```bash
git tag v0.3.0
git push origin v0.3.0
```

This triggers GoReleaser to build cross-platform binaries, sign checksums with GPG, and create a GitHub release. The Terraform Registry picks up new releases automatically.

### Required GitHub Secrets

| Secret | Description |
|--------|-------------|
| `GPG_PRIVATE_KEY` | ASCII-armored GPG private key for signing releases |
| `GPG_PASSPHRASE` | Passphrase for the GPG key |
| `FIVENINES_API_KEY` | API key for the staging organisation the acceptance tests run against. Until it is set, the acceptance workflow reports that it skipped instead of failing. |

An optional `FIVENINES_BASE_URL` repository *variable* points the acceptance
tests at a non-production API.
