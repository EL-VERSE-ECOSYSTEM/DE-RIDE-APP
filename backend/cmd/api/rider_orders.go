package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerRiderOrderRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Post("/ride-orders", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "PASSENGER" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var in struct {
			Pickup  string `json:"pickup_address"`
			Dropoff string `json:"dropoff_address"`
			Fare    int64  `json:"fare_ngn"`
		}
		if c.BodyParser(&in) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		in.Pickup = strings.TrimSpace(in.Pickup)
		in.Dropoff = strings.TrimSpace(in.Dropoff)
		if len(in.Pickup) < 3 || len(in.Pickup) > 300 || len(in.Dropoff) < 3 || len(in.Dropoff) > 300 || in.Fare < 100 || in.Fare > 500000 {
			return c.Status(422).JSON(errJSON("ERR_RIDE_ORDER_INVALID"))
		}
		return createWalletRiderOrder(c, db, me, "RIDE", in.Pickup, in.Dropoff, "", "", in.Fare)
	})
	secure.Post("/parcel-orders", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "PASSENGER" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var in struct {
			Pickup        string `json:"pickup_address"`
			Dropoff       string `json:"dropoff_address"`
			Description   string `json:"parcel_description"`
			ReceiverPhone string `json:"receiver_phone"`
			Fare          int64  `json:"fare_ngn"`
		}
		if c.BodyParser(&in) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		in.Pickup = strings.TrimSpace(in.Pickup)
		in.Dropoff = strings.TrimSpace(in.Dropoff)
		in.Description = strings.TrimSpace(in.Description)
		in.ReceiverPhone = normalizeNigerianPhone(in.ReceiverPhone)
		if len(in.Pickup) < 3 || len(in.Pickup) > 300 || len(in.Dropoff) < 3 || len(in.Dropoff) > 300 || len(in.Description) < 2 || len(in.Description) > 500 || !validNigerianPhone(in.ReceiverPhone) || in.Fare < 100 || in.Fare > 500000 {
			return c.Status(422).JSON(errJSON("ERR_PARCEL_ORDER_INVALID"))
		}
		otp := fmt.Sprintf("%06d", randomSixDigits())
		sum := sha256.Sum256([]byte(otp))
		return createWalletRiderOrder(c, db, me, "PARCEL", in.Pickup, in.Dropoff, in.Description, in.ReceiverPhone, in.Fare, hex.EncodeToString(sum[:]), otp)
	})
	secure.Get("/rider/jobs", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		rows, err := db.Query(c.UserContext(), `SELECT id,'RIDE'::text,pickup_address,dropoff_address,''::text,fare_ngn,total_paid_ngn,status,created_at FROM ride_orders WHERE (status='OPEN' AND ($1='ADMIN' OR ($1='BIKER' AND EXISTS(SELECT 1 FROM users u WHERE u.id=$2 AND u.biker_approved)))) OR passenger_id=$2 OR biker_id=$2 UNION ALL SELECT id,'PARCEL'::text,pickup_address,dropoff_address,parcel_description,fare_ngn,total_paid_ngn,status,created_at FROM parcel_orders WHERE (status='OPEN' AND ($1='ADMIN' OR ($1='DISPATCH_RIDER' AND EXISTS(SELECT 1 FROM users u WHERE u.id=$2 AND u.dispatch_approved)))) OR sender_id=$2 OR rider_id=$2 ORDER BY created_at DESC LIMIT 100`, me.Role, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_RIDER_JOBS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, kind, pickup, dropoff, description, status string
			var fare, total int64
			var created time.Time
			if rows.Scan(&id, &kind, &pickup, &dropoff, &description, &fare, &total, &status, &created) != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "type": kind, "pickup_address": pickup, "dropoff_address": dropoff, "description": description, "fare_ngn": fare, "total_paid_ngn": total, "status": status, "created_at": created})
		}
		return c.JSON(fiber.Map{"jobs": items})
	})
	secure.Post("/rider/jobs/:kind/:id/accept", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		kind := strings.ToUpper(c.Params("kind"))
		if kind == "RIDE" && me.Role != "BIKER" && me.Role != "ADMIN" || kind == "PARCEL" && me.Role != "DISPATCH_RIDER" && me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		if kind != "RIDE" && kind != "PARCEL" {
			return c.Status(404).JSON(errJSON("ERR_RIDER_JOB_NOT_FOUND"))
		}
		var approved bool
		column := "biker_approved"
		if kind == "PARCEL" {
			column = "dispatch_approved"
		}
		if err := db.QueryRow(c.UserContext(), `SELECT `+column+` FROM users WHERE id=$1`, me.UserID).Scan(&approved); err != nil || !approved && me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_RIDER_KYC_REQUIRED"))
		}
		table, owner := "ride_orders", "biker_id"
		if kind == "PARCEL" {
			table, owner = "parcel_orders", "rider_id"
		}
		sql := `UPDATE ` + table + ` SET ` + owner + `=$1,status='ACCEPTED',updated_at=now() WHERE id=$2 AND status='OPEN' RETURNING id`
		var id string
		if err := db.QueryRow(c.UserContext(), sql, me.UserID, c.Params("id")).Scan(&id); err != nil {
			return c.Status(409).JSON(errJSON("ERR_RIDER_JOB_UNAVAILABLE"))
		}
		_, _ = db.Exec(c.UserContext(), `INSERT INTO app_notifications(user_id,category,title,body,action_url) SELECT passenger_id,'TRIP','Rider accepted your request','Your rider is on the way.','/notifications' FROM ride_orders WHERE id=$1 AND $2='RIDE' UNION ALL SELECT sender_id,'TRIP','Dispatch rider accepted your parcel','Your delivery is being prepared.','/notifications' FROM parcel_orders WHERE id=$1 AND $2='PARCEL'`, id, kind)
		return c.JSON(fiber.Map{"id": id, "status": "ACCEPTED"})
	})
	secure.Post("/rider/jobs/:kind/:id/start", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		table, owner, kind, err := riderJobTable(c.Params("kind"))
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_RIDER_JOB_NOT_FOUND"))
		}
		tag, err := db.Exec(c.UserContext(), `UPDATE `+table+` SET status='IN_PROGRESS',updated_at=now() WHERE id=$1 AND `+owner+`=$2 AND status='ACCEPTED'`, c.Params("id"), me.UserID)
		if err != nil || tag.RowsAffected() == 0 {
			return c.Status(409).JSON(errJSON("ERR_RIDER_JOB_TRANSITION_INVALID"))
		}
		return c.JSON(fiber.Map{"id": c.Params("id"), "type": kind, "status": "IN_PROGRESS"})
	})
	secure.Post("/rider/jobs/:kind/:id/complete", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		table, owner, kind, err := riderJobTable(c.Params("kind"))
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_RIDER_JOB_NOT_FOUND"))
		}
		if kind == "PARCEL" {
			var in struct {
				OTP string `json:"receiver_otp"`
			}
			if c.BodyParser(&in) != nil || len(in.OTP) != 6 {
				return c.Status(422).JSON(errJSON("ERR_DELIVERY_OTP_REQUIRED"))
			}
			var stored string
			if err = db.QueryRow(c.UserContext(), `SELECT receiver_otp_hash FROM parcel_orders WHERE id=$1 AND rider_id=$2 AND status='IN_PROGRESS'`, c.Params("id"), me.UserID).Scan(&stored); err != nil {
				return c.Status(404).JSON(errJSON("ERR_RIDER_JOB_NOT_FOUND"))
			}
			sum := sha256.Sum256([]byte(in.OTP))
			if !constantTimeHexEqual(stored, hex.EncodeToString(sum[:])) {
				return c.Status(403).JSON(errJSON("ERR_DELIVERY_OTP_INVALID"))
			}
		}
		tag, err := db.Exec(c.UserContext(), `UPDATE `+table+` SET status='COMPLETED',completed_at=now(),updated_at=now() WHERE id=$1 AND `+owner+`=$2 AND status='IN_PROGRESS'`, c.Params("id"), me.UserID)
		if err != nil || tag.RowsAffected() == 0 {
			return c.Status(409).JSON(errJSON("ERR_RIDER_JOB_TRANSITION_INVALID"))
		}
		award, rankErr := awardCompletedRiderTrip(c.UserContext(), db, me.UserID)
		if rankErr != nil {
			return c.Status(202).JSON(fiber.Map{"id": c.Params("id"), "type": kind, "status": "COMPLETED", "rank_update": "PENDING"})
		}
		return c.JSON(fiber.Map{"id": c.Params("id"), "type": kind, "status": "COMPLETED", "rank": award})
	})
}

