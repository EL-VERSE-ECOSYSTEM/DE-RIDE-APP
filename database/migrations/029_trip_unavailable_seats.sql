CREATE TABLE IF NOT EXISTS trip_unavailable_seats (
    trip_id uuid NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    seat_code text NOT NULL CHECK (seat_code ~ '^[1-9][0-9]{0,2}$'),
    reason text NOT NULL DEFAULT 'UNAVAILABLE' CHECK (reason IN ('UNAVAILABLE','DAMAGED','ACCESSIBILITY')),
    set_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (trip_id, seat_code)
);

CREATE INDEX IF NOT EXISTS trip_unavailable_seats_trip_idx ON trip_unavailable_seats(trip_id);
