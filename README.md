# anime-request-server

A Go server for managing anime requests — includes an embedded web UI and a JSON API.

## What the server does

The server is a **request board**. It presents what anime is wanted and what state each request is in. It does **not** acquire, download, or monitor anything — all status changes are made manually by admin/mod users.

## Prerequisites

- **Go 1.25+** — required to build from source
- **PostgreSQL** — the only supported database backend

## Building from source

```bash
go build ./cmd/anime-request-server/
```

This produces a single `anime-request-server` binary with the web UI embedded.

## Quick start

```bash
# 1. Set environment variables (or copy .env.example)
export DATABASE_URL="******localhost:5432/anime_requests?sslmode=disable"
# SESSION_SECRET is currently unused (optional). For local dev over plain
# HTTP, disable Secure cookies so the session cookie is actually sent:
export COOKIE_SECURE=false

# 2. Initialize the database and create admin user
./anime-request-server init

# 3. Start the server
./anime-request-server serve
```

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `SESSION_SECRET` | No | — | Currently unused; reserved for future cookie signing |
| `SERVER_HOST` | No | `0.0.0.0` | Bind address |
| `SERVER_PORT` | No | `8080` | HTTP port |
| `WEBUI_ENABLED` | No | `true` | Set to `false` for API-only mode |
| `REAL_IP_HEADER` | No | — | Proxy header trusted for the client IP in login rate limiting. Empty = trust none (use TCP peer). E.g. `CF-Connecting-IP`, `X-Forwarded-For` |
| `COOKIE_SECURE` | No | `true` | `Secure` flag on session cookies; set `false` only for local HTTP dev |
| `LOG_REQUEST_IPS` | No | `false` | Log `RemoteAddr`, every forwarding header, and the derived client IP per request. Useful to verify `REAL_IP_HEADER` behind a proxy; logs client IPs (PII), so leave off in normal operation |

## CLI commands

```bash
anime-request-server init                    # Run migrations + create admin user
anime-request-server serve                   # Start the HTTP server
anime-request-server create-user \
  --username alice --password secret123 \
  --role user                                # Create a user directly
anime-request-server generate-invite \
  --expires-in-hours 168                     # Generate an invite code (7 days)
```

## User roles

| Role    | Can do                                                                 |
|---------|------------------------------------------------------------------------|
| `admin` | Everything. Create invite codes, manage users, CLI access, full CRUD.  |
| `mod`   | Rename, set an alternate name, edit (status/category, assign server destination, add AniDB URL) and delete requests; manage server destinations. |
| `user`  | Create requests (name + category). View all requests. Batch add (if granted). |

## User signup

- **No open registration.** Users sign up via:
  1. CLI — admin creates accounts directly.
  2. Invite code — admin generates a code via CLI or web UI; user redeems it to create an account.

## Request lifecycle

1. A **user** requests "Eizouken ni wa Te wo Dasu na!" and labels it **finished airing**.
2. An **admin/mod** sees the new request and reviews it.
3. The admin/mod updates:
   - **status** → `need_to_get`, `acquiring`, `processing`, `syncing`, `done`.
   - **category** → can change `batch_add` to `current_future` or `finished_airing`.
   - **server destinations** → one or more managed server names (e.g. "Server A", "Server B").
   - **AniDB URL** → link to the show page; must be a valid http(s) URL, and cannot be cleared once set.

## Batch add

Users with the `can_batch_add` permission (granted by admin) can submit multiple show names at once:

```json
POST /api/v1/requests/batch
{ "names": ["Show A", "Show B", "Show C"], "category": "batch_add" }
```

All entries are created with category `batch_add` so mods know they haven't been categorized yet. Names that duplicate an existing request (case-insensitive) are skipped; the response reports how many were actually added.

## Data model

### `users`
| Column          | Notes                                      |
|-----------------|--------------------------------------------|
| `id`            | UUID PK                                    |
| `username`      | unique                                     |
| `email`         | unique, optional                           |
| `password_hash` | bcrypt hash                                |
| `totp_secret`   | nullable — TOTP 2FA secret                 |
| `totp_enabled`  | default false                              |
| `role`          | `admin` / `mod` / `user`                   |
| `can_batch_add` | default false — granted by admin           |
| `disabled`      | default false                              |
| `created_at`    |                                            |
| `updated_at`    |                                            |

### `sessions`
| Column       | Notes                                |
|--------------|--------------------------------------|
| `id`         | UUID PK                              |
| `user_id`    | FK → users.id                        |
| `token_hash` | SHA-256 of session token             |
| `expires_at` | auto-expire                          |
| `created_at` |                                      |

### `server_destinations`
| Column       | Notes                                |
|--------------|--------------------------------------|
| `id`         | UUID PK                              |
| `name`       | unique                               |
| `created_by` | FK → users.id                        |
| `created_at` |                                      |

