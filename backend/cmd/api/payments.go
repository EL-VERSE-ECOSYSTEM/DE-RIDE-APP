package main

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type paystackInitializeResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		AuthorizationURL string `json:"authorization_url"`
		AccessCode       string `json:"access_code"`
		Reference        string `json:"reference"`
	} `json:"data"`
}

type paystackVerifyResponse struct {
	Status bool `json:"status"`
	Data   struct {
		Status    string `json:"status"`
		Reference string `json:"reference"`
		Amount    int64  `json:"amount"`
		Currency  string `json:"currency"`
	} `json:"data"`
}

func registerPaymentRoutes(v1, secure fiber.Router, db *pgxpool.Pool) {
	secure.Post("/wallet/fund", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			AmountNaira int64 `json:"amount_ngn"`
		}
		if err := c.BodyParser(&req); err != nil || req.AmountNaira < 100 || req.AmountNaira > 5_000_000 {
			return c.Status(400).JSON(errJSON("ERR_WALLET_FUND_AMOUNT_INVALID"))
		}
		if os.Getenv("PAYSTACK_SECRET_KEY") == "" || !receiptSigningConfigured() {
			return c.Status(503).JSON(errJSON("ERR_PAYMENT_SERVICE_NOT_CONFIGURED"))
		}
		idempotencyKey := strings.TrimSpace(c.Get("Idempotency-Key"))
		if len(idempotencyKey) < 12 || len(idempotencyKey) > 120 {
			return c.Status(400).JSON(errJSON("ERR_IDEMPOTENCY_KEY_REQUIRED"))
		}
		var email string
		if err := db.QueryRow(c.UserContext(), `SELECT email FROM users WHERE id=$1 AND email_verified=true`, me.UserID).Scan(&email); err != nil {
			return c.Status(403).JSON(errJSON("ERR_EMAIL_VERIFICATION_REQUIRED"))
		}
		intentID, reference, authURL, accessCode, status, err := createWalletFundingIntent(c.UserContext(), db, me.UserID, email, req.AmountNaira*100, idempotencyKey)
		if err != nil {
			return c.Status(502).JSON(errJSON("ERR_PAYSTACK_INITIALIZE_FAILED"))
		}
		return c.Status(201).JSON(fiber.Map{"payment_intent_id": intentID, "reference": reference, "authorization_url": authURL, "access_code": accessCode, "status": status, "amount_ngn": req.AmountNaira})
	})
	secure.Get("/payments/verify", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		reference := strings.TrimSpace(c.Query("reference"))
		if reference == "" {
			return c.Status(400).JSON(errJSON("ERR_PAYMENT_REFERENCE_REQUIRED"))
		}
		var intentID, intentStatus, intentType string
		var amount int64
		if err := db.QueryRow(c.UserContext(), `SELECT id,status,amount_kobo,intent_type FROM payment_intents WHERE user_id=$1 AND provider_reference=$2`, me.UserID, reference).Scan(&intentID, &intentStatus, &amount, &intentType); err != nil {
			return c.Status(404).JSON(errJSON("ERR_PAYMENT_NOT_FOUND"))
		}
		if intentStatus != "SUCCEEDED" {
			var settleErr error
			if intentType == "WALLET_FUNDING" {
				settleErr = verifyAndSettleWalletFunding(c.UserContext(), db, reference)
			} else {
				switch intentType {
				case "TICKET_CHECKOUT":
					settleErr = verifyAndSettleTicketCheckout(c.UserContext(), db, reference)
				case "SUBSCRIPTION":
					settleErr = settlePaystackSubscription(c.UserContext(), db, reference)
				case "AMEBO_TOKENS":
					settleErr = settlePaystackAmeboPack(c.UserContext(), db, reference)
				case "VTU_DATA":
					settleErr = settlePaystackVTU(c.UserContext(), db, reference)
				default:
					settleErr = fmt.Errorf("unsupported Paystack intent type")
				}
			}
			if settleErr != nil {
				return c.Status(409).JSON(errJSON("ERR_PAYMENT_NOT_SETTLED"))
			}
		}
		result := fiber.Map{"status": "SUCCEEDED", "reference": reference, "amount_ngn": amount / 100, "payment_intent_id": intentID}
		if intentType == "TICKET_CHECKOUT" {
			var ticketID string
			_ = db.QueryRow(c.UserContext(), `SELECT purpose_id::text FROM payment_intents WHERE id=$1`, intentID).Scan(&ticketID)
			result["ticket_id"] = ticketID
		}
		result["intent_type"] = intentType
		return c.JSON(result)
	})
	v1.Post("/webhooks/paystack", func(c *fiber.Ctx) error {
		if os.Getenv("PAYSTACK_SECRET_KEY") == "" {
			return c.Status(503).JSON(errJSON("ERR_PAYMENT_SERVICE_NOT_CONFIGURED"))
		}
		if !verifyPaystackWebhookSignature(c.Body(), c.Get("paystack-signature"), c.Get("x-paystack-signature")) {
			return c.Status(401).JSON(errJSON("ERR_INVALID_PAYSTACK_SIGNATURE"))
		}
		var event struct {
			Event string `json:"event"`
			Data  struct {
				Reference string `json:"reference"`
			} `json:"data"`
		}
		if err := json.Unmarshal(c.Body(), &event); err != nil || event.Event == "" {
			return c.Status(400).JSON(errJSON("ERR_INVALID_PAYSTACK_EVENT"))
		}
		digest := sha256.Sum256(c.Body())
		eventID := hex.EncodeToString(digest[:])
		var alreadyProcessed bool
		err := db.QueryRow(c.UserContext(), `INSERT INTO paystack_webhook_events(event_id,reference,event_type,payload) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(event_id) DO UPDATE SET event_id=EXCLUDED.event_id RETURNING processed_at IS NOT NULL`, eventID, event.Data.Reference, event.Event, string(c.Body())).Scan(&alreadyProcessed)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_WEBHOOK_RECORD_FAILED"))
		}
		if alreadyProcessed {
			return c.SendStatus(200)
		}
		go processPaystackWebhookEvent(db, eventID)
		return c.SendStatus(200)
	})
}

