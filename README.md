# anime-request-server

A GoLang server for anime-request webui.

## What the server does

The server is a **request board**. It presents what anime is wanted and what state each request is in. It does **not** acquire, download, or monitor anything — all status changes are made manually by admin/mod users.

## Baseline requirements

1. Persistent storage in **SQLite** (default) or **PostgreSQL** (production).
2. Anime request entries with:
   - **Required on creation (by any user):** show name, category (`current/future` or `finished airing`).
   - **Set later by admin/mod:** status, server destination, AniDB URL.
3. User authentication (passwords hashed + salted, optional keyfile support).
4. HTTP/JSON API for the Web UI.

## User roles

| Role    | Can do                                                                 |
|---------|------------------------------------------------------------------------|
| `admin` | Everything. Create invite codes, manage users, CLI access, full CRUD.  |
| `mod`   | Change request status, assign server destination, add AniDB URL.       |
| `user`  | Create requests (name + category). View all requests.                  |

## User signup

- **No open registration.** Users sign up via:
  1. CLI — admin creates accounts directly.
  2. Invite code — admin generates a code via CLI/admin console; user redeems it to create an account.

## Request lifecycle (example)

1. A **user** requests "Eizouken ni wa Te wo Dasu na!" and labels it **finished airing**.
2. An **admin/mod** sees the new request and reviews it.
3. The admin/mod updates:
   - **status** → `done` (already have it), or `need to get`, `acquiring`, `processing`, `syncing`, `done`.
   - **server destination** → Server A or Server B.
   - **AniDB URL** → link to the show page.

## Data model

### `users`
| Column          | Notes                                      |
|-----------------|--------------------------------------------|
| `id`            | PK                                         |
| `username`      | unique                                     |
| `email`         | unique, optional                           |
| `password_hash` | Argon2id or bcrypt                         |
| `password_salt` | per-user random salt                       |
| `keyfile_hash`  | nullable                                   |
| `role`          | `admin` / `mod` / `user`                   |
| `created_at`    |                                            |
| `updated_at`    |                                            |

### `anime_requests`
| Column              | Notes                                              |
|---------------------|----------------------------------------------------|
| `id`                | PK                                                 |
| `name`              | required — show title                              |
| `category`          | `current_future` or `finished_airing`               |
| `status`            | `new` / `done` / `need_to_get` / `acquiring` / `processing` / `syncing` (default `new`) |
| `requested_by`      | FK → users.id                                      |
| `server_destination`| nullable — e.g. "Server A", "Server B"             |
| `anidb_url`         | nullable — added by admin/mod                      |
| `created_at`        |                                                    |
| `updated_at`        |                                                    |

### `invite_codes`
| Column       | Notes                                |
|--------------|--------------------------------------|
| `id`         | PK                                   |
| `code`       | unique, random token                 |
| `created_by` | FK → users.id (admin who created it) |
| `used_by`    | FK → users.id, nullable              |
| `expires_at` | nullable                             |
| `created_at` |                                      |

## Category filtering

The Web UI should:

- Show separate sections/tabs for **Current / Future** and **Finished Airing** requests.
- Provide a filter for `category` (`all`, `current_future`, `finished_airing`).
- Allow filtering by `status` within each category.

## API baseline

### Auth
- `POST /auth/login`
- `POST /auth/logout`
- `POST /auth/redeem-invite` — create account using an invite code

### Requests (all authenticated)
- `GET    /requests` — list requests (filters: `category`, `status`, `requested_by`)
- `POST   /requests` — create request (user: name + category)
- `GET    /requests/{id}`
- `PATCH  /requests/{id}` — update request (admin/mod: status, server, anidb_url)

### Admin (admin only)
- `POST   /admin/invite-codes` — generate invite code
- `GET    /admin/invite-codes` — list invite codes
- `POST   /admin/users` — create user via CLI
- `GET    /admin/users` — list users
- `PATCH  /admin/users/{id}` — change role, disable account

## Security

- Never store plaintext passwords or keyfiles.
- Use constant-time compare for credential checks.
- Rate-limit login endpoint.
- Secure session tokens or JWT with short expiration + refresh.
- Require HTTPS in production.
- Keep secrets in environment variables (not source control).

## Additional ideas

- Duplicate detection by show name or AniDB URL before creating entries.
- Notifications (Discord webhook) when a request status changes.
- Import/export requests as JSON for backup.
- Status change audit log.
- Metrics/health endpoint.

## Suggested implementation phases

1. Schema + migrations + repository layer (SQLite first).
2. Auth + invite code signup + session management.
3. Request CRUD + category/status filtering + role-based permissions.
4. CLI admin commands (create user, generate invite code).
5. Web UI integration.
