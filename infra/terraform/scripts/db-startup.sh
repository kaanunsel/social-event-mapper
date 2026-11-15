#!/usr/bin/env bash
set -euo pipefail

export DEBIAN_FRONTEND=noninteractive

# Install PostgreSQL, PostGIS, and Google Cloud Ops Agent
apt-get update -y
apt-get install -y postgresql postgis curl gnupg lsb-release

# Detect installed PostgreSQL major version
PG_VERSION=$(ls /etc/postgresql | sort -V | tail -n 1)
PG_CONF_DIR="/etc/postgresql/$${PG_VERSION}/main"

# Configure PostgreSQL to listen on private VPC interface
sed -i "s/^#*listen_addresses.*/listen_addresses = '*'/" "$${PG_CONF_DIR}/postgresql.conf"

# Allow connections from VPC primary and GKE pod subnets
if ! grep -q "10.10.0.0/20" "$${PG_CONF_DIR}/pg_hba.conf"; then
  cat <<'EOF' >> "$${PG_CONF_DIR}/pg_hba.conf"
host    all             all             10.10.0.0/20            scram-sha-256
host    all             all             10.20.0.0/16            scram-sha-256
EOF
fi

systemctl enable postgresql
systemctl restart postgresql

# Fetch DB password from Secret Manager and configure database
DB_PASSWORD=$(gcloud secrets versions access latest --secret="${db_password_secret_id}" --project="${project_id}")

su - postgres -c "psql -v ON_ERROR_STOP=1" <<EOSQL
ALTER USER postgres WITH PASSWORD '$${DB_PASSWORD}';
EOSQL

if ! su - postgres -c "psql -tAc \"SELECT 1 FROM pg_database WHERE datname='sem'\"" | grep -q 1; then
  su - postgres -c "createdb sem"
fi

su - postgres -c "psql -d sem -v ON_ERROR_STOP=1" <<'EOSQL'
CREATE EXTENSION IF NOT EXISTS postgis;
EOSQL

# Install and configure Google Cloud Ops Agent if not already installed
if ! dpkg -s google-cloud-ops-agent >/dev/null 2>&1; then
  curl -sSO https://dl.google.com/cloudagents/add-google-cloud-ops-agent-repo.sh
  bash add-google-cloud-ops-agent-repo.sh --also-install
  rm -f add-google-cloud-ops-agent-repo.sh
fi

cat <<EOF > /etc/google-cloud-ops-agent/config.yaml
logging:
  receivers:
    postgresql_log:
      type: postgresql
      include_paths:
        - /var/log/postgresql/postgresql-*.log
  service:
    pipelines:
      postgresql:
        receivers: [postgresql_log]
metrics:
  receivers:
    postgresql:
      type: postgresql
      endpoint: 127.0.0.1:5432
      username: postgres
      password: "$${DB_PASSWORD}"
      databases:
        - sem
  service:
    pipelines:
      postgresql:
        receivers: [postgresql]
EOF

systemctl restart google-cloud-ops-agent