func processPaystackWebhookEvent(db *pgxpool.Pool, eventID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var eventType, reference string
	err := db.QueryRow(ctx, `UPDATE paystack_webhook_events SET processing_at=now() WHERE event_id=$1 AND processed_at IS NULL AND (processing_at IS NULL OR processing_at<now()-interval '2 minutes') RETURNING event_type,reference`, eventID).Scan(&eventType, &reference)
	if err != nil {
		return
	}
	if eventType == "charge.success" {
		var intentType string
		if err = db.QueryRow(ctx, `SELECT intent_type FROM payment_intents WHERE provider_reference=$1`, reference).Scan(&intentType); err != nil {
			_, _ = db.Exec(ctx, `UPDATE paystack_webhook_events SET processing_at=NULL,processing_error=$1 WHERE event_id=$2`, err.Error(), eventID)
			return
		}
		if intentType == "WALLET_FUNDING" {
			err = verifyAndSettleWalletFunding(ctx, db, reference)
		} else if intentType == "TICKET_CHECKOUT" {
			err = verifyAndSettleTicketCheckout(ctx, db, reference)
		} else {
			switch intentType {
			case "SUBSCRIPTION":
				err = settlePaystackSubscription(ctx, db, reference)
			case "AMEBO_TOKENS":
				err = settlePaystackAmeboPack(ctx, db, reference)
			case "VTU_DATA":
				err = settlePaystackVTU(ctx, db, reference)
			default:
				err = fmt.Errorf("unsupported Paystack intent type")
			}
		}
		if err != nil {
			_, _ = db.Exec(ctx, `UPDATE paystack_webhook_events SET processing_at=NULL,processing_error=$1 WHERE event_id=$2`, err.Error(), eventID)
			return
		}
	} else if eventType == "charge.failed" {
		_, _ = db.Exec(ctx, `UPDATE payment_intents SET status='FAILED',updated_at=now() WHERE provider_reference=$1 AND status IN ('CREATED','PENDING')`, reference)
		_, _ = db.Exec(ctx, `UPDATE tickets SET status='CANCELLED' WHERE id=(SELECT purpose_id FROM payment_intents WHERE provider_reference=$1 AND intent_type='TICKET_CHECKOUT') AND status='PENDING'`, reference)
	}
	_, _ = db.Exec(ctx, `UPDATE paystack_webhook_events SET processed_at=now(),processing_at=NULL,processing_error=NULL WHERE event_id=$1`, eventID)
}

func processPendingPaystackEvents(db *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	rows, err := db.Query(ctx, `SELECT event_id FROM paystack_webhook_events WHERE processed_at IS NULL AND (processing_at IS NULL OR processing_at<now()-interval '2 minutes') ORDER BY received_at LIMIT 20`)
	if err != nil {
		cancel()
		return
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	cancel()
	for _, id := range ids {
		processPaystackWebhookEvent(db, id)
	}
}

func processExpiredTicketCheckouts(db *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,purpose_id FROM payment_intents WHERE intent_type='TICKET_CHECKOUT' AND status IN ('CREATED','PENDING','FAILED') AND expires_at<=now() ORDER BY expires_at LIMIT 50 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return
	}
	type expired struct{ intent, ticket string }
	items := make([]expired, 0)
	for rows.Next() {
		var x expired
		if rows.Scan(&x.intent, &x.ticket) == nil {
			items = append(items, x)
		}
	}
	rows.Close()
	for _, x := range items {
		_, _ = tx.Exec(ctx, `UPDATE payment_intents SET status='CANCELLED',updated_at=now() WHERE id=$1`, x.intent)
		_, _ = tx.Exec(ctx, `UPDATE tickets SET status='CANCELLED' WHERE id=$1 AND status='PENDING'`, x.ticket)
	}
	_ = tx.Commit(ctx)
}

