# Social Event Mapper

Social Event Mapper is a platform for discovering, creating, joining, moderating, and checking in to social events on a map.

The project ships three surfaces backed by one API:

- **Web app:** event discovery, event creation/management, profile flows, and admin backoffice.
- **Mobile app:** map-first discovery, event participation, invitations, tickets, QR check-in, and profile flows.
- **Backend API:** auth, event lifecycle, participation, private invitations, protected join requests, QR tickets, notifications, reports, badges, and admin moderation.

## Live Links

| Item | Link |
|---|---|
| Live web app | [https://socialeventmapper.com/](https://socialeventmapper.com/) |
| Live Swagger / OpenAPI docs | [https://socialeventmapper.com/api/docs/](https://socialeventmapper.com/api/docs/) |

## Tech Stack

| Layer | Technology |
|---|---|
| Backend | Go 1.26, Fiber, PostgreSQL 16 + PostGIS, golang-migrate, OpenAPI 3.1 |
| Web | React 19, TypeScript, Vite, React Router, Google Maps |
| Mobile | React Native, Expo SDK 55, Expo Router, React Native Maps |
| Runtime | Docker Compose, NGINX reverse proxy, Swagger UI |
| CI/CD | GitHub Actions, release APK workflow |

## Repository Layout

| Path | Role |
|---|---|
| [`backend/`](backend/) | Go API, domain/application layers, adapters, migrations, backend tests |
| [`frontend/`](frontend/) | React web app and admin backoffice |
| [`mobile/`](mobile/) | Expo React Native app |
| [`deploy/`](deploy/) | Local Docker Compose files and environment template |
| [`nginx/`](nginx/) | Local and deployed NGINX reverse proxy configs |
| [`docs/openapi/`](docs/openapi/) | OpenAPI specs served by Swagger UI |
| [`docs/backend/`](docs/backend/) | Backend architecture and business-flow documentation |
| [`docs/db/`](docs/db/) | Database schema and persistence documentation |

## Quick Start: Local Docker

Use this path if you want to run the whole web stack locally. It builds the backend and frontend from source, starts PostgreSQL/PostGIS, and serves everything through local NGINX.

### Prerequisites

- Docker Desktop or Docker Engine with Compose
- Git
- A Google Maps Web API key if you want the web map to render normally

### 1. Create the local environment file

```bash
cp deploy/.env.example deploy/.env
```

For the minimum local boot, fill:

```dotenv
DB_PASSWORD=<local-postgres-password>
JWT_SECRET=<local-jwt-secret>
VITE_GOOGLE_MAPS_WEB_API_KEY=<google-maps-web-key>
```

Optional feature credentials:

- `RESEND_CLIENT_API_KEY`: only needed if email delivery is changed from mock mode to Resend.
- `SPACES_*`: needed for real direct image uploads.
- `FIREBASE_SERVICE_ACCOUNT_JSON_BASE64` and `NEW_RELIC_LICENSE_KEY`: not needed for local compose; local push notifications use the mock provider.

Do not commit `deploy/.env`.

### 2. Start the local stack

```bash
docker compose -f deploy/docker-compose.local.yml up --build
```

Open:

- Web app: [http://localhost](http://localhost)
- Backend health: [http://localhost/api/health](http://localhost/api/health)
- Swagger UI: [http://localhost/api/docs/](http://localhost/api/docs/)

Useful local details:

- The API base URL is `http://localhost/api`.
- PostgreSQL is exposed to the host at `127.0.0.1:5433`.
- Database name is `sem`, user is `postgres`, password is `DB_PASSWORD`.
- Local backend config uses mock email and mock push providers.
- Migrations seed the canonical event categories and badges. A fresh database does not need a separate category seed script.

Stop the stack with:

```bash
docker compose -f deploy/docker-compose.local.yml down
```

Remove local database state with:

```bash
docker compose -f deploy/docker-compose.local.yml down -v
```

## Local Accounts

No default credentials are stored in this repository. For a fresh local database, create users through the registration flow. To test admin-only screens locally, create a user first, then promote it in the local database:

```bash
docker compose -f deploy/docker-compose.local.yml exec postgres \
  psql -U postgres -d sem \
  -c "UPDATE app_user SET role = 'ADMIN' WHERE email = '<admin-email@example.com>';"
```

Then log in again so the access token includes the updated `ADMIN` role.

## Mobile App Against Local Docker

The mobile app is developed from the `mobile/` directory.

```bash
cd mobile
npm install
cp .env.example .env
npx expo start
```

API base URL behavior:

| Runtime | API base |
|---|---|
| iOS Simulator | `http://localhost/api` |
| Android Emulator | `http://10.0.2.2/api` |
| Physical phone with Expo Go | Set `EXPO_PUBLIC_API_BASE_URL=http://<your-computer-LAN-IP>/api` in `mobile/.env` |

For Android map rendering, set:

```dotenv
EXPO_PUBLIC_GOOGLE_MAPS_ANDROID_API_KEY=<google-maps-android-key>
```

Restart Metro after editing `mobile/.env`.

## Web App Outside Docker

The recommended local path is Docker Compose. If you need to run only the web app during development:

```bash
cd frontend
npm install
cp .env.example .env.local
npm run dev
```

Set `VITE_GOOGLE_MAPS_WEB_API_KEY` in `frontend/.env.local`. The dev server expects a reachable backend API; the easiest option is to keep the Docker backend/nginx stack running.

## Backend Outside Docker

The recommended local path is Docker Compose. If you need to run only the backend:

1. Start or provide a PostgreSQL/PostGIS database.
2. Copy `deploy/.env.example` to `.env` at the repository root and fill local values.
3. Run the server:

```bash
cd backend
go run ./cmd/server
```

By default, the backend listens on `http://localhost:8080`. For full-stack work, prefer NGINX through Docker Compose so the public local API path remains `http://localhost/api`.

## API Documentation

Swagger UI is served from the OpenAPI specs in [`docs/openapi/`](docs/openapi/).

- Local Swagger UI: [http://localhost/api/docs/](http://localhost/api/docs/)
- Live Swagger UI: [https://socialeventmapper.com/api/docs/](https://socialeventmapper.com/api/docs/)
- Raw specs in the deployed environment: `https://socialeventmapper.com/api/docs/openapi/<spec>.yaml`

The API includes domain specs for auth, events, tickets, profiles, notifications, badges, categories, favorite locations, and admin moderation.

## Running Checks

Backend:

```bash
cd backend
./shipcheck.sh
```

Set `SKIP_INTEGRATION=1` only when Docker is unavailable:

```bash
SKIP_INTEGRATION=1 ./shipcheck.sh
```

Frontend:

```bash
cd frontend
npm install
npm run build
npm test
```

Mobile:

```bash
cd mobile
npm install
npm test
```

Mobile E2E scenarios use Maestro:

```bash
cd mobile
npm run test:e2e
```

## Deployment Notes

The local stack is defined in [`deploy/docker-compose.local.yml`](deploy/docker-compose.local.yml). Remote deployment is being redesigned and is not yet defined in this repository. See [`docs/deploy.md`](docs/deploy.md) for local stack details.

## Releases and APKs

The Android release APK is built by [`mobile-apk.yml`](.github/workflows/mobile-apk.yml). Release runs attach the APK to the GitHub Release page.

For local APK build details, see [`mobile/README.md`](mobile/README.md).

## Further Reading

- [Deployment guide](docs/deploy.md)
- [Repository conventions](docs/conventions.md)
- [Backend architecture](docs/backend/architecture.md)
- [Backend business flows](docs/backend/business-flows/)
- [Database guide](docs/db/database.md)
- [OpenAPI specs](docs/openapi/)
