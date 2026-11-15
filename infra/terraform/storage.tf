# Public application media bucket (avatars, event images)
resource "google_storage_bucket" "media" {
  name                        = "sem-media-${var.project_id}"
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "inherited"

  cors {
    origin = [
      "https://${var.domain}",
      "https://www.${var.domain}",
      "https://app.${var.domain}",
      "http://localhost:5173",
      "http://127.0.0.1:5173",
    ]
    method          = ["GET", "HEAD", "PUT", "OPTIONS"]
    response_header = ["Content-Type", "Cache-Control", "ETag"]
    max_age_seconds = 3600
  }

  depends_on = [google_project_service.apis]
}

resource "google_storage_bucket_iam_member" "media_public_read" {
  bucket = google_storage_bucket.media.name
  role   = "roles/storage.objectViewer"
  member = "allUsers"
}

resource "google_storage_bucket_iam_member" "media_api_admin" {
  bucket = google_storage_bucket.media.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.api.email}"
}

# Private bucket for load test run state, manifests, and CSV results
resource "google_storage_bucket" "test_results" {
  name                        = "sem-test-results-${var.project_id}"
  location                    = var.region
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      age = 90
    }
    action {
      type = "Delete"
    }
  }

  depends_on = [google_project_service.apis]
}

resource "google_storage_bucket_iam_member" "test_results_controller" {
  bucket = google_storage_bucket.test_results.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.test_controller.email}"
}

resource "google_storage_bucket_iam_member" "test_results_runner" {
  bucket = google_storage_bucket.test_results.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.locust_runner.email}"
}
