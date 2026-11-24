INSERT INTO subscription_plans(id,name,price_ngn,eligible_roles,interval) VALUES
('commuter_lite','Commuter Lite',0,ARRAY['PASSENGER'],'month'),
('swift_pass','Swift Pass',2500,ARRAY['PASSENGER'],'month'),
('executive_club','Executive Club',5000,ARRAY['PASSENGER'],'month'),
('sovereign_60k','Founding Sovereign',60000,ARRAY['PASSENGER'],'one_time'),
('passenger_lifetime','Passenger Lifetime',60000,ARRAY['PASSENGER'],'one_time'),
('operator_free','Operations Free',0,ARRAY['DRIVER','DISPATCHER','BIKER','DISPATCH_RIDER','PARK_OPERATOR'],'month'),
('pro_5k','Fleet & Rider Pro',5000,ARRAY['DRIVER','DISPATCHER','BIKER','DISPATCH_RIDER'],'month'),
('terminal_15k','Terminal Command',15000,ARRAY['PARK_OPERATOR'],'month'),
('bus_per_trip','Bus per trip',250,ARRAY['DRIVER','DISPATCHER','PARK_OPERATOR'],'per_trip')
ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,price_ngn=EXCLUDED.price_ngn,eligible_roles=EXCLUDED.eligible_roles,interval=EXCLUDED.interval;

ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS downgrade_plan_id text REFERENCES subscription_plans(id);
