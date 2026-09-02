terraform {
  required_providers {
    fivenines = {
      source  = "Five-Nines-io/fivenines"
      version = "~> 0.6"
    }
  }
}

provider "fivenines" {
  api_key = var.fivenines_api_key
  # base_url = "https://fivenines.io" # optional

  # Organization member changes are pre-validated with a dry-run request during
  # `terraform plan`, so the API's refusals surface before an apply starts. Skip
  # it when the key you plan with is not the key you apply with.
  # skip_plan_validation = true
}

variable "fivenines_api_key" {
  type      = string
  sensitive = true
}
