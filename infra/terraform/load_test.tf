resource "random_password" "controller_password" {
  length  = 24
  special = false
}

resource "google_secret_manager_secret" "controller_auth" {
  secret_id = "sem-controller-auth"

  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "controller_auth" {
  secret      = google_secret_manager_secret.controller_auth.id
  secret_data = random_password.controller_password.result
}

resource "google_secret_manager_secret_iam_member" "controller_read_auth" {
  secret_id = google_secret_manager_secret.controller_auth.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.test_controller.email}"
}

resource "google_compute_instance_template" "locust_runner" {
  name_prefix  = "sem-locust-runner-"
  machine_type = "e2-medium"
  region       = var.region
  tags         = ["iap-ssh", "sem-locust-runner"]

  disk {
    source_image = "debian-cloud/debian-12"
    auto_delete  = true
    boot         = true
    disk_size_gb = 10
    disk_type    = "pd-balanced"
  }

  network_interface {
    subnetwork = google_compute_subnetwork.main.id
    access_config {
      # Ephemeral external IP so Locust targets the public HTTPS load balancer
    }
  }

  service_account {
    email  = google_service_account.locust_runner.email
    scopes = ["cloud-platform"]
  }

  metadata = {
    startup-script-url = "gs://${google_storage_bucket.test_results.name}/scripts/runner-entrypoint.sh"
  }

  lifecycle {
    create_before_destroy = true
  }

  depends_on = [google_project_service.apis]
}
