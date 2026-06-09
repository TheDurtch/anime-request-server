ALTER TABLE anime_requests DROP CONSTRAINT IF EXISTS chk_alt_name_differs;
DROP INDEX IF EXISTS idx_anime_requests_alt_name_lower;
ALTER TABLE anime_requests DROP COLUMN IF EXISTS alt_name;
