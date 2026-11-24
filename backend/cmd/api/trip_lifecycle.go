package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerTripLifecycleRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Post("/trips/:tripID/depart", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var status string
		var driverID string
		var parkID string
		if err = tx.QueryRow(c.UserContext(), `SELECT status,driver_id,park_id FROM trips WHERE id=$1 FOR UPDATE`, c.Params("tripID")).Scan(&status, &driverID, &parkID); err != nil {
			return c.Status(404).JSON(errJSON("ERR_TRIP_NOT_FOUND"))
		}
		if !tripOperatorAllowed(c.UserContext(), tx, me, driverID, parkID, c.Params("tripID")) {
			return c.Status(403).JSON(errJSON("ERR_TRIP_ACCESS_DENIED"))
		}
		if status != "OPEN" && status != "BOARDING" {
			return c.Status(409).JSON(errJSON("ERR_TRIP_LIFECYCLE_INVALID"))
		}
		var covered bool
		planID := "pro_5k"
		if me.Role == "PARK_OPERATOR" || me.Role == "DISPATCHER" {
			planID = "terminal_15k"
		}
		_ = tx.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE user_id=$1 AND plan_id=$2 AND status='ACTIVE' AND (ends_at IS NULL OR ends_at>now()))`, me.UserID, planID).Scan(&covered)
		var fee int64
		if !covered {
			fee = 250
			var balance int64
			if err = tx.QueryRow(c.UserContext(), `SELECT balance_ngn FROM wallet_accounts WHERE user_id=$1 FOR UPDATE`, me.UserID).Scan(&balance); err != nil || balance < fee {
				return c.Status(402).JSON(fiber.Map{"error": "ERR_WALLET_BALANCE_INSUFFICIENT", "required_ngn": fee})
			}
			if _, err = tx.Exec(c.UserContext(), `UPDATE wallet_accounts SET balance_ngn=balance_ngn-$1 WHERE user_id=$2`, fee, me.UserID); err != nil {
				return c.Status(500).JSON(errJSON("ERR_WALLET_DEBIT_FAILED"))
			}
			ledger, le := insertLedger(c.UserContext(), tx, me.UserID, "bus-dispatch:"+c.Params("tripID"), -fee, "PER_BUS_FEE")
			if le != nil {
				return c.Status(409).JSON(errJSON("ERR_TRIP_FEE_ALREADY_CHARGED"))
			}
			if le = storeSignedReceipt(c.UserContext(), tx, me.UserID, "PER_BUS_FEE", ledger, map[string]any{"trip_id": c.Params("tripID"), "amount_ngn": fee, "type": "PER_BUS_FEE", "issued_at": time.Now().UTC().Format(time.RFC3339)}); le != nil {
				return c.Status(503).JSON(errJSON("ERR_RECEIPT_SIGNING_UNAVAILABLE"))
			}
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE trips SET status='DEPARTED',departed_at=now() WHERE id=$1`, c.Params("tripID")); err != nil {
			return c.Status(500).JSON(errJSON("ERR_TRIP_UPDATE_FAILED"))
		}
		_, _ = tx.Exec(c.UserContext(), `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) SELECT DISTINCT passenger_id,'TRIP','Your bus has departed','Your trip is now in progress.','/tickets',jsonb_build_object('trip_id',$1) FROM tickets WHERE trip_id=$1 AND status='CONFIRMED'`, c.Params("tripID"))
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_TRIP_UPDATE_FAILED"))
		}
		return c.JSON(fiber.Map{"trip_id": c.Params("tripID"), "status": "DEPARTED", "fee_ngn": fee})
	})
	secure.Post("/trips/:tripID/arrive", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var owner, parkID string
		if err := db.QueryRow(c.UserContext(), `SELECT driver_id,park_id FROM trips WHERE id=$1`, c.Params("tripID")).Scan(&owner, &parkID); err != nil {
			return c.Status(404).JSON(errJSON("ERR_TRIP_NOT_FOUND"))
		}
		if !tripOperatorAllowed(c.UserContext(), db, me, owner, parkID, c.Params("tripID")) {
			return c.Status(403).JSON(errJSON("ERR_TRIP_ACCESS_DENIED"))
		}
		tag, err := db.Exec(c.UserContext(), `UPDATE trips SET status='ARRIVED',arrived_at=now() WHERE id=$1 AND status IN ('DEPARTED','IN_TRANSIT')`, c.Params("tripID"))
		if err != nil || tag.RowsAffected() == 0 {
			return c.Status(409).JSON(errJSON("ERR_TRIP_LIFECYCLE_INVALID"))
		}
		_, _ = db.Exec(c.UserContext(), `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) SELECT DISTINCT passenger_id,'TRIP','You have arrived','Your bus trip is complete.','/tickets',jsonb_build_object('trip_id',$1) FROM tickets WHERE trip_id=$1 AND status IN ('CONFIRMED','BOARDED')`, c.Params("tripID"))
		return c.JSON(fiber.Map{"trip_id": c.Params("tripID"), "status": "ARRIVED"})
	})
	secure.Post("/trips/:tripID/cancel", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var in struct {
			Reason string `json:"reason"`
		}
		if c.BodyParser(&in) != nil || len(strings.TrimSpace(in.Reason)) < 4 || len(in.Reason) > 500 {
			return c.Status(422).JSON(errJSON("ERR_CANCELLATION_REASON_REQUIRED"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var status, driverID, parkID string
		if err = tx.QueryRow(c.UserContext(), `SELECT status,driver_id,park_id FROM trips WHERE id=$1 FOR UPDATE`, c.Params("tripID")).Scan(&status, &driverID, &parkID); err != nil {
			return c.Status(404).JSON(errJSON("ERR_TRIP_NOT_FOUND"))
		}
		if !tripOperatorAllowed(c.UserContext(), tx, me, driverID, parkID, c.Params("tripID")) {
			return c.Status(403).JSON(errJSON("ERR_TRIP_ACCESS_DENIED"))
		}
		if status == "DEPARTED" || status == "IN_TRANSIT" || status == "ARRIVED" || status == "CANCELLED" {
			return c.Status(409).JSON(errJSON("ERR_TRIP_CANNOT_BE_CANCELLED"))
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE trips SET status='CANCELLED',cancellation_reason=$2,arrived_at=now() WHERE id=$1`, c.Params("tripID"), strings.TrimSpace(in.Reason)); err != nil {
			return c.Status(500).JSON(errJSON("ERR_TRIP_UPDATE_FAILED"))
		}
		rows, err := tx.Query(c.UserContext(), `SELECT id,passenger_id,base_fare_ngn,platform_fee_ngn FROM tickets WHERE trip_id=$1 AND status IN ('CONFIRMED','VALID','BOARDED') AND refund_status='NONE' FOR UPDATE`, c.Params("tripID"))
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_REFUND_QUEUE_UNAVAILABLE"))
		}
		type item struct {
			id, user  string
			fare, fee int64
		}
		items := []item{}
		for rows.Next() {
			var x item
			if rows.Scan(&x.id, &x.user, &x.fare, &x.fee) == nil {
				items = append(items, x)
			}
		}
		rows.Close()
		for _, x := range items {
			credit := x.fare + x.fee
			key := "operator-cancel:" + x.id
			if _, err = tx.Exec(c.UserContext(), `INSERT INTO refunds(ticket_id,percent,amount_ngn,destination,idempotency_key) VALUES($1,100,$2,'WALLET',$3)`, x.id, credit, key); err != nil {
				return c.Status(500).JSON(errJSON("ERR_OPERATOR_REFUND_FAILED"))
			}
			if _, err = tx.Exec(c.UserContext(), `INSERT INTO wallet_accounts(user_id,balance_ngn) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET balance_ngn=wallet_accounts.balance_ngn+EXCLUDED.balance_ngn`, x.user, credit); err != nil {
				return c.Status(500).JSON(errJSON("ERR_REFUND_CREDIT_FAILED"))
			}
			ledger, le := insertLedger(c.UserContext(), tx, x.user, key, credit, "REFUND")
			if le != nil {
				return c.Status(500).JSON(errJSON("ERR_LEDGER_WRITE_FAILED"))
			}
			if le = storeSignedReceipt(c.UserContext(), tx, x.user, "REFUND", ledger, map[string]any{"trip_id": c.Params("tripID"), "ticket_id": x.id, "amount_ngn": credit, "type": "OPERATOR_CANCELLATION_REFUND", "issued_at": time.Now().UTC().Format(time.RFC3339)}); le != nil {
				return c.Status(503).JSON(errJSON("ERR_RECEIPT_SIGNING_UNAVAILABLE"))
			}
			_, _ = tx.Exec(c.UserContext(), `UPDATE tickets SET status='CANCELLED',refund_status='REFUNDED_WALLET' WHERE id=$1`, x.id)
			_, _ = tx.Exec(c.UserContext(), `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'PAYMENT','Trip cancelled and refunded',$2,'/transactions',jsonb_build_object('ticket_id',$3,'amount_ngn',$4))`, x.user, fmt.Sprintf("₦%d was credited to your wallet for the cancelled bus trip.", credit), x.id, credit)
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_TRIP_CANCELLATION_FAILED"))
		}
		return c.JSON(fiber.Map{"trip_id": c.Params("tripID"), "status": "CANCELLED", "tickets_refunded": len(items)})
	})
}

type tripQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func tripOperatorAllowed(ctx context.Context, tx tripQueryRower, me identity, driverID, parkID, tripID string) bool {
	if me.Role == "ADMIN" || driverID == me.UserID {
		return true
	}
	var ok bool
	if me.Role == "DISPATCHER" {
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trip_operator_assignments WHERE trip_id=$1 AND user_id=$2)`, tripID, me.UserID).Scan(&ok)
	} else if me.Role == "PARK_OPERATOR" {
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM park_operator_assignments WHERE park_id=$1 AND user_id=$2)`, parkID, me.UserID).Scan(&ok)
	}
	return ok
}
