ALTER TABLE trips ADD COLUMN IF NOT EXISTS vehicle_registration_id uuid REFERENCES vehicle_registrations(id);
CREATE INDEX IF NOT EXISTS trips_open_departure_idx ON trips(departure_at) WHERE status='OPEN';
CREATE INDEX IF NOT EXISTS trips_park_departure_idx ON trips(park_id,departure_at DESC);
ALTER TABLE tickets DROP CONSTRAINT IF EXISTS tickets_trip_id_seat_code_key;
CREATE UNIQUE INDEX IF NOT EXISTS tickets_active_seat_unique_idx ON tickets(trip_id,seat_code) WHERE status NOT IN ('CANCELLED','REFUNDED');
CREATE INDEX IF NOT EXISTS tickets_passenger_created_idx ON tickets(passenger_id,trip_id);
