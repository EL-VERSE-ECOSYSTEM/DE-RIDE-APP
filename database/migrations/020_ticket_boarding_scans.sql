ALTER TABLE tickets ADD COLUMN IF NOT EXISTS scanned_at timestamptz;
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS scanned_by uuid REFERENCES users(id);
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS scanned_offline boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS tickets_trip_unscanned_idx ON tickets(trip_id,status) WHERE scanned_at IS NULL;
