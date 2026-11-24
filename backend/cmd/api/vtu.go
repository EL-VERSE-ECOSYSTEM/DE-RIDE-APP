package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type vtuPlan struct {
	Code      string `json:"code"`
	Network   string `json:"network"`
	Allowance string `json:"allowance"`
	Price     int64  `json:"price_ngn"`
}
type vtuProviderResponse struct {
	Status  bool            `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func registerVTURoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Get("/data/plans", func(c *fiber.Ctx) error {
		network := strings.ToUpper(strings.TrimSpace(c.Query("network")))
		if network != "" && !validVTUNetwork(network) {
			return c.Status(422).JSON(errJSON("ERR_VTU_NETWORK_INVALID"))
		}
		if err := refreshVTUPlans(c.UserContext(), db); err != nil {
			var count int
			if db.QueryRow(c.UserContext(), `SELECT count(*) FROM vtu_data_plans WHERE active=true AND refreshed_at>now()-interval '24 hours'`).Scan(&count) != nil || count == 0 {
				return c.Status(503).JSON(errJSON("ERR_VTU_PLANS_UNAVAILABLE"))
			}
		}
		rows, err := db.Query(c.UserContext(), `SELECT code,network,allowance,price_ngn FROM vtu_data_plans WHERE active=true AND ($1='' OR network=$1) ORDER BY network,price_ngn`, network)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_VTU_PLANS_UNAVAILABLE"))
		}
		defer rows.Close()
		plans := make([]fiber.Map, 0)
		for rows.Next() {
			var p vtuPlan
			if err = rows.Scan(&p.Code, &p.Network, &p.Allowance, &p.Price); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			plans = append(plans, fiber.Map{"code": p.Code, "network": p.Network, "allowance": p.Allowance, "price_ngn": p.Price})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"plans": plans, "updated_at": time.Now().UTC()})
	})
	secure.Post("/data/purchase", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			Network       string `json:"network"`
			PlanCode      string `json:"plan_code"`
			Phone         string `json:"beneficiary_phone"`
			PaymentMethod string `json:"payment_method"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Network = strings.ToUpper(strings.TrimSpace(req.Network))
		req.PlanCode = strings.TrimSpace(req.PlanCode)
		req.Phone = normalizeNigerianPhone(req.Phone)
		req.PaymentMethod = strings.ToUpper(strings.TrimSpace(req.PaymentMethod))
		if req.PaymentMethod == "" {
			req.PaymentMethod = "WALLET"
		}
		key := strings.TrimSpace(c.Get("Idempotency-Key"))
		if !validVTUNetwork(req.Network) || req.PlanCode == "" || len(req.PlanCode) > 100 || !validNigerianPhone(req.Phone) || (req.PaymentMethod != "WALLET" && req.PaymentMethod != "PAYSTACK") || len(key) < 12 || len(key) > 120 {
			return c.Status(422).JSON(errJSON("ERR_VTU_PURCHASE_INVALID"))
		}
		var plan vtuPlan
		err := db.QueryRow(c.UserContext(), `SELECT code,network,allowance,price_ngn FROM vtu_data_plans WHERE code=$1 AND network=$2 AND active=true`, req.PlanCode, req.Network).Scan(&plan.Code, &plan.Network, &plan.Allowance, &plan.Price)
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_VTU_PLAN_NOT_FOUND"))
		}
		if req.PaymentMethod == "PAYSTACK" {
			if os.Getenv("PAYSTACK_SECRET_KEY") == "" || !receiptSigningConfigured() { return c.Status(503).JSON(errJSON("ERR_PAYMENT_SERVICE_NOT_CONFIGURED")) }
			var email string
			if err = db.QueryRow(c.UserContext(),`SELECT email FROM users WHERE id=$1 AND email_verified=true`,me.UserID).Scan(&email);err!=nil{return c.Status(403).JSON(errJSON("ERR_EMAIL_VERIFICATION_REQUIRED"))}
			metadata:=map[string]any{"network":plan.Network,"plan_code":plan.Code,"phone":req.Phone,"allowance":plan.Allowance,"price_ngn":plan.Price}
			id,reference,authURL,accessCode,intentErr:=createPurposePaystackIntent(c.UserContext(),db,me.UserID,email,"VTU_DATA",key,plan.Price*100,metadata)
			if intentErr!=nil{return c.Status(409).JSON(errJSON("ERR_PAYSTACK_INITIALIZE_FAILED"))}
			return c.Status(201).JSON(fiber.Map{"payment_intent_id":id,"reference":reference,"authorization_url":authURL,"access_code":accessCode,"status":"PENDING","network":plan.Network,"allowance":plan.Allowance,"amount_ngn":plan.Price})
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		orderID := uuid.NewString()
		providerRef := "DRV-" + strings.ReplaceAll(orderID, "-", "")
		var foundID, foundStatus, foundNetwork, foundCode, foundPhone string
		var foundRef *string
		err = tx.QueryRow(c.UserContext(), `SELECT id,status,provider_reference,network,plan_code,phone FROM vtu_data_orders WHERE user_id=$1 AND idempotency_key=$2 FOR UPDATE`, me.UserID, key).Scan(&foundID, &foundStatus, &foundRef, &foundNetwork, &foundCode, &foundPhone)
		if err == nil {
			if foundNetwork != req.Network || foundCode != req.PlanCode || foundPhone != req.Phone {
				return c.Status(409).JSON(errJSON("ERR_IDEMPOTENCY_KEY_PAYLOAD_MISMATCH"))
			}
			if err = tx.Commit(c.UserContext()); err != nil {
				return c.Status(500).JSON(errJSON("ERR_VTU_ORDER_FAILED"))
			}
			return c.JSON(fiber.Map{"order_id": foundID, "provider_reference": foundRef, "status": foundStatus})
		}
		if err != pgx.ErrNoRows {
			return c.Status(503).JSON(errJSON("ERR_VTU_ORDER_FAILED"))
		}
		var balance int64
		if err = tx.QueryRow(c.UserContext(), `SELECT balance_ngn FROM wallet_accounts WHERE user_id=$1 FOR UPDATE`, me.UserID).Scan(&balance); err != nil {
			return c.Status(404).JSON(errJSON("ERR_WALLET_NOT_FOUND"))
		}
		if balance < plan.Price {
			return c.Status(402).JSON(fiber.Map{"error": "ERR_WALLET_BALANCE_INSUFFICIENT", "required_ngn": plan.Price, "balance_ngn": balance})
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE wallet_accounts SET balance_ngn=balance_ngn-$1 WHERE user_id=$2`, plan.Price, me.UserID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_WALLET_DEBIT_FAILED"))
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO vtu_data_orders(id,user_id,plan_code,phone,amount_ngn,provider_reference,status,idempotency_key,network,allowance,attempts) VALUES($1,$2,$3,$4,$5,$6,'PROCESSING',$7,$8,$9,0)`, orderID, me.UserID, plan.Code, req.Phone, plan.Price, providerRef, key, plan.Network, plan.Allowance); err != nil {
			return c.Status(409).JSON(errJSON("ERR_VTU_IDEMPOTENCY_CONFLICT"))
		}
		if _, err = insertLedger(c.UserContext(), tx, me.UserID, "vtu:"+me.UserID+":"+key, -plan.Price, "VTU_DATA"); err != nil {
			return c.Status(409).JSON(errJSON("ERR_LEDGER_WRITE_FAILED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_VTU_ORDER_FAILED"))
		}
		status, providerErr := vendVTU(c.UserContext(), plan, req.Phone, providerRef)
		if providerErr != nil {
			if status == "FAILED" {
				if reverseErr := reverseFailedVTU(c.UserContext(), db, orderID, me.UserID, key, plan.Price, providerErr.Error()); reverseErr != nil {
					return c.Status(503).JSON(fiber.Map{"error": "ERR_VTU_REFUND_PENDING", "order_id": orderID, "status": "PROCESSING"})
				}
				return c.Status(502).JSON(fiber.Map{"error": "ERR_VTU_VENDOR_REJECTED", "order_id": orderID, "refunded": true})
			}
			return c.Status(202).JSON(fiber.Map{"order_id": orderID, "provider_reference": providerRef, "status": "PROCESSING"})
		}
		if err = completeVTU(c.UserContext(), db, orderID, me.UserID, providerRef, plan); err != nil {
			return c.Status(202).JSON(fiber.Map{"order_id": orderID, "provider_reference": providerRef, "status": "PROCESSING"})
		}
		return c.Status(201).JSON(fiber.Map{"order_id": orderID, "provider_reference": providerRef, "status": "SUCCEEDED", "network": plan.Network, "allowance": plan.Allowance, "amount_ngn": plan.Price})
	})
	secure.Get("/data/orders", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		rows, err := db.Query(c.UserContext(), `SELECT id,network,plan_code,allowance,phone,amount_ngn,status,provider_reference,created_at FROM vtu_data_orders WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_VTU_ORDERS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, network, code, allowance, phone, status string
			var amount int64
			var ref *string
			var created time.Time
			if err = rows.Scan(&id, &network, &code, &allowance, &phone, &amount, &status, &ref, &created); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "network": network, "plan_code": code, "allowance": allowance, "phone": phone, "amount_ngn": amount, "status": status, "provider_reference": ref, "created_at": created})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"orders": items})
	})
}

func refreshVTUPlans(ctx context.Context, db *pgxpool.Pool) error {
	var fresh bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM vtu_data_plans WHERE active=true AND refreshed_at>now()-interval '15 minutes')`).Scan(&fresh); err != nil {
		return err
	}
	if fresh {
		return nil
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("VTU_BASE_URL")), "/")
	if base == "" || os.Getenv("VTU_API_KEY") == "" {
		return fmt.Errorf("VTU provider is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/data/plans", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("VTU_API_KEY"))
	resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var envelope vtuProviderResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&envelope); err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || !envelope.Status {
		return fmt.Errorf("VTU plans request rejected")
	}
	var plans []vtuPlan
	if err = json.Unmarshal(envelope.Data, &plans); err != nil || len(plans) == 0 {
		return fmt.Errorf("VTU returned no plans")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE vtu_data_plans SET active=false`); err != nil {
		return err
	}
	for _, p := range plans {
		p.Network = strings.ToUpper(strings.TrimSpace(p.Network))
		p.Code = strings.TrimSpace(p.Code)
		p.Allowance = strings.TrimSpace(p.Allowance)
		if !validVTUNetwork(p.Network) || p.Code == "" || p.Price <= 0 || p.Allowance == "" {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO vtu_data_plans(provider,code,network,allowance,price_ngn,active,refreshed_at) VALUES('configured', $1,$2,$3,$4,true,now()) ON CONFLICT(provider,code) DO UPDATE SET network=EXCLUDED.network,allowance=EXCLUDED.allowance,price_ngn=EXCLUDED.price_ngn,active=true,refreshed_at=now()`, p.Code, p.Network, p.Allowance, p.Price); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func vendVTU(ctx context.Context, p vtuPlan, phone, reference string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("VTU_BASE_URL")), "/")
	body, _ := json.Marshal(map[string]string{"network": p.Network, "plan_code": p.Code, "phone": phone, "reference": reference})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/data/purchase", strings.NewReader(string(body)))
	if err != nil {
		return "PROCESSING", err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("VTU_API_KEY"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "PROCESSING", err
	}
	defer resp.Body.Close()
	var result vtuProviderResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "PROCESSING", err
	}
	if resp.StatusCode >= 500 {
		return "PROCESSING", fmt.Errorf("vendor response is ambiguous")
	}
	if resp.StatusCode >= 400 || !result.Status {
		return "FAILED", fmt.Errorf("vendor rejected purchase: %s", result.Message)
	}
	var accepted struct {
		Reference string `json:"reference"`
	}
	if json.Unmarshal(result.Data, &accepted) != nil || accepted.Reference != reference {
		return "PROCESSING", fmt.Errorf("vendor response reference could not be verified")
	}
	return "SUCCEEDED", nil
}

