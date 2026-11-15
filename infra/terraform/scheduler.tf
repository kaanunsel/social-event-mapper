resource "google_secret_manager_secret_iam_member" "functions_read_db_password" {
  secret_id = google_secret_manager_secret.db_password.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.functions.email}"
}

resource "google_secret_manager_secret_iam_member" "functions_read_jwt_secret" {
  secret_id = google_secret_manager_secret.jwt_secret.secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.functions.email}"
}

resource "google_cloud_run_v2_service" "worker" {
  name     = "sem-worker"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    service_account = google_service_account.functions.email

    scaling {
      min_instance_count = 0
      max_instance_count = 2
    }

    vpc_access {
      network_interfaces {
        network    = google_compute_network.vpc.id
        subnetwork = google_compute_subnetwork.main.id
      }
      egress = "PRIVATE_RANGES_ONLY"
    }

    containers {
      image   = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.images.repository_id}/api:latest"
      command = ["/app/worker"]

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
      }

      env {
        name  = "APP_ENV"
        value = "local"
      }
      env {
        name  = "DB_HOST"
        value = google_compute_address.db_internal_ip.address
      }
      env {
        name  = "DB_PORT"
        value = "5432"
      }
      env {
        name  = "DB_NAME"
        value = "sem"
      }
      env {
        name  = "DB_USER"
        value = "postgres"
      }
      env {
        name  = "STORAGE_PROVIDER"
        value = "gcs"
      }
      env {
        name  = "GCS_BUCKET"
        value = google_storage_bucket.media.name
      }
      env {
        name  = "GCS_SERVICE_ACCOUNT_EMAIL"
        value = google_service_account.api.email
      }
      env {
        name  = "SPACES_CDN_BASE_URL"
        value = "https://storage.googleapis.com/${google_storage_bucket.media.name}"
      }
      env {
        name  = "MAIL_PROVIDER"
        value = "mock"
      }
      env {
        name  = "PUSH_PROVIDER"
        value = "mock"
      }
      env {
        name = "DB_PASSWORD"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.db_password.secret_id
            version = "latest"
          }
        }
      }
      env {
        name = "JWT_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.jwt_secret.secret_id
            version = "latest"
          }
        }
      }
    }
  }

  depends_on = [
    google_project_service.apis,
    google_secret_manager_secret_iam_member.functions_read_db_password,
    google_secret_manager_secret_iam_member.functions_read_jwt_secret,
  ]
}

resource "google_cloud_run_v2_service_iam_member" "scheduler_invoke_worker" {
  location = var.region
  name     = google_cloud_run_v2_service.worker.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.scheduler.email}"
}

resource "google_cloud_scheduler_job" "lifecycle" {
  name             = "sem-event-lifecycle"
  region           = var.scheduler_region
  description      = "Transition event statuses every minute"
  schedule         = "* * * * *"
  time_zone        = "UTC"
  attempt_deadline = "60s"

  retry_config {
    retry_count = 1
  }

  http_target {
    http_method = "POST"
    uri         = "${google_cloud_run_v2_service.worker.uri}/lifecycle"

    oidc_token {
      service_account_email = google_service_account.scheduler.email
      audience              = google_cloud_run_v2_service.worker.uri
    }
  }

  depends_on = [google_cloud_run_v2_service_iam_member.scheduler_invoke_worker]
}

resource "google_cloud_scheduler_job" "cleanup" {
  name             = "sem-notification-cleanup"
  region           = var.scheduler_region
  description      = "Delete expired notifications daily"
  schedule         = "0 3 * * *"
  time_zone        = "UTC"
  attempt_deadline = "120s"

  retry_config {
    retry_count = 2
  }

  http_target {
    http_method = "POST"
    uri         = "${google_cloud_run_v2_service.worker.uri}/cleanup"

    oidc_token {
      service_account_email = google_service_account.scheduler.email
      audience              = google_cloud_run_v2_service.worker.uri
    }
  }

  depends_on = [google_cloud_run_v2_service_iam_member.scheduler_invoke_worker]
}
