terraform {
  backend "gcs" {
    bucket = "sem-tfstate-project-72f15835-9e20-4691-877"
    prefix = "infra/terraform"
  }
}
