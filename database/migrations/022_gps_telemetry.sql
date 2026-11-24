ALTER TABLE trips ADD COLUMN IF NOT EXISTS current_location geography(Point,4326);
ALTER TABLE trips ADD COLUMN IF NOT EXISTS gps_updated_at timestamptz;
CREATE INDEX IF NOT EXISTS trips_current_location_gix ON trips USING gist(current_location);
