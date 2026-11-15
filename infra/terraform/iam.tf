# GKE worker nodes service account
resource "google_service_account" "gke_nodes" {
  account_id   = "sem-gke-nodes"
  display_name = "SEM GKE Worker Nodes"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "gke_nodes_roles" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
    "roles/monitoring.viewer",
    "roles/artifactregistry.reader",
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.gke_nodes.email}"
}

# Go API workload identity service account
resource "google_service_account" "api" {
  account_id   = "sem-api"
  display_name = "SEM Go API"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "api_roles" {
  for_each = toset([
    "roles/secretmanager.secretAccessor",
    "roles/iam.serviceAccountTokenCreator",
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.api.email}"
}

# PostGIS VM service account
resource "google_service_account" "db_vm" {
  account_id   = "sem-db-vm"
  display_name = "SEM PostGIS VM"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "db_vm_roles" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.db_vm.email}"
}

# Cloud Functions (lifecycle + cleanup) service account
resource "google_service_account" "functions" {
  account_id   = "sem-functions"
  display_name = "SEM Cloud Functions"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "functions_roles" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
    "roles/secretmanager.secretAccessor",
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.functions.email}"
}

# Cloud Scheduler OIDC invoker service account
resource "google_service_account" "scheduler" {
  account_id   = "sem-scheduler"
  display_name = "SEM Cloud Scheduler Invoker"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "scheduler_run_invoker" {
  project = var.project_id
  role    = "roles/run.invoker"
  member  = "serviceAccount:${google_service_account.scheduler.email}"
}

# OpenTelemetry Collector service account
resource "google_service_account" "otel_collector" {
  account_id   = "sem-otel-collector"
  display_name = "SEM OTel Collector"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "otel_collector_roles" {
  for_each = toset([
    "roles/monitoring.metricWriter",
    "roles/logging.logWriter",
    "roles/cloudtrace.agent",
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.otel_collector.email}"
}

# Grafana service account
resource "google_service_account" "grafana" {
  account_id   = "sem-grafana"
  display_name = "SEM Grafana"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "grafana_roles" {
  for_each = toset([
    "roles/monitoring.viewer",
    "roles/logging.viewer",
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.grafana.email}"
}

# Test controller service account
resource "google_service_account" "test_controller" {
  account_id   = "sem-test-controller"
  display_name = "SEM Test Controller"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "test_controller_compute" {
  project = var.project_id
  role    = "roles/compute.instanceAdmin.v1"
  member  = "serviceAccount:${google_service_account.test_controller.email}"
}

# Locust runner VM service account
resource "google_service_account" "locust_runner" {
  account_id   = "sem-locust-runner"
  display_name = "SEM Locust Runner VM"
  depends_on   = [google_project_service.apis]
}

resource "google_project_iam_member" "locust_runner_roles" {
  for_each = toset([
    "roles/monitoring.metricWriter",
    "roles/logging.logWriter",
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.locust_runner.email}"
}

# Allow test controller to attach the locust runner SA to created VMs
resource "google_service_account_iam_member" "controller_act_as_runner" {
  service_account_id = google_service_account.locust_runner.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.test_controller.email}"
}
