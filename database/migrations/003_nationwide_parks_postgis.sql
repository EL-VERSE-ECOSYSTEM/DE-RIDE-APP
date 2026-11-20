CREATE TABLE IF NOT EXISTS bus_parks(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, state_code char(2) NOT NULL, launch_enabled boolean NOT NULL DEFAULT false, boundary geography(MultiPolygon,4326), location geography(Point,4326), created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS bus_parks_location_gix ON bus_parks USING gist(location);