func verifyPaystackWebhookSignature(body []byte, currentHeader, legacyHeader string) bool {
	legacySecret := os.Getenv("PAYSTACK_SECRET_KEY")
	if legacyHeader != "" && legacySecret != "" && validHexSignature(legacyHeader) {
		mac := hmac.New(sha512.New, []byte(legacySecret))
		_, _ = mac.Write(body)
		if hmac.Equal(mac.Sum(nil), mustDecodeHex(legacyHeader)) {
			return true
		}
	}
	if currentHeader == "" {
		return false
	}
	var timestamp string
	var signatures []string
	for _, part := range strings.Split(currentHeader, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			if timestamp != "" {
				return false
			}
			timestamp = value
		case "v1":
			kid, sig, ok := strings.Cut(value, ":")
			if ok && strings.HasPrefix(kid, "whk_") && validHexSignature(sig) {
				signatures = append(signatures, sig)
			}
		}
	}
	if timestamp == "" || len(signatures) == 0 {
		return false
	}
	signedAt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(signedAt, 0)) > 5*time.Minute || time.Until(time.Unix(signedAt, 0)) > 5*time.Minute {
		return false
	}
	secrets := strings.Split(os.Getenv("PAYSTACK_WEBHOOK_SECRETS"), ";")
	if os.Getenv("PAYSTACK_WEBHOOK_SECRET") != "" {
		secrets = append(secrets, os.Getenv("PAYSTACK_WEBHOOK_SECRET"))
	}
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		mac := hmac.New(sha512.New, []byte(secret))
		_, _ = mac.Write([]byte(timestamp + "."))
		_, _ = mac.Write(body)
		expected := mac.Sum(nil)
		for _, signature := range signatures {
			if hmac.Equal(expected, mustDecodeHex(signature)) {
				return true
			}
		}
	}
	return false
}

func validHexSignature(value string) bool {
	if len(value) != 128 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func mustDecodeHex(value string) []byte {
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil
	}
	return decoded
}

func createWalletFundingIntent(ctx context.Context, db *pgxpool.Pool, userID, email string, amountKobo int64, key string) (string, string, string, string, string, error) {
	reference := "DRW-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var intentID, status string
	var authURL, accessCode *string
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", "", "", "", "", err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO payment_intents(user_id,intent_type,payment_method,idempotency_key,provider_reference,amount_kobo,status,metadata) VALUES($1,'WALLET_FUNDING','PAYSTACK',$2,$3,$4,'CREATED','{}'::jsonb) ON CONFLICT(user_id,idempotency_key) DO NOTHING RETURNING id,status,authorization_url,access_code`, userID, key, reference, amountKobo).Scan(&intentID, &status, &authURL, &accessCode)
	if err == pgx.ErrNoRows {
		var existingAmount int64
		if err = tx.QueryRow(ctx, `SELECT id,status,authorization_url,access_code,provider_reference,amount_kobo FROM payment_intents WHERE user_id=$1 AND idempotency_key=$2 FOR UPDATE`, userID, key).Scan(&intentID, &status, &authURL, &accessCode, &reference, &existingAmount); err != nil {
			return "", "", "", "", "", err
		}
		if existingAmount != amountKobo {
			return "", "", "", "", "", fmt.Errorf("idempotency key amount mismatch")
		}
		if err = tx.Commit(ctx); err != nil {
			return "", "", "", "", "", err
		}
		if authURL != nil {
			return intentID, reference, *authURL, stringValue(accessCode), status, nil
		}
		// A prior initialize attempt may have died after creating the intent. Retry
		// the same provider reference so the idempotency key can recover cleanly.
	}
	if err != nil {
		return "", "", "", "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", "", "", "", err
	}
	frontendURL := strings.TrimRight(os.Getenv("PUBLIC_FRONTEND_URL"), "/")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	callback := frontendURL + "/wallet?payment=return"
	requestBody, _ := json.Marshal(map[string]any{"email": email, "amount": amountKobo, "reference": reference, "currency": "NGN", "callback_url": callback, "metadata": map[string]any{"intent_id": intentID, "purpose": "WALLET_FUNDING", "user_id": userID}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.paystack.co/transaction/initialize", strings.NewReader(string(requestBody)))
	if err != nil {
		return "", "", "", "", "", err
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv("PAYSTACK_SECRET_KEY"))
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return "", "", "", "", "", err
	}
	defer response.Body.Close()
	var provider paystackInitializeResponse
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&provider); err != nil || response.StatusCode < 200 || response.StatusCode >= 300 || !provider.Status || provider.Data.Reference != reference {
		_, _ = db.Exec(ctx, `UPDATE payment_intents SET status='FAILED',updated_at=now() WHERE id=$1`, intentID)
		return "", "", "", "", "", fmt.Errorf("provider rejected initialization")
	}
	_, err = db.Exec(ctx, `UPDATE payment_intents SET status='PENDING',authorization_url=$1,access_code=$2,expires_at=now()+interval '30 minutes',updated_at=now() WHERE id=$3`, provider.Data.AuthorizationURL, provider.Data.AccessCode, intentID)
	return intentID, reference, provider.Data.AuthorizationURL, provider.Data.AccessCode, "PENDING", err
}

