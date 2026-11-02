# Taka Poisa

A money ledger ("hisab khata") that reminds people for you: record money you lent or borrowed,
split it into installments, and Taka Poisa emails the other person until it is paid.

Open source and self-hostable (one binary + PostgreSQL). See [the plan](docs/plan/taka-poisa.md).

> Status: Phase 0. Owner setup, password and Google login, and ledger settings work; the ledger itself is next.

## Self-host with Docker

```bash
cp .env.example .env   # set POSTGRES_PASSWORD and BASE_URL
docker compose up -d
```

Open `BASE_URL` (default http://localhost:8080). The first visitor creates the owner account.
Put it behind HTTPS (Caddy, nginx, Traefik) for anything beyond your own machine; when
`BASE_URL` starts with `https://`, session cookies are marked `Secure`.

Backups: `docker compose exec db pg_dump -U takapoisa takapoisa > backup.sql`.

### Configuration

| Variable | Default | |
|---|---|---|
| `DATABASE_URL` | (required) | PostgreSQL connection URL |
| `BASE_URL` | `http://localhost:8080` | Public URL of the app |
| `LISTEN_ADDR` | `:8080` | Address the server listens on |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | (empty) | Optional Google sign-in, see below |

Migrations run automatically on start.

### Google sign-in (optional)

1. In [Google Cloud Console → Credentials](https://console.cloud.google.com/apis/credentials), create an
   OAuth client ID of type **Web application**.
2. Add the authorized redirect URI `<BASE_URL>/api/auth/google/callback`.
3. Set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, then restart.

The owner can set up the server with Google. After that, Google sign-in works for existing users whose
Google-verified email matches their account; it does not let new people sign up.

## Develop

Needs Go 1.26+, Node 22 + pnpm, and Docker (for Postgres).

```bash
docker run -d --name tp-dev-db -e POSTGRES_PASSWORD=dev -p 5432:5432 postgres:18-alpine
```

```bash
DATABASE_URL='postgres://postgres:dev@localhost:5432/postgres?sslmode=disable' go run ./cmd/takapoisa
```

```bash
cd web && pnpm install && pnpm dev   # http://localhost:5173, proxies /api to :8080
```

`pnpm build` writes the UI into `internal/webui/dist`, which `go build` embeds.

### Tests

```bash
TEST_DATABASE_URL='postgres://postgres:dev@localhost:5432/postgres?sslmode=disable' go test ./...
```

```bash
cd web && pnpm check
```

API tests create and drop their own temporary database; without `TEST_DATABASE_URL` they are skipped.

## Layout

```
cmd/takapoisa/      entry point: config, migrate, HTTP server
internal/auth/      users, owner setup, passwords (argon2id), sessions
internal/account/   ledger settings
internal/httpapi/   JSON API, CSRF protection, rate limiting, static UI
internal/db/        Postgres pool + goose migrations
internal/webui/     embedded UI build
migrations/         SQL migrations
web/                SvelteKit app
```

## License

AGPL-3.0 (license file to be added).
