ALTER TABLE escalations ADD COLUMN IF NOT EXISTS gps_snapshot geography(Point,4326);
CREATE INDEX IF NOT EXISTS escalations_open_trip_idx ON escalations(trip_id,created_at) WHERE status IN ('OPEN','IN_PROGRESS');