func initializeExistingPaystackIntent(ctx context.Context, db *pgxpool.Pool, intentID, userID, email, reference string, amountKobo int64, purpose string) (string, string, error) {
	var savedURL, savedCode *string
	var status string
	if err := db.QueryRow(ctx, `SELECT status,authorization_url,access_code FROM payment_intents WHERE id=$1 AND user_id=$2 AND provider_reference=$3 AND intent_type=$4`, intentID, userID, reference, purpose).Scan(&status, &savedURL, &savedCode); err != nil {
		return "", "", err
	}
	if savedURL != nil && status == "PENDING" {
		return *savedURL, stringValue(savedCode), nil
	}
	if status != "CREATED" && status != "PENDING" {
		return "", "", fmt.Errorf("payment intent is not initializable")
	}
	frontend := strings.TrimRight(os.Getenv("PUBLIC_FRONTEND_URL"), "/")
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	callback := frontend + "/wallet?payment=return"
	switch purpose {
	case "TICKET_CHECKOUT":
		callback = frontend + "/tickets?payment=return"
	case "SUBSCRIPTION":
		callback = frontend + "/subscriptions?payment=return"
	case "AMEBO_TOKENS":
		callback = frontend + "/amebo?payment=return"
	case "VTU_DATA":
		callback = frontend + "/data?payment=return"
	case "RIDER_ORDER":
		callback = frontend + "/mobility?payment=return"
	}
	body, _ := json.Marshal(map[string]any{"email": email, "amount": amountKobo, "reference": reference, "currency": "NGN", "callback_url": callback, "metadata": map[string]any{"intent_id": intentID, "purpose": purpose, "user_id": userID}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.paystack.co/transaction/initialize", strings.NewReader(string(body)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("PAYSTACK_SECRET_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var provider paystackInitializeResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&provider); err != nil {
		return "", "", err
	}
	if resp.StatusCode >= 500 {
		return "", "", fmt.Errorf("provider initialization outcome is ambiguous")
	}
	if resp.StatusCode >= 400 || !provider.Status {
		_, _ = db.Exec(ctx, `UPDATE payment_intents SET status='FAILED',updated_at=now() WHERE id=$1`, intentID)
		if purpose == "TICKET_CHECKOUT" {
			_, _ = db.Exec(ctx, `UPDATE tickets SET status='CANCELLED' WHERE id=(SELECT purpose_id FROM payment_intents WHERE id=$1) AND status='PENDING'`, intentID)
		}
		return "", "", fmt.Errorf("provider rejected initialization")
	}
	if provider.Data.Reference != reference || provider.Data.AuthorizationURL == "" {
		return "", "", fmt.Errorf("provider initialization reference could not be verified")
	}
	_, err = db.Exec(ctx, `UPDATE payment_intents SET status='PENDING',authorization_url=$1,access_code=$2,expires_at=now()+interval '30 minutes',updated_at=now() WHERE id=$3 AND status IN ('CREATED','PENDING')`, provider.Data.AuthorizationURL, provider.Data.AccessCode, intentID)
	return provider.Data.AuthorizationURL, provider.Data.AccessCode, err
}

func createPurposePaystackIntent(ctx context.Context, db *pgxpool.Pool, userID, email, intentType, key string, amountKobo int64, metadata map[string]any) (string, string, string, string, error) {
	if os.Getenv("PAYSTACK_SECRET_KEY") == "" || amountKobo < 100 || strings.TrimSpace(email) == "" {
		return "", "", "", "", fmt.Errorf("Paystack is not configured or intent is invalid")
	}
	key = strings.TrimSpace(key)
	if len(key) < 12 || len(key) > 120 {
		return "", "", "", "", fmt.Errorf("invalid idempotency key")
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", "", "", "", err
	}
	reference := "DR" + intentType[:min(3, len(intentType))] + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", "", "", "", err
	}
	defer tx.Rollback(ctx)
	var id, status string
	var savedURL, savedCode *string
	err = tx.QueryRow(ctx, `INSERT INTO payment_intents(user_id,intent_type,payment_method,idempotency_key,provider_reference,amount_kobo,status,metadata) VALUES($1,$2,'PAYSTACK',$3,$4,$5,'CREATED',$6::jsonb) ON CONFLICT(user_id,idempotency_key) DO NOTHING RETURNING id,status,authorization_url,access_code`, userID, intentType, intentType+":"+key, reference, amountKobo, string(metadataJSON)).Scan(&id, &status, &savedURL, &savedCode)
	if err == pgx.ErrNoRows {
		var existingType string
		var existingAmount int64
		var existingMetadata []byte
		if err = tx.QueryRow(ctx, `SELECT id,intent_type,amount_kobo,metadata,status,authorization_url,access_code,provider_reference FROM payment_intents WHERE user_id=$1 AND idempotency_key=$2 FOR UPDATE`, userID, intentType+":"+key).Scan(&id, &existingType, &existingAmount, &existingMetadata, &status, &savedURL, &savedCode, &reference); err != nil {
			return "", "", "", "", err
		}
		if existingType != intentType || existingAmount != amountKobo || !jsonEqual(existingMetadata, metadataJSON) {
			return "", "", "", "", fmt.Errorf("idempotency payload mismatch")
		}
	}
	if err != nil {
		return "", "", "", "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", "", "", "", err
	}
	if status == "SUCCEEDED" {
		return id, reference, stringValue(savedURL), stringValue(savedCode), nil
	}
	authURL, accessCode, err := initializeExistingPaystackIntent(ctx, db, id, userID, email, reference, amountKobo, intentType)
	return id, reference, authURL, accessCode, err
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && fmt.Sprint(a) == fmt.Sprint(b)
}

