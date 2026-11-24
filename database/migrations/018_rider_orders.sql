-- Durable request, pricing, assignment, and proof-of-delivery details for
-- passenger bike rides and dispatch jobs.
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS pickup_address text NOT NULL DEFAULT '';
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS dropoff_address text NOT NULL DEFAULT '';
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS platform_fee_ngn bigint NOT NULL DEFAULT 20 CHECK(platform_fee_ngn>=0);
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS total_paid_ngn bigint NOT NULL DEFAULT 0 CHECK(total_paid_ngn>=0);
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS payment_status text NOT NULL DEFAULT 'PAID' CHECK(payment_status IN ('PENDING','PAID','REFUNDED'));
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS payment_reference text;
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE ride_orders ADD COLUMN IF NOT EXISTS completed_at timestamptz;
CREATE INDEX IF NOT EXISTS ride_orders_open_idx ON ride_orders(created_at) WHERE status='OPEN';
CREATE INDEX IF NOT EXISTS ride_orders_rider_active_idx ON ride_orders(biker_id,status) WHERE status IN ('ACCEPTED','IN_PROGRESS');

ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS pickup_address text NOT NULL DEFAULT '';
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS dropoff_address text NOT NULL DEFAULT '';
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS parcel_description text NOT NULL DEFAULT '';
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS platform_fee_ngn bigint NOT NULL DEFAULT 20 CHECK(platform_fee_ngn>=0);
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS total_paid_ngn bigint NOT NULL DEFAULT 0 CHECK(total_paid_ngn>=0);
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS payment_status text NOT NULL DEFAULT 'PAID' CHECK(payment_status IN ('PENDING','PAID','REFUNDED'));
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS payment_reference text;
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE parcel_orders ADD COLUMN IF NOT EXISTS completed_at timestamptz;
CREATE INDEX IF NOT EXISTS parcel_orders_open_idx ON parcel_orders(created_at) WHERE status='OPEN';
CREATE INDEX IF NOT EXISTS parcel_orders_rider_active_idx ON parcel_orders(rider_id,status) WHERE status IN ('ACCEPTED','IN_PROGRESS');

CREATE TABLE IF NOT EXISTS rider_order_idempotency (
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 idempotency_key text NOT NULL,
 order_type text NOT NULL CHECK(order_type IN ('RIDE','PARCEL')),
 order_id uuid NOT NULL,
 payload_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,idempotency_key)
);
