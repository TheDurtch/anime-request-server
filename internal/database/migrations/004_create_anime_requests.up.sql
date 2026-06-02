CREATE TABLE anime_requests (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                   TEXT NOT NULL,
    category               TEXT NOT NULL CHECK (category IN ('current_future', 'finished_airing', 'batch_add')),
    status                 TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'done', 'need_to_get', 'acquiring', 'processing', 'syncing')),
    requested_by           UUID NOT NULL REFERENCES users(id),
    server_destination_id  UUID REFERENCES server_destinations(id) ON DELETE SET NULL,
    anidb_url              TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_anime_requests_category ON anime_requests(category);
CREATE INDEX idx_anime_requests_status ON anime_requests(status);
CREATE INDEX idx_anime_requests_requested_by ON anime_requests(requested_by);
