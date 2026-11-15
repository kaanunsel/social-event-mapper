resource "google_artifact_registry_repository" "images" {
  location      = var.region
  repository_id = "sem-images"
  description   = "Docker images for Social Event Mapper"
  format        = "DOCKER"

  cleanup_policies {
    id     = "keep-recent"
    action = "KEEP"
    most_recent_versions {
      keep_count = 10
    }
  }

  depends_on = [google_project_service.apis]
}
