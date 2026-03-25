terraform {
  required_providers {
    fivenines = {
      source  = "Five-Nines-io/fivenines"
      version = "~> 0.1"
    }
  }
}

provider "fivenines" {
  api_key = var.fivenines_api_key
  # base_url = "https://fivenines.io" # optional
}

variable "fivenines_api_key" {
  type      = string
  sensitive = true
}