// Provider requests reuse the stable client reference so a timeout can be
// retried without creating a second bundle. Orders remain PROCESSING when the
// provider outcome is ambiguous; funds are never reversed on a timeout.
func processPendingVTUOrders(db *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rows, err := db.Query(ctx, `SELECT o.id,o.user_id,o.plan_code,o.phone,o.amount_ngn,o.provider_reference,o.idempotency_key,o.network,o.allowance FROM vtu_data_orders o JOIN vtu_data_plans p ON p.code=o.plan_code AND p.network=o.network AND p.provider='configured' WHERE o.status='PROCESSING' AND o.attempts<5 AND o.updated_at<now()-interval '2 minutes' ORDER BY o.updated_at LIMIT 10`)
	if err != nil {
		return
	}
	type retry struct {
		id, user, code, phone, reference, key, network, allowance string
		amount                                                    int64
	}
	items := make([]retry, 0)
	for rows.Next() {
		var x retry
		if rows.Scan(&x.id, &x.user, &x.code, &x.phone, &x.amount, &x.reference, &x.key, &x.network, &x.allowance) == nil {
			items = append(items, x)
		}
	}
	rows.Close()
	for _, x := range items {
		tag, e := db.Exec(ctx, `UPDATE vtu_data_orders SET attempts=attempts+1,updated_at=now() WHERE id=$1 AND status='PROCESSING' AND updated_at<now()-interval '2 minutes'`, x.id)
		if e != nil || tag.RowsAffected() == 0 {
			continue
		}
		plan := vtuPlan{Code: x.code, Network: x.network, Allowance: x.allowance, Price: x.amount}
		status, e := vendVTU(ctx, plan, x.phone, x.reference)
		if e != nil {
			if status == "FAILED" {
				_ = reverseFailedVTU(ctx, db, x.id, x.user, x.key, x.amount, e.Error())
			}
			continue
		}
		_ = completeVTU(ctx, db, x.id, x.user, x.reference, plan)
	}
}
func completeVTU(ctx context.Context, db *pgxpool.Pool, id, userID, reference string, p vtuPlan) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM vtu_data_orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, userID).Scan(&status); err != nil {
		return err
	}
	if status == "SUCCEEDED" {
		return tx.Commit(ctx)
	}
	if status != "PROCESSING" {
		return fmt.Errorf("order is no longer processable")
	}
	if _, err = tx.Exec(ctx, `UPDATE vtu_data_orders SET status='SUCCEEDED',attempts=attempts+1,updated_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	var ledger string
	if err = tx.QueryRow(ctx, `SELECT l.id FROM ledger_entries l JOIN vtu_data_orders o ON l.idempotency_key='vtu:'||o.user_id::text||':'||o.idempotency_key WHERE o.id=$1`, id).Scan(&ledger); err != nil {
		return err
	}
	claims := map[string]any{"receipt_id": ledger, "order_id": id, "reference": reference, "user_id": userID, "type": "VTU_DATA", "network": p.Network, "allowance": p.Allowance, "amount_ngn": p.Price, "currency": "NGN", "issued_at": time.Now().UTC().Format(time.RFC3339)}
	if err = storeSignedReceipt(ctx, tx, userID, "VTU_DATA", ledger, claims); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url) VALUES($1,'PAYMENT','Data bundle delivered',$2,'/transactions')`, userID, p.Network+" data bundle delivered to your number.")
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	go sendPushNotificationToUser(db, userID, "Data bundle delivered", p.Network+" data bundle delivered to your number.", "/transactions")
	return nil
}
func reverseFailedVTU(ctx context.Context, db *pgxpool.Pool, id, userID, key string, amount int64, reason string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err = tx.QueryRow(ctx, `SELECT status FROM vtu_data_orders WHERE id=$1 FOR UPDATE`, id).Scan(&status); err != nil {
		return err
	}
	if status == "FAILED" {
		return tx.Commit(ctx)
	}
	if status != "PROCESSING" {
		return fmt.Errorf("order already settled")
	}
	if _, err = tx.Exec(ctx, `UPDATE wallet_accounts SET balance_ngn=balance_ngn+$1 WHERE user_id=$2`, amount, userID); err != nil {
		return err
	}
	if _, err = insertLedger(ctx, tx, userID, "vtu-reversal:"+userID+":"+key, amount, "VTU_REVERSAL"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE vtu_data_orders SET status='FAILED',attempts=attempts+1,updated_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	_, _ = tx.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) VALUES($1,'PAYMENT','Data purchase failed','Your wallet was refunded for the failed data purchase.','/transactions',jsonb_build_object('reason',$2))`, userID, reason)
	return tx.Commit(ctx)
}
func validVTUNetwork(v string) bool {
	return v == "MTN" || v == "AIRTEL" || v == "GLO" || v == "9MOBILE"
}
func validNigerianPhone(v string) bool {
	v = strings.TrimPrefix(v, "+")
	if strings.HasPrefix(v, "234") {
		v = "0" + strings.TrimPrefix(v, "234")
	}
	if len(v) != 11 || v[0] != '0' {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
