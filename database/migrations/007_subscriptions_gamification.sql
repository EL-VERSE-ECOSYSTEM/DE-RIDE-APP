CREATE TABLE IF NOT EXISTS subscription_plans(id text PRIMARY KEY, name text NOT NULL, price_ngn bigint NOT NULL CHECK(price_ngn>=0), eligible_roles text[] NOT NULL, interval text NOT NULL);
CREATE TABLE IF NOT EXISTS subscriptions(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id), plan_id text NOT NULL REFERENCES subscription_plans(id), starts_at timestamptz NOT NULL, ends_at timestamptz, status text NOT NULL);
CREATE TABLE IF NOT EXISTS ranks(id text PRIMARY KEY, role text NOT NULL, trips_required int NOT NULL, prize_ngn bigint NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS medals(id text PRIMARY KEY, name text NOT NULL, criteria jsonb NOT NULL);
