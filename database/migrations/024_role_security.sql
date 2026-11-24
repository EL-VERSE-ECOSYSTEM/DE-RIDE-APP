ALTER TABLE users ADD COLUMN IF NOT EXISTS suspended_at timestamptz;
UPDATE users u SET roles=ARRAY(SELECT DISTINCT r FROM unnest(COALESCE(u.roles,'{}'::text[])||ARRAY[u.active_role]) AS r WHERE r=ANY(ARRAY['PASSENGER','DRIVER','DISPATCHER','BIKER','DISPATCH_RIDER','PARK_OPERATOR','ADMIN']::text[]));
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='users_roles_allowed_values') THEN
  ALTER TABLE users ADD CONSTRAINT users_roles_allowed_values CHECK(roles <@ ARRAY['PASSENGER','DRIVER','DISPATCHER','BIKER','DISPATCH_RIDER','PARK_OPERATOR','ADMIN']::text[]);
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='users_active_role_granted') THEN
  ALTER TABLE users ADD CONSTRAINT users_active_role_granted CHECK(active_role=ANY(roles));
 END IF;
END $$;

CREATE TABLE IF NOT EXISTS role_requests(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 requested_role text NOT NULL CHECK(requested_role IN ('DRIVER','DISPATCHER','BIKER','DISPATCH_RIDER','PARK_OPERATOR')),
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','APPROVED','REJECTED')),
 requested_at timestamptz NOT NULL DEFAULT now(),
 reviewed_by uuid REFERENCES users(id),
 reviewed_at timestamptz,
 review_note text
);
CREATE UNIQUE INDEX IF NOT EXISTS role_requests_pending_user_role_idx ON role_requests(user_id,requested_role) WHERE status='PENDING';
CREATE INDEX IF NOT EXISTS role_requests_admin_queue_idx ON role_requests(requested_at) WHERE status='PENDING';

CREATE TABLE IF NOT EXISTS role_audit_log(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 actor_id uuid NOT NULL REFERENCES users(id),
 action text NOT NULL CHECK(action IN ('REQUESTED','GRANTED','REJECTED','ACTIVATED','SUSPENDED','RESTORED')),
 role text NOT NULL,
 note text,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS role_audit_user_idx ON role_audit_log(user_id,created_at DESC);
