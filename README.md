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

### Reporting a failure

Every diagnostic carries the API's request ID, which correlates your failed
apply with the server-side logs:

```
API error 422: [Display name can't be blank] (request_id: 9f1c4e2a-...)
```

Quote that `request_id` in support tickets.

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
make testacc    # run acceptance tests (requires API key)
make install    # install locally for testing
make docs       # regenerate registry documentation
```

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
