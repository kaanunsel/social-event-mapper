resource "google_container_cluster" "primary" {
  name     = "sem-gke"
  location = var.zone

  remove_default_node_pool = true
  initial_node_count       = 1
  deletion_protection      = false

  network    = google_compute_network.vpc.id
  subnetwork = google_compute_subnetwork.main.id

  ip_allocation_policy {
    cluster_secondary_range_name  = "gke-pods"
    services_secondary_range_name = "gke-services"
  }

  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = false
    master_ipv4_cidr_block  = "172.16.0.0/28"
  }

  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  gateway_api_config {
    channel = "CHANNEL_STANDARD"
  }

  addons_config {
    http_load_balancing {
      disabled = false
    }
  }

  release_channel {
    channel = "REGULAR"
  }

  depends_on = [
    google_project_service.apis,
    google_compute_router_nat.nat,
  ]
}

resource "google_container_node_pool" "primary_nodes" {
  name       = "sem-pool"
  location   = var.zone
  cluster    = google_container_cluster.primary.name
  node_count = 2

  autoscaling {
    min_node_count = 2
    max_node_count = 4
  }

  node_config {
    machine_type = "e2-medium"
    disk_size_gb = 20
    disk_type    = "pd-balanced"
    image_type   = "COS_CONTAINERD"

    service_account = google_service_account.gke_nodes.email
    oauth_scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
    ]

    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    tags = ["sem-gke-node"]
  }
}

# Workload Identity bindings for Kubernetes ServiceAccounts in namespace "sem"
locals {
  workload_identity_bindings = {
    "sem-api"             = google_service_account.api.name
    "sem-otel-collector"  = google_service_account.otel_collector.name
    "sem-grafana"         = google_service_account.grafana.name
    "sem-test-controller" = google_service_account.test_controller.name
  }
}

resource "google_service_account_iam_member" "workload_identity" {
  for_each = local.workload_identity_bindings

  service_account_id = each.value
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[sem/${each.key}]"

  depends_on = [google_container_cluster.primary]
}

# Global static IP and managed SSL certificate for GKE Gateway
resource "google_compute_global_address" "gateway_ip" {
  name       = "sem-gateway-ip"
  depends_on = [google_project_service.apis]
}

resource "google_compute_managed_ssl_certificate" "gateway_cert" {
  name = "sem-gateway-cert"

  managed {
    domains = [
      var.domain,
      "app.${var.domain}",
      "monitoring.${var.domain}",
      "tests.${var.domain}",
    ]
  }

  depends_on = [google_project_service.apis]
}
