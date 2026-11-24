package main

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerGPSTelemetryRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Get("/admin/live-trips", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT t.id,t.status,t.gps_source,t.gps_updated_at,ST_Y(t.current_location::geometry),ST_X(t.current_location::geometry),p.name,t.departure_at FROM trips t JOIN bus_parks p ON p.id=t.park_id WHERE t.status IN ('OPEN','BOARDING','DEPARTED','IN_TRANSIT') ORDER BY t.gps_updated_at DESC NULLS LAST LIMIT 500`)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_LIVE_TRIPS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, status, source, park string
			var updated, departure *time.Time
			var lat, lng *float64
			if rows.Scan(&id, &status, &source, &updated, &lat, &lng, &park, &departure) != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"trip_id": id, "status": status, "gps_source": source, "gps_updated_at": updated, "latitude": lat, "longitude": lng, "park_name": park, "departure_at": departure})
		}
		return c.JSON(fiber.Map{"trips": items})
	})
}

func storeUserTelemetry(ctx context.Context, db *pgxpool.Pool, user identity, tripID string, lat, lng, speed float64, source string) error {
	source = strings.ToUpper(strings.TrimSpace(source))
	if source != "DRIVER_APP" && source != "DISPATCHER_APP" {
		return errGPSSource
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 || speed < 0 || speed > 180 || math.IsNaN(lat) || math.IsNaN(lng) || math.IsInf(lat, 0) || math.IsInf(lng, 0) {
		return errGPSCoordinates
	}
	var allowed bool
	var optedIn bool
	err := db.QueryRow(ctx, `SELECT u.gps_opt_in,EXISTS(SELECT 1 FROM trips t WHERE t.id=$1 AND ((t.driver_id=$2 AND $3='DRIVER_APP') OR ($3='DISPATCHER_APP' AND EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=t.id AND a.user_id=$2) AND t.status IN ('OPEN','BOARDING')))) FROM users u WHERE u.id=$2`, tripID, user.UserID, source).Scan(&optedIn, &allowed)
	if err != nil || !allowed {
		return errGPSAccess
	}
	if !optedIn {
		return errGPSOptIn
	}
	if source == "DISPATCHER_APP" {
		var within bool
		if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trips t JOIN bus_parks p ON p.id=t.park_id WHERE t.id=$1 AND p.boundary IS NOT NULL AND ST_Covers(p.boundary,ST_SetSRID(ST_MakePoint($3,$2),4326)::geography) AND ST_Distance(p.location,ST_SetSRID(ST_MakePoint($3,$2),4326)::geography)<=2000)`, tripID, lat, lng).Scan(&within); err != nil || !within {
			return errGPSGeofence
		}
	}
	_, err = db.Exec(ctx, `UPDATE trips SET current_location=ST_SetSRID(ST_MakePoint($1,$2),4326)::geography,gps_source=$3,gps_updated_at=now() WHERE id=$4 AND CASE gps_source WHEN 'HARDWARE' THEN 3 WHEN 'DRIVER_APP' THEN 2 WHEN 'DISPATCHER_APP' THEN 1 ELSE 0 END <= CASE $3 WHEN 'HARDWARE' THEN 3 WHEN 'DRIVER_APP' THEN 2 WHEN 'DISPATCHER_APP' THEN 1 ELSE 0 END`, lng, lat, source, tripID)
	return err
}

type gpsError string

func (e gpsError) Error() string { return string(e) }

const (
	errGPSSource      gpsError = "ERR_GPS_SOURCE_INVALID"
	errGPSCoordinates gpsError = "ERR_GPS_COORDINATES_INVALID"
	errGPSAccess      gpsError = "ERR_GPS_TRIP_ACCESS_DENIED"
	errGPSOptIn       gpsError = "ERR_GPS_OPT_IN_REQUIRED"
	errGPSGeofence    gpsError = "ERR_GPS_GEOFENCE_REQUIRED"
)
