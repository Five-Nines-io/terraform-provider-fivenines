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
| `fivenines_dashboard` | Dashboards, empty or built from a gallery template |
| `fivenines_dashboard_section` | Collapsible sections (Grafana-style rows) on a dashboard |
| `fivenines_dashboard_visualization` | Panels: 10 chart types over instances, monitors, tasks, devices and org-wide metrics |

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `fivenines_probe_regions` | Available probe regions for uptime monitors |
| `fivenines_integrations` | Configured notification integrations |
| `fivenines_workflow_runs` | Workflow execution history |
| `fivenines_incidents` | Incidents triggered by workflows |
| `fivenines_dashboard_templates` | The dashboard gallery, with an availability verdict per template |

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

## Importing existing resources

Import resources created outside of Terraform:

```bash
terraform import fivenines_instance.web <instance-uuid>
terraform import fivenines_task.backup <task-uuid>
terraform import fivenines_uptime_monitor.api <monitor-uuid>
terraform import fivenines_workflow.alert <workflow-id>
terraform import fivenines_network_device.switch <device-uuid>
terraform import fivenines_status_page.public <status-page-id>
terraform import fivenines_dashboard.fleet <dashboard-id>

# Sections and panels are addressed through their dashboard, so their import
# ids carry it.
terraform import fivenines_dashboard_section.compute <dashboard-id>:<section-id>
terraform import fivenines_dashboard_visualization.cpu <dashboard-id>:<panel-id>
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
