ALTER TABLE trips ADD COLUMN IF NOT EXISTS departed_at timestamptz;
ALTER TABLE trips ADD COLUMN IF NOT EXISTS cancellation_reason text;
CREATE INDEX IF NOT EXISTS trips_lifecycle_idx ON trips(status,departure_at);
