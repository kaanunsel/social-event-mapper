# Local Stack and Configuration

This document covers the local Docker Compose stack, API documentation serving, and the runtime configuration contract. Remote deployment is not defined in this repository yet.

## Compose file

| File | Purpose |
|------|---------|
| [`deploy/docker-compose.local.yml`](../deploy/docker-compose.local.yml) | `build:` from `../backend` and `../frontend`. Postgres is published to **127.0.0.1:5433** on the host (→ container `5432`) for psql and GUI clients. |

Run from the **repository root** so Compose's default project directory is `deploy/` and `deploy/.env` is loaded:

```bash
docker compose -f deploy/docker-compose.local.yml up --build
```

### Postgres host port

PostGIS is reachable on **127.0.0.1:5433** on your machine (host port only; the container still uses `5432` internally). Connect with user `postgres`, database `sem`, and `DB_PASSWORD` from `deploy/.env`:

```bash
psql -h 127.0.0.1 -p 5433 -U postgres -d sem
```

If port `5433` is taken, create an uncommitted `deploy/docker-compose.override.yml` with a different host port:

```yaml
services:
  postgres:
    ports:
      - "127.0.0.1:55432:5432"
```

## Nginx

[`nginx/nginx.local.conf`](../nginx/nginx.local.conf) is used by the local compose file: HTTP only (port 80), routing `/api/`, `/api/docs/`, and `/`.

## API documentation

This section covers **OpenAPI 3.x YAML** specs and the **Swagger UI** used to browse them interactively. This keeps the Go API binary free of documentation routes: **nginx** serves the documentation files in the **local Docker Compose** stack.

### How it works

1. **Local Compose** bind-mounts **only** [`docs/openapi/`](../docs/openapi/) and [`docs/swagger-ui/`](../docs/swagger-ui/) into the nginx container (read-only). Other repo folders under `docs/` (for example `db/`, `design/`) are **not** mounted, so they cannot be served or enumerated by nginx.
2. **Nginx** exposes:
   - **`/api/docs/`** — Swagger UI (HTML + vendored Swagger UI JS/CSS under [`docs/swagger-ui/`](../docs/swagger-ui/)).
   - **`/api/docs/openapi/`** — Raw OpenAPI files (e.g. [`docs/openapi/auth.yaml`](../docs/openapi/auth.yaml)).
3. **Backend traffic** is unchanged: requests under **`/api/`** that are *not* matched by the docs locations still proxy to the Go service.

Swagger UI assets are **vendored** in git (see [`docs/swagger-ui/vendor/README.md`](../docs/swagger-ui/vendor/README.md)) so the docs work **without internet access** (no CDN).

### URLs

| Stack | Swagger UI | Example spec URL |
|-------|------------|------------------|
| **Local** (`docker-compose.local`) | `http://localhost/api/docs/` | `http://localhost/api/docs/openapi/auth.yaml` |

`/api/docs` without a trailing slash redirects to `/api/docs/`.

### Multiple OpenAPI specs

Swagger UI auto-discovers every `.yaml` or `.yml` file under [`docs/openapi/`](../docs/openapi/) from the nginx-served directory listing.

To add another API:

1. Add `docs/openapi/<your-spec>.yaml` or `docs/openapi/<your-spec>.yml`.
2. Reload `/api/docs/`; the new spec will appear automatically in the dropdown.
3. Open the UI with `?spec=<name>` to select that spec by default, or use the built-in spec picker.

OpenAPI `servers` in each YAML should match how clients call the API (this repo uses `url: /api` for same-origin requests from the browser).

## Environment variables (app contract)

[`deploy/.env.example`](../deploy/.env.example) lists the Compose variables. Non-secret values such as Postgres user/database live directly in the compose files, and backend runtime defaults live in `backend/config/application.<APP_ENV>.yaml`.

These names still matter at runtime:

| Variable | Used by |
|----------|---------|
| `DB_PASSWORD` | Postgres + backend |
| `JWT_SECRET` | Backend |
| `NEW_RELIC_LICENSE_KEY` | Backend OpenTelemetry export to New Relic OTLP |
| `RESEND_CLIENT_API_KEY` | Backend transactional email delivery |
| `SPACES_ACCESS_KEY` | Backend direct-upload signing + Spaces admin script |
| `SPACES_SECRET_KEY` | Backend direct-upload signing + Spaces admin script |
| `SPACES_ENDPOINT` | Backend direct-upload signing + Spaces admin script |
| `SPACES_BUCKET` | Backend direct-upload signing + Spaces admin script |
| `SPACES_CDN_BASE_URL` | Backend public image URLs returned to clients |
| `SPACES_S3_REGION` | Backend Spaces SDK configuration + Spaces admin script |
| `FIREBASE_SERVICE_ACCOUNT_JSON_BASE64` | Backend FCM push delivery when `PUSH_PROVIDER=firebase` |



## Spaces bucket setup

The backend now issues signed direct-upload URLs for profile avatars and event images. Each upload instruction includes `x-amz-acl: public-read`, so uploaded objects become readable from the origin and CDN without requiring signed read URLs.

If your Spaces key is a limited-access key, that is expected. In that case:

- Keep using the backend-generated upload headers exactly as returned.
- The `x-amz-acl: public-read` upload header remains the primary mechanism that makes uploaded images readable from the CDN.
- Configure CORS manually in the DigitalOcean panel for browser-based uploads.

Recommended CORS configuration:

- Origins: `http://localhost:5173`, `http://127.0.0.1:5173`, `https://socialeventmapper.com`, `https://www.socialeventmapper.com`
- Methods: `GET`, `HEAD`, `PUT`
- Allowed headers: `Content-Type`, `Cache-Control`, `x-amz-acl`
- Exposed headers: `ETag`

For the end-to-end client flow, examples, and troubleshooting notes, see [docs/backend/features/image-upload.md](backend/business-flows/image-upload.md).
