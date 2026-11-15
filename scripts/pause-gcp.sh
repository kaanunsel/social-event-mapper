#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="project-72f15835-9e20-4691-877"
ZONE="europe-north2-a"
SCHEDULER_REGION="europe-west1"
CLUSTER="sem-gke"
NODE_POOL="sem-pool"
DB_VM="sem-db"

echo "==> Connecting to GKE cluster ${CLUSTER}..."
gcloud container clusters get-credentials "${CLUSTER}" --zone="${ZONE}" --project="${PROJECT_ID}" --quiet

echo "==> Pausing Cloud Scheduler jobs..."
gcloud scheduler jobs pause sem-event-lifecycle --location="${SCHEDULER_REGION}" --project="${PROJECT_ID}" --quiet || true
gcloud scheduler jobs pause sem-notification-cleanup --location="${SCHEDULER_REGION}" --project="${PROJECT_ID}" --quiet || true

echo "==> Scaling GKE deployments to 0 (PVCs and DB data remain intact)..."
kubectl -n sem scale deployment sem-api sem-web sem-grafana sem-otel-collector sem-test-controller --replicas=0 || true

echo "==> Deleting GKE Gateway sem-gateway to stop Global Load Balancer hourly charges (static IP & SSL cert preserved)..."
kubectl -n sem delete gateway sem-gateway --ignore-not-found --wait=true

echo "==> Disabling autoscaling and resizing GKE node pool ${NODE_POOL} to 0 nodes..."
gcloud container clusters update "${CLUSTER}" --no-enable-autoscaling --node-pool="${NODE_POOL}" --zone="${ZONE}" --project="${PROJECT_ID}" --quiet
gcloud container clusters resize "${CLUSTER}" --node-pool="${NODE_POOL}" --num-nodes=0 --zone="${ZONE}" --project="${PROJECT_ID}" --quiet

echo "==> Stopping PostGIS VM ${DB_VM} (boot/data disk preserved)..."
gcloud compute instances stop "${DB_VM}" --zone="${ZONE}" --project="${PROJECT_ID}" --quiet

echo "==> All compute workloads and Load Balancer paused safely. Persistent disks (DB + Grafana PVC), static IP, SSL cert, and GCS buckets are preserved."
