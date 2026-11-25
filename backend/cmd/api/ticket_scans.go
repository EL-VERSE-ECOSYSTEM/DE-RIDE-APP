package main

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/deride/ecosystem/backend/internal/tickets"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerTicketScanRoutes(v1, secure fiber.Router, db *pgxpool.Pool) {
	v1.Get("/keys/public", func(c *fiber.Ctx) error {
		key := strings.ReplaceAll(os.Getenv("QR_PUBLIC_KEY_PEM"), `\n`, "\n")
		if key == "" || os.Getenv("QR_KEY_ID") == "" {
			return c.Status(503).JSON(errJSON("ERR_QR_KEY_UNAVAILABLE"))
		}
		return c.JSON(fiber.Map{"algorithm": "RS256", "key_id": os.Getenv("QR_KEY_ID"), "public_key_pem": key})
	})
	secure.Post("/tickets/scan", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "DRIVER" && me.Role != "DISPATCHER" && me.Role != "PARK_OPERATOR" && me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var in struct {
			TripID     string          `json:"trip_id"`
			ManualCode string          `json:"manual_code"`
			QR         json.RawMessage `json:"qr_payload"`
			Offline    bool            `json:"offline_sync"`
			ScannedAt  *time.Time      `json:"scanned_at"`
		}
		if c.BodyParser(&in) != nil || in.TripID == "" || (strings.TrimSpace(in.ManualCode) == "" && len(in.QR) == 0) {
			return c.Status(422).JSON(errJSON("ERR_TICKET_SCAN_INVALID"))
		}
		if in.Offline && in.ScannedAt != nil && (in.ScannedAt.After(time.Now().Add(time.Minute)) || in.ScannedAt.Before(time.Now().Add(-24*time.Hour))) {
			return c.Status(422).JSON(errJSON("ERR_SCAN_TIME_INVALID"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var tripAllowed bool
		if err = tx.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM trips t WHERE t.id=$1 AND ($2='ADMIN' OR t.driver_id=$3 OR EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=t.id AND a.user_id=$3) OR EXISTS(SELECT 1 FROM park_operator_assignments a WHERE a.park_id=t.park_id AND a.user_id=$3)))`, in.TripID, me.Role, me.UserID).Scan(&tripAllowed); err != nil || !tripAllowed {
			return c.Status(403).JSON(errJSON("ERR_TRIP_ACCESS_DENIED"))
		}
		var ticketID, passenger, seat, manual, status string
		var stored json.RawMessage
		if len(in.QR) > 0 {
			var envelope tickets.SignedPayload
			if json.Unmarshal(in.QR, &envelope) != nil {
				return c.Status(422).JSON(errJSON("ERR_QR_INVALID"))
			}
			if err = tickets.Verify(envelope, os.Getenv("QR_PUBLIC_KEY_PEM"), os.Getenv("QR_KEY_ID"), time.Now().UTC()); err != nil {
				return c.Status(422).JSON(errJSON("ERR_QR_SIGNATURE_INVALID"))
			}
			ticketID = envelope.Payload.TicketID
			err = tx.QueryRow(c.UserContext(), `SELECT id,passenger_id,seat_code,manual_code,status,qr_payload FROM tickets WHERE id=$1 AND trip_id=$2 FOR UPDATE`, ticketID, in.TripID).Scan(&ticketID, &passenger, &seat, &manual, &status, &stored)
			if err == nil {
				var saved tickets.SignedPayload
				if json.Unmarshal(stored, &saved) != nil || saved.Signature != envelope.Signature || saved.KeyID != envelope.KeyID || saved.Payload != envelope.Payload {
					err = os.ErrInvalid
				}
			}
		} else {
			if !tickets.VerifyManualCode(in.ManualCode) {
				return c.Status(422).JSON(errJSON("ERR_TICKET_CODE_INVALID"))
			}
			err = tx.QueryRow(c.UserContext(), `SELECT id,passenger_id,seat_code,manual_code,status,qr_payload FROM tickets WHERE manual_code=$1 AND trip_id=$2 FOR UPDATE`, strings.ToUpper(strings.TrimSpace(in.ManualCode)), in.TripID).Scan(&ticketID, &passenger, &seat, &manual, &status, &stored)
		}
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_TICKET_NOT_FOUND"))
		}
		if status == "BOARDED" {
			if err = tx.Commit(c.UserContext()); err != nil {
				return c.Status(503).JSON(errJSON("ERR_SCAN_SYNC_FAILED"))
			}
			return c.JSON(fiber.Map{"ticket_id": ticketID, "passenger_id": passenger, "seat_code": seat, "manual_code": manual, "status": "BOARDED", "already_scanned": true})
		}
		if status != "CONFIRMED" && status != "VALID" {
			return c.Status(409).JSON(errJSON("ERR_TICKET_NOT_BOARDABLE"))
		}
		scannedAt := time.Now().UTC()
		if in.Offline && in.ScannedAt != nil {
			scannedAt = in.ScannedAt.UTC()
		}
		tag, err := tx.Exec(c.UserContext(), `UPDATE tickets SET status='BOARDED',scanned_at=$1,scanned_by=$2,scanned_offline=$3 WHERE id=$4 AND scanned_at IS NULL`, scannedAt, me.UserID, in.Offline, ticketID)
		if err != nil || tag.RowsAffected() == 0 {
			return c.Status(409).JSON(errJSON("ERR_TICKET_ALREADY_SCANNED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(503).JSON(errJSON("ERR_SCAN_SYNC_FAILED"))
		}
		return c.JSON(fiber.Map{"ticket_id": ticketID, "passenger_id": passenger, "seat_code": seat, "manual_code": manual, "status": "BOARDED", "scanned_offline": in.Offline, "scanned_at": scannedAt})
	})
}
