# anime-request-server

A Go server for managing anime requests — includes an embedded web UI and a JSON API.

## What the server does

The server is a **request board**. It presents what anime is wanted and what state each request is in. It does **not** acquire, download, or monitor anything — all status changes are made manually by admin/mod users.

## Quick start

```bash
# 1. Set environment variables (or create a .env file)
export DATABASE_URL="******localhost:5432/anime_requests?sslmode=disable"
export SESSION_SECRET="your-random-secret-at-least-32-chars"

# 2. Initialize the database and create admin user
anime-request-server init

# 3. Start the server
anime-request-server serve
```

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `SESSION_SECRET` | Yes | — | Secret for session token hashing |
| `SERVER_HOST` | No | `0.0.0.0` | Bind address |
| `SERVER_PORT` | No | `8080` | HTTP port |
| `WEBUI_ENABLED` | No | `true` | Set to `false` for API-only mode |

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
| `mod`   | Change request status/category, assign server destination, add AniDB URL, manage server destinations. |
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
   - **server destination** → a managed server name (e.g. "Server A").
   - **AniDB URL** → link to the show page.

## Batch add

Users with the `can_batch_add` permission (granted by admin) can submit multiple show names at once:

```json
POST /api/v1/requests/batch
{ "names": ["Show A", "Show B", "Show C"], "category": "batch_add" }
```

All entries are created with category `batch_add` so mods know they haven't been categorized yet.

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
| `name`                | required — show title                              |
| `category`            | `current_future` / `finished_airing` / `batch_add` |
| `status`              | `new` / `done` / `need_to_get` / `acquiring` / `processing` / `syncing` (default `new`) |
| `requested_by`        | FK → users.id                                      |
| `server_destination_id` | nullable, FK → server_destinations.id            |
| `anidb_url`           | nullable — added by admin/mod                      |
| `created_at`          |                                                    |
| `updated_at`          |                                                    |

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
- `POST   /api/v1/requests` — create request (name + category)
- `POST   /api/v1/requests/batch` — batch add (requires `can_batch_add` permission)
- `GET    /api/v1/requests/{id}`
- `PATCH  /api/v1/requests/{id}` — update (admin/mod: status, category, server, anidb_url)

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
- Server-side sessions stored in PostgreSQL.
- Constant-time comparison for credential checks.
- Rate-limited login endpoint (in-memory, per IP).
- Session tokens are SHA-256 hashed before storage.
- Optional TOTP 2FA (non-mandatory).
- Secrets via environment variables only.
- Assumes reverse proxy or LAN use — no built-in HTTPS.

## Future TODOs

- Discord webhooks for new requests and status changes (with silent option).
- Import/export requests as JSON.
- Status change audit log.
- Prometheus metrics endpoint.