func awardCompletedRiderTrip(ctx context.Context, db *pgxpool.Pool, userID string) (fiber.Map, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var role, current string
	var count int
	if err = tx.QueryRow(ctx, `UPDATE users SET completed_trips=completed_trips+1 WHERE id=$1 RETURNING active_role,current_rank,completed_trips`, userID).Scan(&role, &current, &count); err != nil {
		return nil, err
	}
	var rankID string
	var threshold int
	var prize int64
	err = tx.QueryRow(ctx, `SELECT id,trips_required,prize_ngn FROM ranks WHERE role=$1 AND trips_required<=$2 ORDER BY trips_required DESC LIMIT 1`, role, count).Scan(&rankID, &threshold, &prize)
	if err == pgx.ErrNoRows {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return fiber.Map{"title": current, "completed_trips": count, "prize_ngn": 0}, nil
	}
	if err != nil {
		return nil, err
	}
	title, medal := rankPresentation(role, threshold)
	if _, err = tx.Exec(ctx, `UPDATE users SET current_rank=$2,current_medal=$3 WHERE id=$1`, userID, title, medal); err != nil {
		return nil, err
	}
	paid := false
	if prize > 0 {
		key := "rank-prize:" + userID + ":" + rankID
		var ledgerID string
		err = tx.QueryRow(ctx, `INSERT INTO ledger_entries(user_id,idempotency_key,amount_ngn,entry_type) VALUES($1,$2,$3,'RANK_CASH_PRIZE') ON CONFLICT(idempotency_key) DO NOTHING RETURNING id`, userID, key, prize).Scan(&ledgerID)
		if err == nil {
			if _, err = tx.Exec(ctx, `INSERT INTO wallet_accounts(user_id,balance_ngn) VALUES($1,0) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE wallet_accounts SET balance_ngn=balance_ngn+$1 WHERE user_id=$2`, prize, userID); err != nil {
				return nil, err
			}
			if err = storeSignedReceipt(ctx, tx, userID, "RANK_CASH_PRIZE", ledgerID, map[string]any{"rank_id": rankID, "rank": title, "prize_ngn": prize, "completed_trips": count}); err != nil {
				return nil, err
			}
			_, _ = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'WALLET','Rank reward unlocked',$2,'/transactions',jsonb_build_object('rank',$3,'amount_ngn',$4))`, userID, fmt.Sprintf("You reached %s and earned ₦%d.", title, prize), title, prize)
			paid = true
		} else if err != pgx.ErrNoRows {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return fiber.Map{"title": title, "medal": medal, "completed_trips": count, "prize_ngn": prize, "prize_credited": paid}, nil
}

func rankPresentation(role string, trips int) (string, string) {
	if role == "PASSENGER" {
		switch {
		case trips >= 150:
			return "DE RIDE Legend", "Diamond Crown"
		case trips >= 75:
			return "Interstate Marshal", "Platinum Shield"
		case trips >= 30:
			return "Express Voyager", "Gold Winged Wheel"
		case trips >= 10:
			return "Highway Cruiser", "Silver Highway"
		default:
			return "Street Scout", "Bronze Compass"
		}
	}
	switch {
	case trips >= 500:
		return "Grand Master of the Road", "Diamond Emblem"
	case trips >= 200:
		return "Transit Titan", "Platinum Crest"
	case trips >= 80:
		return "Highway Commander", "Gold Turbo"
	case trips >= 25:
		return "Route Captain", "Silver Piston"
	default:
		return "Verified Pilot", "Bronze Steering"
	}
}

func createWalletRiderOrder(c *fiber.Ctx, db *pgxpool.Pool, me identity, kind, pickup, dropoff, description, receiver string, fare int64, extra ...string) error {
	key := strings.TrimSpace(c.Get("Idempotency-Key"))
	if len(key) < 12 || len(key) > 120 {
		return c.Status(400).JSON(errJSON("ERR_IDEMPOTENCY_KEY_REQUIRED"))
	}
	fee := int64(20)
	total := fare + fee
	tx, err := db.Begin(c.UserContext())
	if err != nil {
		return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
	}
	defer tx.Rollback(c.UserContext())
	payloadDigest := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%s|%d", kind, pickup, dropoff, description, receiver, fare)))
	payloadHash := hex.EncodeToString(payloadDigest[:])
	var existingID string
	var existingType, existingHash string
	err = tx.QueryRow(c.UserContext(), `SELECT order_id::text,order_type,payload_hash FROM rider_order_idempotency WHERE user_id=$1 AND idempotency_key=$2`, me.UserID, key).Scan(&existingID, &existingType, &existingHash)
	if err == nil {
		if existingType != kind || existingHash != payloadHash {
			return c.Status(409).JSON(errJSON("ERR_IDEMPOTENCY_KEY_PAYLOAD_MISMATCH"))
		}
		return c.JSON(fiber.Map{"id": existingID, "status": "OPEN", "total_paid_ngn": total})
	}
	if err != pgx.ErrNoRows {
		return c.Status(503).JSON(errJSON("ERR_RIDER_ORDER_LOOKUP_FAILED"))
	}
	var balance int64
	if err = tx.QueryRow(c.UserContext(), `SELECT balance_ngn FROM wallet_accounts WHERE user_id=$1 FOR UPDATE`, me.UserID).Scan(&balance); err != nil {
		return c.Status(404).JSON(errJSON("ERR_WALLET_NOT_FOUND"))
	}
	if balance < total {
		return c.Status(402).JSON(fiber.Map{"error": "ERR_WALLET_BALANCE_INSUFFICIENT", "required_ngn": total, "balance_ngn": balance})
	}
	id, err := newUUID()
	if err != nil {
		return c.Status(500).JSON(errJSON("ERR_RIDER_ORDER_CREATE_FAILED"))
	}
	ref := "RDO-" + strings.ReplaceAll(id, "-", "")
	if kind == "RIDE" {
		_, err = tx.Exec(c.UserContext(), `INSERT INTO ride_orders(id,passenger_id,status,fare_ngn,pickup_address,dropoff_address,platform_fee_ngn,total_paid_ngn,payment_status,payment_reference) VALUES($1,$2,'OPEN',$3,$4,$5,$6,$7,'PAID',$8)`, id, me.UserID, fare, pickup, dropoff, fee, total, ref)
	} else {
		otpHash, otp := extra[0], extra[1]
		_, err = tx.Exec(c.UserContext(), `INSERT INTO parcel_orders(id,sender_id,receiver_phone,receiver_otp_hash,status,fare_ngn,pickup_address,dropoff_address,parcel_description,platform_fee_ngn,total_paid_ngn,payment_status,payment_reference) VALUES($1,$2,$3,$4,'OPEN',$5,$6,$7,$8,$9,$10,'PAID',$11)`, id, me.UserID, receiver, otpHash, fare, pickup, dropoff, description, fee, total, ref)
		_ = otp
	}
	if err != nil {
		return c.Status(500).JSON(errJSON("ERR_RIDER_ORDER_CREATE_FAILED"))
	}
	if _, err = tx.Exec(c.UserContext(), `UPDATE wallet_accounts SET balance_ngn=balance_ngn-$1 WHERE user_id=$2`, total, me.UserID); err != nil {
		return c.Status(500).JSON(errJSON("ERR_WALLET_DEBIT_FAILED"))
	}
	ledger, err := insertLedger(c.UserContext(), tx, me.UserID, "rider-order:"+key, -total, "RIDER_ORDER")
	if err != nil {
		return c.Status(409).JSON(errJSON("ERR_LEDGER_WRITE_FAILED"))
	}
	if err = storeSignedReceipt(c.UserContext(), tx, me.UserID, "RIDER_ORDER", ledger, map[string]any{"order_id": id, "reference": ref, "fare_ngn": fare, "platform_fee_ngn": fee, "total_ngn": total, "type": kind}); err != nil {
		return c.Status(503).JSON(errJSON("ERR_RECEIPT_SIGNING_UNAVAILABLE"))
	}
	_, err = tx.Exec(c.UserContext(), `INSERT INTO rider_order_idempotency(user_id,idempotency_key,order_type,order_id,payload_hash) VALUES($1,$2,$3,$4,$5)`, me.UserID, key, kind, id, payloadHash)
	if err != nil {
		return c.Status(409).JSON(errJSON("ERR_RIDER_ORDER_IDEMPOTENCY_CONFLICT"))
	}
	if err = tx.Commit(c.UserContext()); err != nil {
		return c.Status(500).JSON(errJSON("ERR_RIDER_ORDER_CREATE_FAILED"))
	}
	if kind == "PARCEL" {
		return c.Status(201).JSON(fiber.Map{"id": id, "status": "OPEN", "total_paid_ngn": total, "receiver_otp": extra[1]})
	}
	return c.Status(201).JSON(fiber.Map{"id": id, "status": "OPEN", "total_paid_ngn": total})
}
func riderJobTable(value string) (table, owner, kind string, err error) {
	switch strings.ToUpper(value) {
	case "RIDE":
		return "ride_orders", "biker_id", "RIDE", nil
	case "PARCEL":
		return "parcel_orders", "rider_id", "PARCEL", nil
	default:
		return "", "", "", fmt.Errorf("unknown rider job")
	}
}
func randomSixDigits() int {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return int(time.Now().UnixNano() % 1000000)
	}
	return int(uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3])) % 1000000
}
func constantTimeHexEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
