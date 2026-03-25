# Terraform Provider for FiveNines

Manage your [FiveNines](https://fivenines.io) monitoring infrastructure as code.

## Resources

| Resource | Description |
|----------|-------------|
| `fivenines_instance` | Server/host instances |
| `fivenines_task` | Cron & heartbeat monitors |
| `fivenines_uptime_monitor` | HTTP/HTTPS/TCP/ICMP uptime checks |
| `fivenines_workflow` | Automation workflows |

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `fivenines_probe_regions` | Available probe regions for uptime monitors |
| `fivenines_integrations` | Configured notification integrations |

## Quick Start

### 1. Get an API key

Go to **Settings > API** in your FiveNines dashboard and create an API key.

### 2. Configure the provider

```hcl
terraform {
  required_providers {
    fivenines = {
      source  = "Five-Nines-io/fivenines"
      version = "~> 0.1"
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
  interval = 60

  probe_regions = ["us-east", "eu-west"]
}

# Track a cron job
resource "fivenines_task" "backup" {
  name          = "Nightly DB Backup"
  schedule_type = "cron"
  schedule      = "0 2 * * *"
  grace_period  = 300
}

# Create a workflow
resource "fivenines_workflow" "alert" {
  name        = "API Down Alert"
  description = "Notify team when API is unreachable"
  enabled     = true
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

## Importing existing resources

Import resources created outside of Terraform:

```bash
terraform import fivenines_instance.web <instance-uuid>
terraform import fivenines_task.backup <task-uuid>
terraform import fivenines_uptime_monitor.api <monitor-uuid>
terraform import fivenines_workflow.alert <workflow-id>
```

## Development

```bash
make build      # compile the provider
make test       # run unit tests
make testacc    # run acceptance tests (requires API key)
make install    # install locally for testing
```

## Publishing

Releases are automated via GitHub Actions. To create a release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

This triggers GoReleaser to build cross-platform binaries, sign checksums with GPG, and create a GitHub release. The Terraform Registry picks up new releases automatically.

### Required GitHub Secrets

| Secret | Description |
|--------|-------------|
| `GPG_PRIVATE_KEY` | ASCII-armored GPG private key for signing releases |
| `GPG_PASSPHRASE` | Passphrase for the GPG key |
