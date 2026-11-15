# GCP Operations Guide

## Pausing and Resuming Before Demo (Zero Data Loss)

PostGIS data lives on the `sem-db` persistent disk and Grafana dashboards live on `sem-grafana-pvc`. Pausing stops compute billing for the DB VM and GKE worker nodes without deleting any data.

### Pause all compute
```bash
./scripts/pause-gcp.sh
```
This script:
1. Pauses Cloud Scheduler jobs (`sem-event-lifecycle`, `sem-notification-cleanup`).
2. Scales all GKE deployments in namespace `sem` to `0`.
3. Resizes `sem-pool` to `0` nodes.
4. Stops the `sem-db` Compute Engine VM.

### Resume before demo
```bash
./scripts/resume-gcp.sh
```
This script:
1. Starts `sem-db` and waits for it to boot.
2. Resizes `sem-pool` back to `2` nodes.
3. Scales GKE deployments back to `1` replica and waits for rollouts.
4. Resumes Cloud Scheduler jobs.

---

## Release & Rolling Deploy via GitHub Actions

Pushing to `main` does **not** deploy to GCP. Deployments are triggered only when you create a tag (`v*`) or publish a GitHub Release (`.github/workflows/deploy-release.yml`).

### How to deploy a new release
```bash
git tag v1.0.1
git push origin v1.0.1
```
(Or create a Release in GitHub UI with tag `v1.0.1`.)

The workflow:
1. Authenticates to GCP via Workload Identity Federation (`sem-github-deployer`).
2. Builds and pushes `api:<tag>`, `web:<tag>`, and `test-control:<tag>` to Artifact Registry.
3. Runs the `sem-db-migrate` Kubernetes Job first. **If migrations fail, the deployment aborts immediately and no pods are updated.**
4. Performs a rolling update on `sem-api`, `sem-web`, and `sem-test-controller`, and updates the `sem-worker` Cloud Run service.

---

## Accessing Secrets

Secrets are stored in GCP Secret Manager (`project-72f15835-9e20-4691-877`):
```bash
gcloud secrets versions access latest --secret=sem-db-password --project=project-72f15835-9e20-4691-877
gcloud secrets versions access latest --secret=sem-jwt-secret --project=project-72f15835-9e20-4691-877
gcloud secrets versions access latest --secret=sem-controller-auth --project=project-72f15835-9e20-4691-877
gcloud secrets versions access latest --secret=sem-google-maps-web-api-key --project=project-72f15835-9e20-4691-877
```
