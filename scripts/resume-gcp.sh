#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="project-72f15835-9e20-4691-877"
ZONE="europe-north2-a"
SCHEDULER_REGION="europe-west1"
CLUSTER="sem-gke"
NODE_POOL="sem-pool"
DB_VM="sem-db"

echo "==> Starting PostGIS VM ${DB_VM}..."
gcloud compute instances start "${DB_VM}" --zone="${ZONE}" --project="${PROJECT_ID}" --quiet

echo "==> Resizing GKE node pool ${NODE_POOL} to 2 nodes..."
gcloud container clusters resize "${CLUSTER}" --node-pool="${NODE_POOL}" --num-nodes=2 --zone="${ZONE}" --project="${PROJECT_ID}" --quiet

echo "==> Connecting to GKE cluster ${CLUSTER}..."
gcloud container clusters get-credentials "${CLUSTER}" --zone="${ZONE}" --project="${PROJECT_ID}" --quiet

echo "==> Scaling GKE deployments back to 1 replica..."
kubectl -n sem scale deployment sem-api sem-web sem-grafana sem-otel-collector sem-test-controller --replicas=1

echo "==> Waiting for rollouts..."
kubectl -n sem rollout status deployment/sem-api --timeout=300s
kubectl -n sem rollout status deployment/sem-web --timeout=300s
kubectl -n sem rollout status deployment/sem-grafana --timeout=300s
kubectl -n sem rollout status deployment/sem-otel-collector --timeout=300s

echo "==> Resuming Cloud Scheduler jobs..."
gcloud scheduler jobs resume sem-event-lifecycle --location="${SCHEDULER_REGION}" --project="${PROJECT_ID}" --quiet || true
gcloud scheduler jobs resume sem-notification-cleanup --location="${SCHEDULER_REGION}" --project="${PROJECT_ID}" --quiet || true

echo "==> System resumed! Current pods:"
kubectl -n sem get pods
