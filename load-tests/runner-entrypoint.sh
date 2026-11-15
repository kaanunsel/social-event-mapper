#!/usr/bin/env bash
set -euo pipefail

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

WORKDIR="/opt/sem-loadtest"
mkdir -p "${WORKDIR}"
cd "${WORKDIR}"

export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y python3-pip python3-venv curl

python3 -m venv "${WORKDIR}/venv"
"${WORKDIR}/venv/bin/pip" install --no-cache-dir locust==2.32.2 google-cloud-storage==2.19.0

gcloud storage cp "${LOCUSTFILE_GCS}" "${WORKDIR}/locustfile.py"

python3 - <<PYEOF
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

set +e
"${WORKDIR}/venv/bin/locust" \
  -f "${WORKDIR}/locustfile.py" \
  --headless \
  --host="${TARGET_HOST}" \
  -u "${USERS}" \
  -r "${SPAWN_RATE}" \
  -t "${DURATION}" \
  --csv="${WORKDIR}/locust" \
  --logfile="${WORKDIR}/runner.log" \
  --stop-timeout=15 &
LOCUST_PID=$!

# Poll for STOPPING desired_state from controller
while kill -0 "${LOCUST_PID}" 2>/dev/null; do
  DESIRED=$(python3 - <<PYEOF
import json
from google.cloud import storage
client = storage.Client()
blob = client.bucket("${RESULTS_BUCKET}").blob("runs/${RUN_ID}/manifest.json")
print(json.loads(blob.download_as_text()).get("desired_state", "RUNNING"))
PYEOF
)
  if [ "${DESIRED}" = "STOPPING" ]; then
    kill -TERM "${LOCUST_PID}" || true
    break
  fi
  sleep 5
done

wait "${LOCUST_PID}"
EXIT_CODE=$?
set -e

for f in locust_stats.csv locust_stats_history.csv locust_failures.csv runner.log; do
  if [ -f "${WORKDIR}/${f}" ]; then
    gcloud storage cp "${WORKDIR}/${f}" "gs://${RESULTS_BUCKET}/runs/${RUN_ID}/${f}"
  fi
done

python3 - <<PYEOF
import json, datetime
from google.cloud import storage

client = storage.Client()
bucket = client.bucket("${RESULTS_BUCKET}")
blob = bucket.blob("runs/${RUN_ID}/manifest.json")
data = json.loads(blob.download_as_text())
data["status"] = "SUCCEEDED" if ${EXIT_CODE} == 0 else "FAILED"
data["exit_code"] = ${EXIT_CODE}
data["finished_at"] = datetime.datetime.utcnow().isoformat() + "Z"
blob.upload_from_string(json.dumps(data, indent=2), content_type="application/json")
PYEOF
