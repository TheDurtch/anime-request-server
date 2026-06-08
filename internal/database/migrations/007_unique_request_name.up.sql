-- Enforce case-insensitive uniqueness of request names, matching the
-- application's duplicate check (LOWER(name)). This closes the race where two
-- concurrent submissions both pass the app-level check and insert duplicates.
--
-- NOTE: this migration FAILS if the table already contains case-insensitive
-- duplicate names (e.g. created via batch add, which previously skipped the
-- duplicate check). De-duplicate first if needed, keeping the earliest row:
--   DELETE FROM anime_requests a USING anime_requests b
--   WHERE a.ctid > b.ctid AND LOWER(a.name) = LOWER(b.name);
CREATE UNIQUE INDEX idx_anime_requests_name_lower ON anime_requests (LOWER(name));
