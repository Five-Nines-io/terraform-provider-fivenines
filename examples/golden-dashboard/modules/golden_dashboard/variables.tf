variable "environment" {
  description = "Environment name. Used to name the dashboard and its sections."
  type        = string
}

variable "host_ids" {
  description = "Instance UUIDs this environment's compute panels chart."
  type        = list(string)
  default     = []
}

variable "uptime_monitor_ids" {
  description = "Uptime monitor UUIDs this environment's availability panels chart."
  type        = list(string)
  default     = []
}

variable "description" {
  description = "Dashboard description."
  type        = string
  default     = null
}