func verifyPaystackIntentAmount(ctx context.Context, reference string, expected int64) error {
	if os.Getenv("PAYSTACK_SECRET_KEY") == "" {
		return fmt.Errorf("payment provider is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.paystack.co/transaction/verify/"+url.PathEscape(reference), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("PAYSTACK_SECRET_KEY"))
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var verified paystackVerifyResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&verified); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !verified.Status || verified.Data.Status != "success" || verified.Data.Reference != reference || verified.Data.Currency != "NGN" || verified.Data.Amount != expected {
		return fmt.Errorf("verified payment does not match intent")
	}
	return nil
}

func settlePaystackSubscription(ctx context.Context, db *pgxpool.Pool, reference string) error {
	var amount int64
	if err := db.QueryRow(ctx, `SELECT amount_kobo FROM payment_intents WHERE provider_reference=$1 AND intent_type='SUBSCRIPTION'`, reference).Scan(&amount); err != nil {
		return err
	}
	if err := verifyPaystackIntentAmount(ctx, reference, amount); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, userID, status string
	var metadata []byte
	if err = tx.QueryRow(ctx, `SELECT id,user_id,status,metadata FROM payment_intents WHERE provider_reference=$1 AND intent_type='SUBSCRIPTION' FOR UPDATE`, reference).Scan(&id, &userID, &status, &metadata); err != nil {
		return err
	}
	if status == "SUCCEEDED" {
		return tx.Commit(ctx)
	}
	if status != "CREATED" && status != "PENDING" {
		return fmt.Errorf("payment intent is not settleable")
	}
	var payload struct {
		PlanID string `json:"plan_id"`
	}
	if json.Unmarshal(metadata, &payload) != nil || payload.PlanID == "" {
		return fmt.Errorf("subscription metadata invalid")
	}
	var price int64
	var interval string
	if err = tx.QueryRow(ctx, `SELECT price_ngn,interval FROM subscription_plans WHERE id=$1`, payload.PlanID).Scan(&price, &interval); err != nil {
		return err
	}
	if price*100 != amount {
		return fmt.Errorf("subscription price mismatch")
	}
	if _, err = tx.Exec(ctx, `UPDATE subscriptions SET status='SUPERSEDED',ends_at=now() WHERE user_id=$1 AND status='ACTIVE'`, userID); err != nil {
		return err
	}
	var ends any
	if interval != "one_time" {
		ends = time.Now().Add(30 * 24 * time.Hour)
	}
	var subscriptionID string
	if err = tx.QueryRow(ctx, `INSERT INTO subscriptions(user_id,plan_id,starts_at,ends_at,status) VALUES($1,$2,now(),$3,'ACTIVE') RETURNING id`, userID, payload.PlanID, ends).Scan(&subscriptionID); err != nil {
		return err
	}
	ledgerID, err := insertLedger(ctx, tx, userID, "subscription-paystack:"+id, -price, "SUBSCRIPTION")
	if err != nil {
		return err
	}
	if err = storeSignedReceipt(ctx, tx, userID, "SUBSCRIPTION", ledgerID, map[string]any{"subscription_id": subscriptionID, "plan_id": payload.PlanID, "amount_ngn": price, "reference": reference, "type": "SUBSCRIPTION", "issued_at": time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_intents SET status='SUCCEEDED',settled_at=now(),updated_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'SUBSCRIPTION','Subscription active','Your subscription payment was confirmed.','/transactions',jsonb_build_object('plan_id',$2))`, userID, payload.PlanID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	go sendPushNotificationToUser(db, userID, "Subscription active", "Your subscription payment was confirmed.", "/transactions")
	return nil
}

func settlePaystackAmeboPack(ctx context.Context, db *pgxpool.Pool, reference string) error {
	var amount int64
	if err := db.QueryRow(ctx, `SELECT amount_kobo FROM payment_intents WHERE provider_reference=$1 AND intent_type='AMEBO_TOKENS'`, reference).Scan(&amount); err != nil {
		return err
	}
	if err := verifyPaystackIntentAmount(ctx, reference, amount); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id, userID, status string
	var metadata []byte
	if err = tx.QueryRow(ctx, `SELECT id,user_id,status,metadata FROM payment_intents WHERE provider_reference=$1 AND intent_type='AMEBO_TOKENS' FOR UPDATE`, reference).Scan(&id, &userID, &status, &metadata); err != nil {
		return err
	}
	if status == "SUCCEEDED" {
		return tx.Commit(ctx)
	}
	if status != "CREATED" && status != "PENDING" {
		return fmt.Errorf("payment intent is not settleable")
	}
	var payload struct {
		PackID string `json:"pack_id"`
	}
	if json.Unmarshal(metadata, &payload) != nil {
		return fmt.Errorf("token pack metadata invalid")
	}
	pack, ok := ameboPacks[payload.PackID]
	if !ok || pack.Price*100 != amount {
		return fmt.Errorf("token pack price mismatch")
	}
	grant := ameboDailyGrant(ctx, db, userID)
	if _, err = tx.Exec(ctx, `INSERT INTO amebo_wallets(user_id,free_tokens,free_expires_at,purchased_tokens) VALUES($1,$2,(date_trunc('day',now() AT TIME ZONE 'Africa/Lagos')+interval '1 day') AT TIME ZONE 'Africa/Lagos',0) ON CONFLICT(user_id) DO NOTHING`, userID, grant); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE amebo_wallets SET purchased_tokens=purchased_tokens+$1 WHERE user_id=$2`, pack.Tokens, userID); err != nil {
		return err
	}
	ledgerID, err := insertLedger(ctx, tx, userID, "amebo-paystack:"+id, -pack.Price, "AMEBO_TOKENS")
	if err != nil {
		return err
	}
	if err = storeSignedReceipt(ctx, tx, userID, "AMEBO_TOKENS", ledgerID, map[string]any{"pack_id": payload.PackID, "tokens": pack.Tokens, "amount_ngn": pack.Price, "reference": reference, "type": "AMEBO_TOKENS", "issued_at": time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_intents SET status='SUCCEEDED',settled_at=now(),updated_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'AMEBO','Amebo tokens added','Your purchased tokens are ready.','/amebo',jsonb_build_object('tokens',$2))`, userID, pack.Tokens); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	go sendPushNotificationToUser(db, userID, "Amebo tokens added", "Your purchased tokens are ready.", "/amebo")
	return nil
}

func settlePaystackVTU(ctx context.Context, db *pgxpool.Pool, reference string) error {
	var amount int64
	if err := db.QueryRow(ctx, `SELECT amount_kobo FROM payment_intents WHERE provider_reference=$1 AND intent_type='VTU_DATA'`, reference).Scan(&amount); err != nil {
		return err
	}
	if err := verifyPaystackIntentAmount(ctx, reference, amount); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var intentID, userID, status, idempotency string
	var metadata []byte
	if err = tx.QueryRow(ctx, `SELECT id,user_id,status,idempotency_key,metadata FROM payment_intents WHERE provider_reference=$1 AND intent_type='VTU_DATA' FOR UPDATE`, reference).Scan(&intentID, &userID, &status, &idempotency, &metadata); err != nil {
		return err
	}
	if status == "SUCCEEDED" {
		return tx.Commit(ctx)
	}
	if status != "CREATED" && status != "PENDING" {
		return fmt.Errorf("payment intent is not settleable")
	}
	var payload struct {
		Network   string `json:"network"`
		PlanCode  string `json:"plan_code"`
		Phone     string `json:"phone"`
		Allowance string `json:"allowance"`
		Price     int64  `json:"price_ngn"`
	}
	if json.Unmarshal(metadata, &payload) != nil || !validVTUNetwork(payload.Network) || payload.PlanCode == "" || !validNigerianPhone(payload.Phone) || payload.Price*100 != amount || payload.Allowance == "" {
		return fmt.Errorf("VTU metadata invalid")
	}
	key := strings.TrimPrefix(idempotency, "VTU_DATA:")
	if key == idempotency || len(key) < 12 {
		return fmt.Errorf("VTU idempotency key invalid")
	}
	orderID := uuid.NewString()
	providerReference := "DRV-" + strings.ReplaceAll(orderID, "-", "")
	if _, err = tx.Exec(ctx, `INSERT INTO vtu_data_orders(id,user_id,plan_code,phone,amount_ngn,provider_reference,status,idempotency_key,network,allowance,attempts) VALUES($1,$2,$3,$4,$5,$6,'PROCESSING',$7,$8,$9,0) ON CONFLICT(user_id,idempotency_key) DO NOTHING`, orderID, userID, payload.PlanCode, payload.Phone, payload.Price, providerReference, key, payload.Network, payload.Allowance); err != nil {
		return err
	}
	var savedOrder string
	if err = tx.QueryRow(ctx, `SELECT id FROM vtu_data_orders WHERE user_id=$1 AND idempotency_key=$2`, userID, key).Scan(&savedOrder); err != nil {
		return err
	}
	ledgerID, err := insertLedger(ctx, tx, userID, "vtu:"+userID+":"+key, -payload.Price, "VTU_DATA")
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_intents SET purpose_id=$2,status='SUCCEEDED',settled_at=now(),updated_at=now() WHERE id=$1`, intentID, savedOrder); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'PAYMENT','Data purchase paid','Your data bundle is being delivered.','/data',jsonb_build_object('order_id',$2))`, userID, savedOrder); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	_ = ledgerID
	return nil
}

func verifyAndSettleWalletFunding(ctx context.Context, db *pgxpool.Pool, reference string) error {
	secret := os.Getenv("PAYSTACK_SECRET_KEY")
	if secret == "" {
		return fmt.Errorf("payment provider is not configured")
	}
	endpoint := "https://api.paystack.co/transaction/verify/" + url.PathEscape(reference)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var verified paystackVerifyResponse
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&verified); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || !verified.Status || verified.Data.Status != "success" || verified.Data.Reference != reference || verified.Data.Currency != "NGN" {
		return fmt.Errorf("provider transaction is not successful")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var intentID, userID string
	var amount int64
	var status string
	err = tx.QueryRow(ctx, `SELECT id,user_id,amount_kobo,status FROM payment_intents WHERE provider_reference=$1 AND intent_type='WALLET_FUNDING' FOR UPDATE`, reference).Scan(&intentID, &userID, &amount, &status)
	if err != nil {
		return err
	}
	if amount != verified.Data.Amount {
		return fmt.Errorf("verified amount mismatch")
	}
	if status == "SUCCEEDED" {
		return tx.Commit(ctx)
	}
	if status != "PENDING" && status != "CREATED" {
		return fmt.Errorf("payment intent is not settleable")
	}
	if amount%100 != 0 {
		return fmt.Errorf("wallet funding must settle in whole naira")
	}
	var email string
	if err = tx.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, userID).Scan(&email); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO wallet_accounts(user_id,balance_ngn) VALUES($1,0) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE wallet_accounts SET balance_ngn=balance_ngn+$1 WHERE user_id=$2`, amount/100, userID); err != nil {
		return err
	}
	ledgerKey := "wallet-fund:" + intentID
	var ledgerID string
	err = tx.QueryRow(ctx, `INSERT INTO ledger_entries(user_id,idempotency_key,amount_ngn,entry_type) VALUES($1,$2,$3,'WALLET_FUNDING') ON CONFLICT(idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id`, userID, ledgerKey, amount/100).Scan(&ledgerID)
	if err != nil {
		return err
	}
	claims := map[string]any{"receipt_id": uuid.NewString(), "reference": reference, "user_id": userID, "email": email, "type": "WALLET_FUNDING", "amount_ngn": amount / 100, "currency": "NGN", "issued_at": time.Now().UTC().Format(time.RFC3339)}
	signed, keyID, err := signReceiptClaims(claims)
	if err != nil {
		return err
	}
	claimBytes, _ := json.Marshal(claims)
	signedBytes, _ := json.Marshal(signed)
	if _, err = tx.Exec(ctx, `INSERT INTO signed_receipts(user_id,payment_intent_id,ledger_entry_id,receipt_type,key_id,claims,signed_payload) VALUES($1,$2,$3,'WALLET_FUNDING',$4,$5::jsonb,$6) ON CONFLICT(ledger_entry_id) DO NOTHING`, userID, intentID, ledgerID, keyID, string(claimBytes), string(signedBytes)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_intents SET status='SUCCEEDED',settled_at=now(),updated_at=now() WHERE id=$1`, intentID); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'WALLET','Wallet funded','Your wallet was credited with NGN '||$2::text,'/transactions',jsonb_build_object('reference',$3))`, userID, amount/100, reference)
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	go sendPushNotificationToUser(db, userID, "Wallet funded", "Your DE RIDE wallet has been credited.", "/transactions")
	return nil
}

