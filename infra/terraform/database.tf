resource "google_compute_address" "db_internal_ip" {
  name         = "sem-db-internal-ip"
  subnetwork   = google_compute_subnetwork.main.id
  address_type = "INTERNAL"
  address      = "10.10.0.10"
  region       = var.region
}

resource "google_compute_firewall" "allow_db_ingress" {
  name    = "sem-allow-db-ingress"
  network = google_compute_network.vpc.id

  allow {
    protocol = "tcp"
    ports    = ["5432"]
  }

  source_ranges = [
    "10.10.0.0/20",
    "10.20.0.0/16",
  ]
  target_tags = ["sem-db"]
}

resource "google_compute_instance" "db" {
  name         = "sem-db"
  machine_type = "e2-medium"
  zone         = var.zone
  tags         = ["iap-ssh", "sem-db"]

  boot_disk {
    initialize_params {
      image = "debian-cloud/debian-12"
      size  = 15
      type  = "pd-balanced"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.main.id
    network_ip = google_compute_address.db_internal_ip.address
  }

  service_account {
    email  = google_service_account.db_vm.email
    scopes = ["cloud-platform"]
  }

  metadata_startup_script = templatefile("${path.module}/scripts/db-startup.sh", {
    project_id             = var.project_id
    db_password_secret_id  = google_secret_manager_secret.db_password.secret_id
  })

  allow_stopping_for_update = true

  depends_on = [
    google_compute_router_nat.nat,
    google_secret_manager_secret_version.db_password,
    google_secret_manager_secret_iam_member.db_vm_read_password,
  ]
}
