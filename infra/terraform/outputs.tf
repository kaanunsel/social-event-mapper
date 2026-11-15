output "vpc_id" {
  value = google_compute_network.vpc.id
}

output "vpc_name" {
  value = google_compute_network.vpc.name
}

output "subnet_id" {
  value = google_compute_subnetwork.main.id
}

output "subnet_name" {
  value = google_compute_subnetwork.main.name
}

output "service_accounts" {
  value = {
    gke_nodes       = google_service_account.gke_nodes.email
    api             = google_service_account.api.email
    db_vm           = google_service_account.db_vm.email
    functions       = google_service_account.functions.email
    scheduler       = google_service_account.scheduler.email
    otel_collector  = google_service_account.otel_collector.email
    grafana         = google_service_account.grafana.email
    test_controller = google_service_account.test_controller.email
    locust_runner   = google_service_account.locust_runner.email
  }
}

output "artifact_registry_repo" {
  value = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.images.repository_id}"
}

output "media_bucket" {
  value = google_storage_bucket.media.name
}

output "test_results_bucket" {
  value = google_storage_bucket.test_results.name
}

output "db_private_ip" {
  value = google_compute_address.db_internal_ip.address
}

output "gke_cluster_name" {
  value = google_container_cluster.primary.name
}

output "gateway_ip" {
  value = google_compute_global_address.gateway_ip.address
}

output "gateway_ssl_cert_name" {
  value = google_compute_managed_ssl_certificate.gateway_cert.name
}

output "locust_instance_template" {
  value = google_compute_instance_template.locust_runner.self_link
}
