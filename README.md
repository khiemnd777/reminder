# Reminder Calendar Aggregator

Minimal calendar aggregation app with:
- static SPA frontend
- Go + Fiber backend
- SQLite persistence
- Google Calendar via OAuth2 + REST

The app is intentionally small:
- no Redis
- no queue
- no background sync
- no cron
- no multi-user auth model

## Project Layout

```text
backend/
  cmd/
  internal/
  pkg/
web/
Dockerfile
docker-compose.yml
```

## Environment Variables

The backend reads these variables:

| Variable | Required | Description |
| --- | --- | --- |
| `APP_ADDR` | no | Fiber listen address. Default: `:8080` |
| `DATABASE_PATH` | no | SQLite file path. Default: `./data/reminder.db` |
| `GOOGLE_CLIENT_ID` | yes for Google OAuth | Google OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | yes for Google OAuth | Google OAuth client secret |
| `GOOGLE_REDIRECT_URL` | yes for Google OAuth | Must match Google OAuth callback URL |

See [.env.example](/Users/khiemnguyen/Works/manle/reminder/.env.example) for the template.

## Run Locally

1. Copy the example env file and fill in real Google credentials.

```bash
cp .env.example .env
```

2. Start the app with Docker Compose.

```bash
docker compose up --build
```

3. Open [http://localhost:8080](http://localhost:8080).

SQLite data is persisted in the `./data` directory through a bind mount.

## Google OAuth Setup

Create an OAuth client in Google Cloud and configure:
- Authorized redirect URI: `http://localhost:8080/auth/google/callback`
- Calendar API access enabled

For local Docker usage, `GOOGLE_REDIRECT_URL` should usually remain:

```env
GOOGLE_REDIRECT_URL=http://localhost:8080/auth/google/callback
```

The flow is:
- open `/auth/google/login`
- complete Google consent
- callback stores access + refresh tokens in SQLite

## API Summary

### Health

- `GET /health`

### Auth

- `GET /auth/google/login`
- `GET /auth/google/callback`

### Appointments

- `GET /appointments?from=<RFC3339>&to=<RFC3339>`
- `POST /appointments`

Example create payload:

```json
{
  "title": "Team Sync",
  "startAt": "2026-04-23T09:00:00Z",
  "endAt": "2026-04-23T10:00:00Z",
  "syncGoogle": true
}
```

`endAt` is optional. If omitted, the backend defaults the appointment end time to one hour after `startAt`.

If `from` and `to` are omitted, the backend defaults to a window from now until 30 days ahead.

## Development

Run tests:

```bash
GOCACHE=$PWD/.gocache GOMODCACHE=$PWD/.gomodcache go test ./...
```

Build the app:

```bash
GOCACHE=$PWD/.gocache GOMODCACHE=$PWD/.gomodcache go build -o app ./backend/cmd
```

## Production Deployment

GitHub Actions deploys pushes to `main` to the VPS and serves the app at:

```text
https://manle.info
```

Required repository secrets:

| Secret | Description |
| --- | --- |
| `VPS_HOST` | VPS IP or hostname |
| `VPS_USER` | SSH user, currently `root` |
| `VPS_PASSWORD` | SSH password |
| `APP_ADDR` | Production bind address from `.env.prod` |
| `DATABASE_PATH` | Production SQLite path from `.env.prod` |
| `GOOGLE_CLIENT_ID` | Production Google OAuth client ID |
| `GOOGLE_CLIENT_SECRET` | Production Google OAuth client secret |
| `GOOGLE_REDIRECT_URL` | Production OAuth callback URL from `.env.prod` |

Production GitHub Secrets should be synced from `.env.prod`. Keep `.env` local only for development.

The production workflow writes those secret values to `/opt/reminder/.env` on the VPS. For `manle.info`, the production callback URL should be:

```env
GOOGLE_REDIRECT_URL=https://manle.info/auth/google/callback
```

Make sure DNS has an `A` record from `manle.info` to the VPS IP before the first deploy. Caddy listens on ports `80` and `443`, proxies to the app using `APP_ADDR`, obtains the SSL certificate automatically, and renews it through its persistent Docker volumes.

## Current v1 Limitations

- credentials are stored in SQLite without encryption
- no update/delete event API
- no webhook sync
- no background sync
- no deduplication or conflict handling
- single-user deployment model
