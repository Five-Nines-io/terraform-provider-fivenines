terraform {
  required_providers {
    fivenines = {
      source  = "Five-Nines-io/fivenines"
      version = "~> 0.4"
    }
  }
}

provider "fivenines" {
  # api_key comes from FIVENINES_API_KEY
}

variable "staging_host_ids" {
  type    = list(string)
  default = []
}

variable "production_host_ids" {
  type    = list(string)
  default = []
}

variable "staging_monitor_ids" {
  type    = list(string)
  default = []
}

variable "production_monitor_ids" {
  type    = list(string)
  default = []
}

module "staging_dashboard" {
  source = "./modules/golden_dashboard"

  environment        = "Staging"
  description        = "Golden dashboard, staging"
  host_ids           = var.staging_host_ids
  uptime_monitor_ids = var.staging_monitor_ids
}

module "production_dashboard" {
  source = "./modules/golden_dashboard"

  environment        = "Production"
  description        = "Golden dashboard, production"
  host_ids           = var.production_host_ids
  uptime_monitor_ids = var.production_monitor_ids
}

output "dashboard_ids" {
  value = {
    staging    = module.staging_dashboard.dashboard_id
    production = module.production_dashboard.dashboard_id
  }
}

# A dashboard holding more panels than the module declares means someone added
# one by hand. Panels are declarative here, so the next apply does not remove
# it - this is the signal to fold it into the module or delete it.
output "panel_counts" {
  value = {
    staging    = module.staging_dashboard.visualization_count
    production = module.production_dashboard.visualization_count
  }
}
