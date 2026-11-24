-- Rank milestones and prizes from BUILD.docx. Idempotent inserts allow
-- existing installations to pick up the catalog during ordered migration.
INSERT INTO ranks(id,role,trips_required,prize_ngn) VALUES
 ('passenger_street_scout','PASSENGER',1,0),('passenger_highway_cruiser','PASSENGER',10,500),('passenger_express_voyager','PASSENGER',30,2000),('passenger_interstate_marshal','PASSENGER',75,5000),('passenger_deride_legend','PASSENGER',150,15000),
 ('rider_verified_pilot','BIKER',1,0),('rider_route_captain','BIKER',25,1500),('rider_highway_commander','BIKER',80,5000),('rider_transit_titan','BIKER',200,15000),('rider_grand_master','BIKER',500,50000),
 ('dispatch_verified_pilot','DISPATCH_RIDER',1,0),('dispatch_route_captain','DISPATCH_RIDER',25,1500),('dispatch_highway_commander','DISPATCH_RIDER',80,5000),('dispatch_transit_titan','DISPATCH_RIDER',200,15000),('dispatch_grand_master','DISPATCH_RIDER',500,50000),
 ('driver_verified_pilot','DRIVER',1,0),('driver_route_captain','DRIVER',25,1500),('driver_highway_commander','DRIVER',80,5000),('driver_transit_titan','DRIVER',200,15000),('driver_grand_master','DRIVER',500,50000),
 ('dispatcher_verified_pilot','DISPATCHER',1,0),('dispatcher_route_captain','DISPATCHER',25,1500),('dispatcher_highway_commander','DISPATCHER',80,5000),('dispatcher_transit_titan','DISPATCHER',200,15000),('dispatcher_grand_master','DISPATCHER',500,50000),
 ('operator_verified_pilot','PARK_OPERATOR',1,0),('operator_route_captain','PARK_OPERATOR',25,1500),('operator_highway_commander','PARK_OPERATOR',80,5000),('operator_transit_titan','PARK_OPERATOR',200,15000),('operator_grand_master','PARK_OPERATOR',500,50000)
ON CONFLICT(id) DO UPDATE SET role=EXCLUDED.role,trips_required=EXCLUDED.trips_required,prize_ngn=EXCLUDED.prize_ngn;

INSERT INTO medals(id,name,criteria) VALUES
 ('bronze_compass','Bronze Compass','{"rank":"Street Scout"}'),('silver_highway','Silver Highway','{"rank":"Highway Cruiser"}'),('gold_winged_wheel','Gold Winged Wheel','{"rank":"Express Voyager"}'),('platinum_shield','Platinum Shield','{"rank":"Interstate Marshal"}'),('diamond_crown','Diamond Crown','{"rank":"DE RIDE Legend"}'),
 ('bronze_steering','Bronze Steering','{"rank":"Verified Pilot"}'),('silver_piston','Silver Piston','{"rank":"Route Captain"}'),('gold_turbo','Gold Turbo','{"rank":"Highway Commander"}'),('platinum_crest','Platinum Crest','{"rank":"Transit Titan"}'),('diamond_emblem','Diamond Emblem','{"rank":"Grand Master of the Road"}')
ON CONFLICT(id) DO NOTHING;
