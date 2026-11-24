package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ameboPack struct {
	Tokens int
	Price  int64
}

var ameboPacks = map[string]ameboPack{"PACK_23": {23, 4200}, "PACK_50": {50, 6700}, "PACK_75": {75, 8000}, "PACK_100": {100, 10200}}

func registerAmeboRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Post("/amebo/tokens/buy", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var in struct {
			PackID        string `json:"pack_id"`
			PaymentMethod string `json:"payment_method"`
		}
		if c.BodyParser(&in) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		in.PackID = strings.ToUpper(strings.TrimSpace(in.PackID))
		in.PaymentMethod = strings.ToUpper(strings.TrimSpace(in.PaymentMethod))
		p, ok := ameboPacks[in.PackID]
		if in.PaymentMethod == "" { in.PaymentMethod = "WALLET" }
		if !ok || (in.PaymentMethod != "WALLET" && in.PaymentMethod != "PAYSTACK") {
			return c.Status(422).JSON(errJSON("ERR_AMEBO_PACK_INVALID"))
		}
		key := strings.TrimSpace(c.Get("Idempotency-Key"))
		if len(key) < 12 || len(key) > 120 {
			return c.Status(400).JSON(errJSON("ERR_IDEMPOTENCY_KEY_REQUIRED"))
		}
		if in.PaymentMethod == "PAYSTACK" {
			if os.Getenv("PAYSTACK_SECRET_KEY") == "" || !receiptSigningConfigured() { return c.Status(503).JSON(errJSON("ERR_PAYMENT_SERVICE_NOT_CONFIGURED")) }
			var email string
			if err := db.QueryRow(c.UserContext(), `SELECT email FROM users WHERE id=$1 AND email_verified=true`, me.UserID).Scan(&email); err != nil { return c.Status(403).JSON(errJSON("ERR_EMAIL_VERIFICATION_REQUIRED")) }
			id,reference,authURL,accessCode,err := createPurposePaystackIntent(c.UserContext(),db,me.UserID,email,"AMEBO_TOKENS",key,p.Price*100,map[string]any{"pack_id":in.PackID})
			if err != nil { return c.Status(409).JSON(errJSON("ERR_PAYSTACK_INITIALIZE_FAILED")) }
			return c.Status(201).JSON(fiber.Map{"payment_intent_id":id,"reference":reference,"authorization_url":authURL,"access_code":accessCode,"status":"PENDING","pack_id":in.PackID,"tokens":p.Tokens,"price_ngn":p.Price})
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var existing int
		err = tx.QueryRow(c.UserContext(), `SELECT purchased_tokens FROM amebo_wallets WHERE user_id=$1 FOR UPDATE`, me.UserID).Scan(&existing)
		if err != nil && err != pgx.ErrNoRows {
			return c.Status(503).JSON(errJSON("ERR_AMEBO_WALLET_UNAVAILABLE"))
		}
		if err == pgx.ErrNoRows {
			grant := ameboDailyGrant(c.UserContext(), db, me.UserID)
			_, err = tx.Exec(c.UserContext(), `INSERT INTO amebo_wallets(user_id,free_tokens,free_expires_at,purchased_tokens) VALUES($1,$2,(date_trunc('day',now() AT TIME ZONE 'Africa/Lagos')+interval '1 day') AT TIME ZONE 'Africa/Lagos',0)`, me.UserID, grant)
			if err != nil {
				return c.Status(503).JSON(errJSON("ERR_AMEBO_WALLET_UNAVAILABLE"))
			}
		}
		var prior int
		err = tx.QueryRow(c.UserContext(), `SELECT count(*) FROM ledger_entries WHERE user_id=$1 AND idempotency_key=$2`, me.UserID, "amebo-pack:"+key).Scan(&prior)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_LEDGER_LOOKUP_FAILED"))
		}
		if prior > 0 {
			_ = tx.Commit(c.UserContext())
			return c.JSON(fiber.Map{"status": "SUCCEEDED", "pack_id": in.PackID, "tokens": p.Tokens, "price_ngn": p.Price})
		}
		var balance int64
		if err = tx.QueryRow(c.UserContext(), `SELECT balance_ngn FROM wallet_accounts WHERE user_id=$1 FOR UPDATE`, me.UserID).Scan(&balance); err != nil {
			return c.Status(404).JSON(errJSON("ERR_WALLET_NOT_FOUND"))
		}
		if balance < p.Price {
			return c.Status(402).JSON(fiber.Map{"error": "ERR_WALLET_BALANCE_INSUFFICIENT", "required_ngn": p.Price, "balance_ngn": balance})
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE wallet_accounts SET balance_ngn=balance_ngn-$1 WHERE user_id=$2`, p.Price, me.UserID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_WALLET_DEBIT_FAILED"))
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE amebo_wallets SET purchased_tokens=purchased_tokens+$1 WHERE user_id=$2`, p.Tokens, me.UserID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_AMEBO_TOKEN_CREDIT_FAILED"))
		}
		ledgerID, ledgerErr := insertLedger(c.UserContext(), tx, me.UserID, "amebo-pack:"+key, -p.Price, "AMEBO_TOKENS")
		if ledgerErr != nil {
			return c.Status(409).JSON(errJSON("ERR_LEDGER_WRITE_FAILED"))
		}
		if err = storeSignedReceipt(c.UserContext(), tx, me.UserID, "AMEBO_TOKENS", ledgerID, map[string]any{"pack_id": in.PackID, "tokens": p.Tokens, "amount_ngn": p.Price, "type": "AMEBO_TOKENS", "issued_at": time.Now().UTC().Format(time.RFC3339)}); err != nil {
			return c.Status(503).JSON(errJSON("ERR_RECEIPT_SIGNING_UNAVAILABLE"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_AMEBO_PURCHASE_FAILED"))
		}
		return c.Status(201).JSON(fiber.Map{"status": "SUCCEEDED", "pack_id": in.PackID, "tokens": p.Tokens, "price_ngn": p.Price})
	})
	secure.Post("/amebo/chat", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var in struct {
			Message  string `json:"message"`
			Mode     string `json:"mode"`
			Language string `json:"language"`
		}
		if c.BodyParser(&in) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		in.Message = strings.TrimSpace(in.Message)
		in.Mode = strings.ToUpper(strings.TrimSpace(in.Mode))
		in.Language = strings.ToUpper(strings.TrimSpace(in.Language))
		if in.Mode == "" {
			in.Mode = "TEXT"
		}
		if in.Language == "" {
			in.Language = "PCM"
		}
		if in.Message == "" || len(in.Message) > 4000 || in.Mode != "TEXT" || !map[string]bool{"EN": true, "YO": true, "HA": true, "IG": true, "PCM": true}[in.Language] {
			return c.Status(422).JSON(errJSON("ERR_AMEBO_REQUEST_INVALID"))
		}
		ctx := c.UserContext()
		tx, err := db.Begin(ctx)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(ctx)
		grant := ameboDailyGrant(ctx, db, me.UserID)
		_, err = tx.Exec(ctx, `INSERT INTO amebo_wallets(user_id,free_tokens,free_expires_at,purchased_tokens) VALUES($1,$2,(date_trunc('day',now() AT TIME ZONE 'Africa/Lagos')+interval '1 day') AT TIME ZONE 'Africa/Lagos',0) ON CONFLICT(user_id) DO NOTHING`, me.UserID, grant)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_AMEBO_WALLET_UNAVAILABLE"))
		}
		var free, purchased int
		var expires time.Time
		if err = tx.QueryRow(ctx, `SELECT free_tokens,purchased_tokens,free_expires_at FROM amebo_wallets WHERE user_id=$1 FOR UPDATE`, me.UserID).Scan(&free, &purchased, &expires); err != nil {
			return c.Status(503).JSON(errJSON("ERR_AMEBO_WALLET_UNAVAILABLE"))
		}
		if !expires.After(time.Now()) {
			free = grant
			expires = time.Now().Add(24 * time.Hour)
			if _, err = tx.Exec(ctx, `UPDATE amebo_wallets SET free_tokens=$2,free_expires_at=(date_trunc('day',now() AT TIME ZONE 'Africa/Lagos')+interval '1 day') AT TIME ZONE 'Africa/Lagos' WHERE user_id=$1`, me.UserID, grant); err != nil {
				return c.Status(503).JSON(errJSON("ERR_AMEBO_WALLET_UNAVAILABLE"))
			}
		}
		if free <= 0 && purchased <= 0 {
			return c.Status(402).JSON(errJSON("ERR_AMEBO_INSUFFICIENT_TOKENS"))
		}
		if free > 0 {
			_, err = tx.Exec(ctx, `UPDATE amebo_wallets SET free_tokens=free_tokens-1 WHERE user_id=$1`, me.UserID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE amebo_wallets SET purchased_tokens=purchased_tokens-1 WHERE user_id=$1`, me.UserID)
		}
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_AMEBO_TOKEN_DEBIT_FAILED"))
		}
		if err = tx.Commit(ctx); err != nil {
			return c.Status(503).JSON(errJSON("ERR_AMEBO_TOKEN_DEBIT_FAILED"))
		}
		persona := map[string]string{"PASSENGER": "passenger", "DRIVER": "driver", "BIKER": "biker", "DISPATCH_RIDER": "dispatch_rider", "PARK_OPERATOR": "operator", "DISPATCHER": "operator"}[me.Role]
		if persona == "" {
			persona = "passenger"
		}
		baseURL := os.Getenv("AMEBO_BASE_URL")
		if baseURL == "" {
			baseURL = os.Getenv("AI_SERVICE_URL")
		}
		base := strings.TrimRight(baseURL, "/")
		secret := os.Getenv("INTERNAL_AI_SHARED_SECRET")
		if base == "" || secret == "" {
			restoreAmeboToken(db, me.UserID, free > 0)
			return c.Status(503).JSON(errJSON("ERR_AMEBO_PROVIDER_UNAVAILABLE"))
		}
		body, _ := json.Marshal(map[string]string{"message": in.Message, "language": in.Language, "persona": persona})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/ai/v1/chat", bytes.NewReader(body))
		if err != nil {
			restoreAmeboToken(db, me.UserID, free > 0)
			return c.Status(503).JSON(errJSON("ERR_AMEBO_PROVIDER_UNAVAILABLE"))
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Secret", secret)
		resp, err := (&http.Client{Timeout: 35 * time.Second}).Do(req)
		if err != nil {
			restoreAmeboToken(db, me.UserID, free > 0)
			return c.Status(503).JSON(errJSON("ERR_AMEBO_PROVIDER_UNAVAILABLE"))
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			restoreAmeboToken(db, me.UserID, free > 0)
			return c.Status(502).JSON(errJSON("ERR_AMEBO_PROVIDER_FAILED"))
		}
		var output struct {
			Reply string `json:"reply"`
		}
		if json.Unmarshal(raw, &output) != nil || strings.TrimSpace(output.Reply) == "" {
			restoreAmeboToken(db, me.UserID, free > 0)
			return c.Status(502).JSON(errJSON("ERR_AMEBO_PROVIDER_FAILED"))
		}
		return c.JSON(fiber.Map{"reply": output.Reply, "tokens_remaining_free": max(0, free-boolInt(free > 0)), "tokens_remaining_purchased": max(0, purchased-boolInt(free <= 0)), "free_expires_at": expires, "token_cost": 1})
	})
}

func ameboDailyGrant(ctx context.Context, db *pgxpool.Pool, userID string) int {
	var plan string
	err := db.QueryRow(ctx, `SELECT plan_id FROM subscriptions WHERE user_id=$1 AND status='ACTIVE' AND (ends_at IS NULL OR ends_at>now()) ORDER BY starts_at DESC LIMIT 1`, userID).Scan(&plan)
	if err != nil {
		return 10
	}
	switch plan {
	case "swift_pass":
		return 25
	case "executive_club":
		return 40
	case "sovereign_60k", "passenger_lifetime":
		return 50
	case "pro_5k":
		return 35
	default:
		return 10
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func restoreAmeboToken(db *pgxpool.Pool, userID string, free bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if free {
		_, _ = db.Exec(ctx, `UPDATE amebo_wallets SET free_tokens=free_tokens+1 WHERE user_id=$1 AND free_expires_at>now()`, userID)
	} else {
		_, _ = db.Exec(ctx, `UPDATE amebo_wallets SET purchased_tokens=purchased_tokens+1 WHERE user_id=$1`, userID)
	}
}
