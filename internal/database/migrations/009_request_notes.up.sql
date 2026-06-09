-- Notes on requests: a running, attributed log shown on the request detail page.
-- Any user may post by default; the users.notes_blocked flag revokes posting.
CREATE TABLE request_notes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id UUID NOT NULL REFERENCES anime_requests(id) ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES users(id),
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_request_notes_request ON request_notes (request_id, created_at);

-- Per-user block on posting notes (default false = may post), mirroring
-- can_batch_add / disabled. Admin-controlled, for abuse mitigation.
ALTER TABLE users ADD COLUMN notes_blocked BOOLEAN NOT NULL DEFAULT false;
