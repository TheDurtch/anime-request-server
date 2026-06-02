-- Create junction table for many-to-many relationship between requests and server destinations
CREATE TABLE request_server_destinations (
    request_id UUID NOT NULL REFERENCES anime_requests(id) ON DELETE CASCADE,
    server_destination_id UUID NOT NULL REFERENCES server_destinations(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (request_id, server_destination_id)
);

CREATE INDEX idx_request_server_destinations_request ON request_server_destinations(request_id);
CREATE INDEX idx_request_server_destinations_server ON request_server_destinations(server_destination_id);

-- Migrate existing server_destination_id data to junction table
INSERT INTO request_server_destinations (request_id, server_destination_id)
SELECT id, server_destination_id
FROM anime_requests
WHERE server_destination_id IS NOT NULL;

-- Remove the old server_destination_id column from anime_requests
ALTER TABLE anime_requests DROP COLUMN server_destination_id;
