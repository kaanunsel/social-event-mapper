#!/usr/bin/env bash
set -euo pipefail

WORKDIR="/opt/sem-loadtest"
mkdir -p "${WORKDIR}"
cd "${WORKDIR}"

exec > >(tee -a "${WORKDIR}/runner.log") 2>&1

META_URL="http://metadata.google.internal/computeMetadata/v1/instance/attributes"
meta() {
  curl -sf -H "Metadata-Flavor: Google" "${META_URL}/$1"
}

RUN_ID=$(meta run-id)
TARGET_HOST=$(meta target-host)
USERS=$(meta users)
SPAWN_RATE=$(meta spawn-rate)
DURATION=$(meta duration)
RESULTS_BUCKET=$(meta results-bucket)
LOCUSTFILE_GCS=$(meta locustfile-gcs)

FINALIZED=0
STOPPED_BY_USER=0
EXIT_CODE=1

upload_artifacts() {
  for f in locust_stats.csv locust_stats_history.csv locust_failures.csv runner.log; do
    if [ -f "${WORKDIR}/${f}" ]; then
      gcloud storage cp "${WORKDIR}/${f}" "gs://${RESULTS_BUCKET}/runs/${RUN_ID}/${f}" || true
    fi
  done
}

update_manifest() {
  local status="$1"
  local exit_code="$2"
  local py_bin="${WORKDIR}/venv/bin/python3"
  if [ ! -x "${py_bin}" ]; then
    return 0
  fi
  "${py_bin}" - <<PYEOF || true
import json, datetime
from google.cloud import storage

client = storage.Client()
bucket = client.bucket("${RESULTS_BUCKET}")
blob = bucket.blob("runs/${RUN_ID}/manifest.json")
data = json.loads(blob.download_as_text())
data["status"] = "${status}"
data["exit_code"] = ${exit_code}
data["finished_at"] = datetime.datetime.utcnow().isoformat() + "Z"
blob.upload_from_string(json.dumps(data, indent=2), content_type="application/json")
PYEOF
}

on_exit() {
  local rc=$?
  if [ "${FINALIZED}" -eq 0 ]; then
    upload_artifacts
    if [ "${STOPPED_BY_USER}" -eq 1 ]; then
      update_manifest "STOPPED" "${rc}"
    else
      update_manifest "FAILED" "${rc}"
    fi
  fi
}
trap on_exit EXIT

echo "[$(date -u +%FT%TZ)] Starting load test run=${RUN_ID} target=${TARGET_HOST} users=${USERS} rate=${SPAWN_RATE} duration=${DURATION}"

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y python3-pip python3-venv curl

python3 -m venv "${WORKDIR}/venv"
"${WORKDIR}/venv/bin/pip" install --no-cache-dir locust==2.32.2 google-cloud-storage==2.19.0

gcloud storage cp "${LOCUSTFILE_GCS}" "${WORKDIR}/locustfile.py"

"${WORKDIR}/venv/bin/python3" - <<PYEOF
import json, datetime
from google.cloud import storage

client = storage.Client()
bucket = client.bucket("${RESULTS_BUCKET}")
blob = bucket.blob("runs/${RUN_ID}/manifest.json")
data = json.loads(blob.download_as_text())
data["status"] = "RUNNING"
data["started_at"] = datetime.datetime.utcnow().isoformat() + "Z"
blob.upload_from_string(json.dumps(data, indent=2), content_type="application/json")
PYEOF

echo "[$(date -u +%FT%TZ)] Launching Locust headless..."

set +e
"${WORKDIR}/venv/bin/locust" \
  -f "${WORKDIR}/locustfile.py" \
  --headless \
  --host="${TARGET_HOST}" \
  -u "${USERS}" \
  -r "${SPAWN_RATE}" \
  -t "${DURATION}" \
  --csv="${WORKDIR}/locust" \
  --stop-timeout=15 &
LOCUST_PID=$!

# Poll for STOPPING desired_state from controller
while kill -0 "${LOCUST_PID}" 2>/dev/null; do
  DESIRED=$("${WORKDIR}/venv/bin/python3" - <<PYEOF 2>/dev/null || echo "RUNNING"
import json
from google.cloud import storage
client = storage.Client()
blob = client.bucket("${RESULTS_BUCKET}").blob("runs/${RUN_ID}/manifest.json")
print(json.loads(blob.download_as_text()).get("desired_state", "RUNNING"))
PYEOF
)
  if [ "${DESIRED}" = "STOPPING" ]; then
    echo "[$(date -u +%FT%TZ)] Stop requested by controller, sending SIGTERM to Locust..."
    STOPPED_BY_USER=1
    kill -TERM "${LOCUST_PID}" || true
    break
  fi
  sleep 5
done

wait "${LOCUST_PID}"
EXIT_CODE=$?
set -e

echo "[$(date -u +%FT%TZ)] Locust exited with code ${EXIT_CODE} (stopped_by_user=${STOPPED_BY_USER})"

upload_artifacts

if [ "${STOPPED_BY_USER}" -eq 1 ]; then
  update_manifest "STOPPED" "${EXIT_CODE}"
elif [ "${EXIT_CODE}" -eq 0 ]; then
  update_manifest "SUCCEEDED" "${EXIT_CODE}"
else
  update_manifest "FAILED" "${EXIT_CODE}"
fi
FINALIZED=1
