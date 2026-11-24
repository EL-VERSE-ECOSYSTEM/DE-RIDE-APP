package main

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerTripRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Get("/my-trips", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "DRIVER" && me.Role != "DISPATCHER" && me.Role != "PARK_OPERATOR" && me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT t.id,p.name,t.departure_at,t.status FROM trips t JOIN bus_parks p ON p.id=t.park_id WHERE ($1='ADMIN' OR t.driver_id=$2 OR ($1='DISPATCHER' AND EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=t.id AND a.user_id=$2)) OR ($1='PARK_OPERATOR' AND EXISTS(SELECT 1 FROM park_operator_assignments a WHERE a.park_id=t.park_id AND a.user_id=$2))) ORDER BY t.departure_at DESC LIMIT 100`, me.Role, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_TRIPS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, name, status string
			var departure time.Time
			if err = rows.Scan(&id, &name, &departure, &status); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"trip_id": id, "park_name": name, "departure_at": departure, "status": status})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"trips": items})
	})
	secure.Get("/trips/:tripID/messages", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var allowed, expired bool
		err := db.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM trips t WHERE t.id=$1 AND (t.driver_id=$2 OR EXISTS(SELECT 1 FROM tickets k WHERE k.trip_id=t.id AND k.passenger_id=$2) OR EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=t.id AND a.user_id=$2) OR EXISTS(SELECT 1 FROM park_operator_assignments a WHERE a.park_id=t.park_id AND a.user_id=$2) OR ($3='ADMIN' AND EXISTS(SELECT 1 FROM escalations e WHERE e.trip_id=t.id AND e.status IN ('OPEN','IN_PROGRESS'))))),EXISTS(SELECT 1 FROM trips t WHERE t.id=$1 AND t.arrived_at IS NOT NULL AND now()>t.arrived_at+interval '6 minutes' AND NOT EXISTS(SELECT 1 FROM escalations e WHERE e.trip_id=t.id))`, c.Params("tripID"), me.UserID, me.Role).Scan(&allowed, &expired)
		if err != nil || !allowed {
			return c.Status(404).JSON(errJSON("ERR_TRIP_ACCESS_DENIED"))
		}
		if expired {
			return c.Status(410).JSON(errJSON("ERR_CHAT_WINDOW_EXPIRED"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT id,sender_id,body,attachment_media_id,created_at FROM messages WHERE trip_id=$1 ORDER BY created_at LIMIT 500`, c.Params("tripID"))
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_CHAT_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, sender, body string
			var attachment *string
			var created time.Time
			if err = rows.Scan(&id, &sender, &body, &attachment, &created); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "sender_id": sender, "content": body, "attachment_media_id": attachment, "created_at": created})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"messages": items})
	})
	secure.Get("/trips", func(c *fiber.Ctx) error {
		state := strings.ToUpper(strings.TrimSpace(c.Query("state")))
		if len(state) > 2 {
			return c.Status(422).JSON(errJSON("ERR_STATE_CODE_INVALID"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT t.id,t.park_id,p.name,t.state_code,t.fare_ngn,t.capacity,t.departure_at,t.status,COALESCE(s.sold,0) FROM trips t JOIN bus_parks p ON p.id=t.park_id LEFT JOIN LATERAL (SELECT count(*) sold FROM tickets k WHERE k.trip_id=t.id AND k.status NOT IN ('CANCELLED','REFUNDED')) s ON true WHERE t.status='OPEN' AND t.departure_at>now() AND ($1='' OR t.state_code=$1) ORDER BY t.departure_at LIMIT 100`, state)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_TRIPS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, parkID, name, stateCode, status string
			var fare int64
			var capacity, sold int
			var departure time.Time
			if err = rows.Scan(&id, &parkID, &name, &stateCode, &fare, &capacity, &departure, &status, &sold); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "park_id": parkID, "park_name": name, "state_code": strings.TrimSpace(stateCode), "fare_ngn": fare, "capacity": capacity, "seats_sold": sold, "seats_available": max(0, capacity-sold), "departure_at": departure, "status": status})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"trips": items})
	})
	secure.Get("/trips/:tripID/seats", func(c *fiber.Ctx) error {
		var capacity int
		if err := db.QueryRow(c.UserContext(), `SELECT capacity FROM trips WHERE id=$1 AND status='OPEN'`, c.Params("tripID")).Scan(&capacity); err != nil {
			return c.Status(404).JSON(errJSON("ERR_TRIP_NOT_FOUND"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT seat_code FROM tickets WHERE trip_id=$1 AND status NOT IN ('CANCELLED','REFUNDED') ORDER BY seat_code`, c.Params("tripID"))
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_SEAT_MAP_UNAVAILABLE"))
		}
		defer rows.Close()
		taken := make([]string, 0)
		for rows.Next() {
			var code string
			if err = rows.Scan(&code); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			taken = append(taken, code)
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"capacity": capacity, "taken_seats": taken})
	})
	secure.Post("/trips", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "DRIVER" && me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var req struct {
			ParkID    string    `json:"park_id"`
			VehicleID string    `json:"vehicle_registration_id"`
			Fare      int64     `json:"fare_ngn"`
			Departure time.Time `json:"departure_at"`
		}
		if err := c.BodyParser(&req); err != nil || req.ParkID == "" || req.VehicleID == "" || req.Fare < 0 || !req.Departure.After(time.Now()) {
			return c.Status(422).JSON(errJSON("ERR_TRIP_CREATE_INVALID"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var capacity int
		var vehicleType string
		if err = tx.QueryRow(c.UserContext(), `SELECT seat_capacity,vehicle_type FROM vehicle_registrations WHERE id=$1 AND verification_status='APPROVED' AND ($2='ADMIN' OR owner_id=$3) FOR SHARE`, req.VehicleID, me.Role, me.UserID).Scan(&capacity, &vehicleType); err != nil {
			return c.Status(403).JSON(errJSON("ERR_APPROVED_BUS_REQUIRED"))
		}
		if vehicleType == "BIKE" || vehicleType == "DISPATCH_BIKE" {
			return c.Status(422).JSON(errJSON("ERR_BUS_VEHICLE_REQUIRED"))
		}
		var state string
		if err = tx.QueryRow(c.UserContext(), `SELECT state_code FROM bus_parks WHERE id=$1`, req.ParkID).Scan(&state); err != nil {
			return c.Status(404).JSON(errJSON("ERR_PARK_NOT_FOUND"))
		}
		var id string
		if err = tx.QueryRow(c.UserContext(), `INSERT INTO trips(park_id,driver_id,state_code,fare_ngn,capacity,departure_at,vehicle_registration_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, req.ParkID, me.UserID, state, req.Fare, capacity, req.Departure, req.VehicleID).Scan(&id); err != nil {
			return c.Status(500).JSON(errJSON("ERR_TRIP_CREATE_FAILED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_TRIP_CREATE_FAILED"))
		}
		return c.Status(201).JSON(fiber.Map{"id": id, "status": "OPEN", "capacity": capacity, "departure_at": req.Departure})
	})
	secure.Get("/my-tickets", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		rows, err := db.Query(c.UserContext(), `SELECT k.id,k.trip_id,k.seat_code,k.manual_code,k.qr_payload,k.total_paid_ngn,k.status,p.name,t.departure_at,pi.authorization_url FROM tickets k JOIN trips t ON t.id=k.trip_id JOIN bus_parks p ON p.id=t.park_id LEFT JOIN payment_intents pi ON pi.purpose_id=k.id AND pi.intent_type='TICKET_CHECKOUT' WHERE k.passenger_id=$1 ORDER BY t.departure_at DESC LIMIT 100`, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_TICKETS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, tripID, seat, manual, status, park string
			var qr any
			var amount int64
			var departure time.Time
			var authorizationURL *string
			if err = rows.Scan(&id, &tripID, &seat, &manual, &qr, &amount, &status, &park, &departure, &authorizationURL); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "trip_id": tripID, "seat_code": seat, "manual_code": manual, "qr_payload": qr, "total_paid_ngn": amount, "status": status, "park_name": park, "departure_at": departure, "authorization_url": authorizationURL})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"tickets": items})
	})
}
