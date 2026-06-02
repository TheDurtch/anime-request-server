-- Restore the server_destination_id column
ALTER TABLE anime_requests ADD COLUMN server_destination_id UUID REFERENCES server_destinations(id) ON DELETE SET NULL;

-- Migrate back (take first destination if multiple exist)
UPDATE anime_requests ar
SET server_destination_id = (
    SELECT server_destination_id
    FROM request_server_destinations
    WHERE request_id = ar.id
    ORDER BY added_at ASC
    LIMIT 1
);

-- Drop junction table
DROP TABLE IF EXISTS request_server_destinations;
