variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "Primary GCP region"
  type        = string
  default     = "europe-north2"
}

variable "zone" {
  description = "Primary GCP zone"
  type        = string
  default     = "europe-north2-a"
}

variable "scheduler_region" {
  description = "Cloud Scheduler region (Stockholm is not supported by Scheduler)"
  type        = string
  default     = "europe-west1"
}

variable "billing_account_id" {
  description = "GCP Billing Account ID (without billingAccounts/ prefix)"
  type        = string
}

variable "budget_amount" {
  description = "Monthly project budget alert threshold in billing account currency"
  type        = number
  default     = 5200
}

variable "budget_currency" {
  description = "Billing account currency code (must match the GCP billing account currency)"
  type        = string
  default     = "TRY"
}

variable "alert_email" {
  description = "Email address for budget and monitoring alerts"
  type        = string
}

variable "domain" {
  description = "Root domain name"
  type        = string
  default     = "socialeventmapper.com"
}
