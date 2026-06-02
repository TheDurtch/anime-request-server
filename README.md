# anime-request-server

A GoLang server for anime-request webui.

## Baseline goals

The server baseline should provide:

1. Persistent storage in **SQLite** (default) or **PostgreSQL** (production/multi-user).
2. Anime request tracking with required fields:
   - `name`
   - `anidb_url`
   - `status` (requested / approved / in-progress / complete / rejected)
   - `requested_by_user_id`
   - `priority` (low / normal / high / urgent)
   - `plex_destination`
3. User authentication with:
   - unique username/email
   - password hashing using a modern KDF (Argon2id or bcrypt)
   - per-user random salt
   - optional keyfile support (hashed, never stored in plain text)
4. HTTP API designed for a future Web UI client.

## Data model baseline

### `users`
- `id` (PK)
- `username` (unique)
- `email` (unique, optional)
- `password_hash`
- `password_salt`
- `keyfile_hash` (nullable)
- `role` (admin / user)
- `created_at`, `updated_at`

### `anime_entries`
- `id` (PK)
- `name`
- `anidb_url`
- `status`
- `requested_by_user_id` (FK -> users.id)
- `priority`
- `plex_destination`
- `tracking_type` (`airing` or `completed`)
- `episode_count_total` (nullable for airing)
- `episode_count_acquired`
- `next_expected_episode_date` (nullable)
- `last_checked_at` (nullable)
- `created_at`, `updated_at`

### Optional supporting tables (next step)
- `entry_status_history` (audit log for status changes)
- `entry_comments` (discussion between requester/admin)
- `release_checks` (external release polling records)

## Separate tracking workflows (new requirement)

### Airing anime workflow
Use for shows releasing over time.

- Entry starts as `tracking_type=airing`.
- Store known/estimated total episodes (nullable until known).
- Periodically check for new episodes and update `episode_count_acquired`.
- Keep `next_expected_episode_date` and `last_checked_at`.
- Mark collection complete when the season ends and all episodes are acquired.

### Completed anime workflow
Use for shows where all episodes are already available.

- Entry starts as `tracking_type=completed`.
- Total episode count should be known at creation when possible.
- Focus on one-time completion progress to full collection.
- Once all episodes are acquired, mark as finished collection.

### UI separation/filtering expectations

The Web UI should:

- Show separate sections/tabs for **Airing** and **Completed** entries.
- Provide a filter control for `tracking_type` (`all`, `airing`, `completed`).
- Allow status/priority filtering within each category.
- Show category-specific columns:
  - Airing: next expected episode, last checked date.
  - Completed: total episodes, acquired/remaining progress.

## API baseline (for Web UI integration)

- `POST /auth/register`
- `POST /auth/login`
- `POST /auth/logout`
- `GET /entries` (supports filters: `tracking_type`, `status`, `priority`, `requested_by`)
- `POST /entries`
- `GET /entries/{id}`
- `PATCH /entries/{id}`
- `GET /entries/{id}/history`

## Security baseline

- Never store plaintext passwords or keyfiles.
- Use constant-time compare for credential checks.
- Rate-limit login endpoint.
- Use secure session tokens/JWT with short expiration + refresh strategy.
- Require HTTPS in production.
- Keep secrets in environment variables or secret manager (not source control).

## Additional ideas

- Role-based access control (admin can approve/reprioritize; users can request and track).
- Notifications (Discord/email/webhook) for status changes or new airing episode detected.
- Duplicate detection by AniDB URL before creating entries.
- Import/export entries as JSON for backup and migration.
- Background job worker for airing release checks.
- Metrics/health endpoints for operations and observability.

## Suggested implementation phases

1. Schema + migrations + repository layer (SQLite first, PostgreSQL compatibility).
2. Auth + session management + protected API routes.
3. Entry CRUD + filters + airing/completed workflow logic.
4. Background release monitor for airing entries.
5. Web UI integration and polishing.