func verifyAndSettleTicketCheckout(ctx context.Context, db *pgxpool.Pool, reference string) error {
	secret := os.Getenv("PAYSTACK_SECRET_KEY")
	if secret == "" {
		return fmt.Errorf("payment provider is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.paystack.co/transaction/verify/"+url.PathEscape(reference), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var verified paystackVerifyResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&verified); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !verified.Status || verified.Data.Status != "success" || verified.Data.Reference != reference || verified.Data.Currency != "NGN" {
		return fmt.Errorf("provider transaction is not successful")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var intentID, userID, ticketID, status string
	var amount int64
	if err = tx.QueryRow(ctx, `SELECT id,user_id,purpose_id,amount_kobo,status FROM payment_intents WHERE provider_reference=$1 AND intent_type='TICKET_CHECKOUT' FOR UPDATE`, reference).Scan(&intentID, &userID, &ticketID, &amount, &status); err != nil {
		return err
	}
	if amount != verified.Data.Amount {
		return fmt.Errorf("verified amount mismatch")
	}
	if status == "SUCCEEDED" {
		return tx.Commit(ctx)
	}
	if status != "PENDING" && status != "CREATED" {
		return fmt.Errorf("ticket payment is not settleable")
	}
	var ticketStatus, manualCode, checkoutKey string
	var total int64
	if err = tx.QueryRow(ctx, `SELECT status,manual_code,checkout_key,total_paid_ngn FROM tickets WHERE id=$1 AND passenger_id=$2 FOR UPDATE`, ticketID, userID).Scan(&ticketStatus, &manualCode, &checkoutKey, &total); err != nil {
		return err
	}
	if ticketStatus != "PENDING" || total*100 != amount {
		return fmt.Errorf("ticket reservation is invalid")
	}
	ledgerID, err := insertLedger(ctx, tx, userID, "ticket:"+userID+":"+checkoutKey, -total, "TICKET_PURCHASE")
	if err != nil {
		return err
	}
	claims := map[string]any{"receipt_id": ledgerID, "ticket_id": ticketID, "reference": reference, "user_id": userID, "type": "TICKET_PURCHASE", "amount_ngn": total, "currency": "NGN", "issued_at": time.Now().UTC().Format(time.RFC3339)}
	if err = storeSignedReceipt(ctx, tx, userID, "TICKET_PURCHASE", ledgerID, claims); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE signed_receipts SET payment_intent_id=$1 WHERE ledger_entry_id=$2`, intentID, ledgerID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE tickets SET status='CONFIRMED' WHERE id=$1`, ticketID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE payment_intents SET status='SUCCEEDED',settled_at=now(),updated_at=now() WHERE id=$1`, intentID); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'TRIP','Ticket confirmed',$2,'/tickets',jsonb_build_object('ticket_id',$3))`, userID, "Your ticket is confirmed. Boarding code: "+manualCode, ticketID)
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	go sendPushNotificationToUser(db, userID, "Ticket confirmed", "Your ticket is ready to board.", "/tickets")
	return nil
}

func signReceiptClaims(claims map[string]any) (map[string]string, string, error) {
	privatePEM := strings.ReplaceAll(os.Getenv("QR_PRIVATE_KEY_PEM"), `\n`, "\n")
	block, _ := pem.Decode([]byte(privatePEM))
	if block == nil {
		return nil, "", fmt.Errorf("receipt signing key is missing")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, parseErr := x509.ParsePKCS1PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, "", fmt.Errorf("receipt key must be RSA")
		}
		parsed = key
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 {
		return nil, "", fmt.Errorf("receipt key must be RSA with at least 2048 bits")
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(body)
	raw, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return nil, "", err
	}
	keyID := os.Getenv("QR_KEY_ID")
	if keyID == "" {
		return nil, "", fmt.Errorf("receipt key ID is missing")
	}
	return map[string]string{"claims": base64.RawURLEncoding.EncodeToString(body), "signature": base64.RawURLEncoding.EncodeToString(raw), "key_id": keyID}, keyID, nil
}

func receiptSigningConfigured() bool {
	return os.Getenv("QR_PRIVATE_KEY_PEM") != "" && os.Getenv("QR_KEY_ID") != ""
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