### `anime_requests`
| Column                | Notes                                              |
|-----------------------|----------------------------------------------------|
| `id`                  | UUID PK                                            |
| `name`                | required — show title; unique (case-insensitive)   |
| `alt_name`            | nullable — alternate title (e.g. English); set by mod/admin; unique (case-insensitive) and counts as a duplicate |
| `category`            | `current_future` / `finished_airing` / `batch_add` |
| `status`              | `new` / `done` / `need_to_get` / `acquiring` / `processing` / `syncing` (default `new`) |
| `requested_by`        | FK → users.id                                      |
| `anidb_url`           | nullable — added by admin/mod                      |
| `created_at`          |                                                    |
| `updated_at`          |                                                    |

### `request_server_destinations` (junction table)
| Column                  | Notes                                |
|-------------------------|--------------------------------------|
| `request_id`            | FK → anime_requests.id               |
| `server_destination_id` | FK → server_destinations.id          |
| `added_at`              | timestamp of when destination was added |
| PK                      | composite (`request_id`, `server_destination_id`) |

### `invite_codes`
| Column       | Notes                                |
|--------------|--------------------------------------|
| `id`         | UUID PK                              |
| `code`       | unique, random token                 |
| `created_by` | FK → users.id (admin who created it) |
| `used_by`    | FK → users.id, nullable              |
| `expires_at` | nullable                             |
| `created_at` |                                      |

## Web UI

The web UI is embedded in the server binary and served at `/` by default. It features:

- Dark mode (default theme)
- Category tabs: All, Current/Future, Finished Airing, Batch Add
- Status filtering within each category
- Request creation and detail views
- Admin panels: user management, invite codes, server destinations

Set `WEBUI_ENABLED=false` to disable the web UI and run in API-only mode.

## API

All API endpoints are under `/api/v1/`.

### Auth
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/logout`
- `POST /api/v1/auth/redeem-invite` — create account using an invite code

### Requests (authenticated)
- `GET    /api/v1/requests` — list (filters: `category`, `status`, `requested_by`, `page`, `per_page`)
- `POST   /api/v1/requests` — create request (name + category; admin/mod may also set `alt_name`, `status`, `anidb_url`, `server_destination_ids`)
- `POST   /api/v1/requests/batch` — batch add (requires `can_batch_add` permission)
- `GET    /api/v1/requests/{id}`
- `PATCH  /api/v1/requests/{id}` — update (admin/mod: name, alt_name, status, category, anidb_url)
- `DELETE /api/v1/requests/{id}` — delete request (admin/mod)
- `POST   /api/v1/requests/{id}/destinations` — add server destination (admin/mod)
- `DELETE /api/v1/requests/{id}/destinations/{dest_id}` — remove server destination (admin/mod)

### Server destinations (admin/mod)
- `GET    /api/v1/server-destinations`
- `POST   /api/v1/server-destinations`
- `DELETE /api/v1/server-destinations/{id}`

### Admin (admin only)
- `POST   /api/v1/admin/invite-codes` — generate invite code
- `GET    /api/v1/admin/invite-codes` — list invite codes
- `POST   /api/v1/admin/users` — create user
- `GET    /api/v1/admin/users` — list users
- `PATCH  /api/v1/admin/users/{id}` — change role, toggle batch_add, disable account

### Health
- `GET /api/v1/health`

## Security

- Passwords hashed with bcrypt (pure Go, no CGO).
- Server-side sessions stored in PostgreSQL; tokens are SHA-256 hashed before storage.
- Login verification spends bcrypt time even for unknown usernames, to resist username enumeration via response timing.
- Rate-limited login endpoint (in-memory, per client IP) with temporary bans. **Behind a reverse proxy you must set `REAL_IP_HEADER`** to the header your proxy populates with the real client IP (e.g. `CF-Connecting-IP`, `X-Forwarded-For`), or every request shares the proxy's IP and the limiter is useless. If the server is directly exposed, leave it empty so spoofable forwarding headers are ignored.
- Session cookies are `HttpOnly` + `SameSite=Lax`, and `Secure` by default (`COOKIE_SECURE`).
- TOTP 2FA: login-time validation is implemented and fails closed when enabled, but there is **no enrollment flow yet**, so 2FA cannot currently be turned on through the app.
- Secrets via environment variables only.
- Assumes a reverse proxy or LAN use — no built-in HTTPS.

## Project structure

```
cmd/anime-request-server/   CLI entry point (init, serve, create-user, generate-invite)
internal/
  auth/                     Password hashing, session tokens, invite code generation
  config/                   Environment variable loading
  database/                 PostgreSQL connection, embedded migrations
  handler/api/              JSON API handlers and router
  handler/web/              Web UI handlers (HTML templates)
  middleware/               Auth middleware (session + role checks)
  models/                   Shared types (User, Request, Role, Status, etc.)
  repository/               Database queries (users, sessions, requests, invites, destinations)
web/
  static/                   CSS and JS assets (embedded)
  templates/                HTML templates (embedded)
```

## Future TODOs

- TOTP 2FA enrollment flow (generate secret + QR, enable per user) — login-time validation is already implemented.
- Discord webhooks for new requests and status changes (with silent option).
- Import/export requests as JSON.
- Status change audit log.
- Prometheus metrics endpoint.

## License

MIT — see [LICENSE](LICENSE) for details.
