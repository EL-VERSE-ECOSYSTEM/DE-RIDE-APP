CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE TABLE IF NOT EXISTS users (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email citext UNIQUE NOT NULL, password_hash text NOT NULL,
 active_role text NOT NULL CHECK(active_role IN ('PASSENGER','DRIVER','DISPATCHER','BIKER','DISPATCH_RIDER','PARK_OPERATOR','ADMIN')),
 roles text[] NOT NULL DEFAULT '{}', biker_approved boolean NOT NULL DEFAULT false, dispatch_approved boolean NOT NULL DEFAULT false,
 gps_opt_in boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
