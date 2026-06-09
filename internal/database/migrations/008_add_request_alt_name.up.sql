-- Add an optional alternate name (alias) to requests, e.g. the English title
-- "The Apothecary Diaries" for "Kusuriya no Hitorigoto". The alternate name
-- counts as a duplicate alongside the primary name.
--
-- Safe on existing data: alt_name starts all-NULL, so neither the unique index
-- nor the CHECK can fail on backfill.
ALTER TABLE anime_requests ADD COLUMN alt_name TEXT;

-- Alt names are globally unique (case-insensitive), like the primary name. This
-- enforces alt<->alt collisions; name<->name is covered by migration 007. The
-- cross case (one row's name == another row's alt_name) is enforced in the app.
CREATE UNIQUE INDEX idx_anime_requests_alt_name_lower
    ON anime_requests (LOWER(alt_name)) WHERE alt_name IS NOT NULL;

-- A row's alternate name may not equal its own primary name.
ALTER TABLE anime_requests
    ADD CONSTRAINT chk_alt_name_differs
    CHECK (alt_name IS NULL OR LOWER(alt_name) <> LOWER(name));
