package main

import (
	"context"
	"strings"
	"time"

	"github.com/deride/ecosystem/backend/internal/push"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
)

func registerAccountRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Get("/transactions", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		category := strings.ToUpper(strings.TrimSpace(c.Query("category")))
		rows, err := db.Query(c.UserContext(), `SELECT l.id,l.entry_type,l.amount_ngn,l.created_at,r.id,r.signed_payload,r.payment_intent_id FROM ledger_entries l LEFT JOIN signed_receipts r ON r.ledger_entry_id=l.id WHERE l.user_id=$1 AND ($2='' OR l.entry_type=$2) ORDER BY l.created_at DESC LIMIT 100`, me.UserID, category)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_TRANSACTIONS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, entryType string
			var amount int64
			var createdAt time.Time
			var receiptID, signedPayload *string
			var paymentIntentID *string
			if err = rows.Scan(&id, &entryType, &amount, &createdAt, &receiptID, &signedPayload, &paymentIntentID); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "category": entryType, "amount_ngn": amount, "created_at": createdAt, "receipt_id": receiptID, "signed_receipt": signedPayload, "payment_intent_id": paymentIntentID})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"transactions": items})
	})
	secure.Get("/transactions/:id/receipt", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var id, kind, keyID, payload string
		var claims any
		var issued time.Time
		err := db.QueryRow(c.UserContext(), `SELECT id,receipt_type,key_id,claims,signed_payload,issued_at FROM signed_receipts WHERE id=$1 AND user_id=$2`, c.Params("id"), me.UserID).Scan(&id, &kind, &keyID, &claims, &payload, &issued)
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_RECEIPT_NOT_FOUND"))
		}
		return c.JSON(fiber.Map{"id": id, "type": kind, "key_id": keyID, "claims": claims, "signed_payload": payload, "issued_at": issued})
	})
	secure.Get("/notifications", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		unread := strings.EqualFold(c.Query("unread_only"), "true")
		rows, err := db.Query(c.UserContext(), `SELECT id,category,title,body,action_url,payload,read_at,created_at FROM app_notifications WHERE user_id=$1 AND ($2=false OR read_at IS NULL) ORDER BY created_at DESC LIMIT 100`, me.UserID, unread)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_NOTIFICATIONS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, category, title, body string
			var action *string
			var payload any
			var readAt *time.Time
			var created time.Time
			if err = rows.Scan(&id, &category, &title, &body, &action, &payload, &readAt, &created); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "category": category, "title": title, "body": body, "action_url": action, "payload": payload, "read_at": readAt, "created_at": created})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"notifications": items})
	})
	secure.Patch("/notifications/:id/read", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		result, err := db.Exec(c.UserContext(), `UPDATE app_notifications SET read_at=COALESCE(read_at,now()) WHERE id=$1 AND user_id=$2`, c.Params("id"), me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_NOTIFICATION_UPDATE_FAILED"))
		}
		if result.RowsAffected() == 0 {
			return c.Status(404).JSON(errJSON("ERR_NOTIFICATION_NOT_FOUND"))
		}
		return c.JSON(fiber.Map{"status": "READ"})
	})
	secure.Post("/notifications/read-all", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		tag, err := db.Exec(c.UserContext(), `UPDATE app_notifications SET read_at=now() WHERE user_id=$1 AND read_at IS NULL`, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_NOTIFICATION_UPDATE_FAILED"))
		}
		return c.JSON(fiber.Map{"marked_read": tag.RowsAffected()})
	})
	secure.Post("/push-subscriptions", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			Endpoint  string `json:"endpoint"`
			P256dh    string `json:"p256dh"`
			Auth      string `json:"auth"`
			UserAgent string `json:"user_agent"`
		}
		if err := c.BodyParser(&req); err != nil || !strings.HasPrefix(req.Endpoint, "https://") || req.P256dh == "" || req.Auth == "" || len(req.Endpoint) > 2048 || len(req.P256dh) > 256 || len(req.Auth) > 256 {
			return c.Status(400).JSON(errJSON("ERR_PUSH_SUBSCRIPTION_INVALID"))
		}
		var id string
		err := db.QueryRow(c.UserContext(), `INSERT INTO push_subscriptions(user_id,endpoint,p256dh,auth_secret,user_agent,last_used_at) VALUES($1,$2,$3,$4,$5,now()) ON CONFLICT(endpoint) DO UPDATE SET user_id=EXCLUDED.user_id,p256dh=EXCLUDED.p256dh,auth_secret=EXCLUDED.auth_secret,user_agent=EXCLUDED.user_agent,last_used_at=now() RETURNING id`, me.UserID, req.Endpoint, req.P256dh, req.Auth, req.UserAgent).Scan(&id)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_PUSH_SUBSCRIPTION_SAVE_FAILED"))
		}
		return c.Status(201).JSON(fiber.Map{"id": id, "status": "ACTIVE"})
	})
	secure.Delete("/push-subscriptions/:id", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		tag, err := db.Exec(c.UserContext(), `DELETE FROM push_subscriptions WHERE id=$1 AND user_id=$2`, c.Params("id"), me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_PUSH_SUBSCRIPTION_DELETE_FAILED"))
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(errJSON("ERR_PUSH_SUBSCRIPTION_NOT_FOUND"))
		}
		return c.SendStatus(204)
	})
}

func sendPushNotificationToUser(db *pgxpool.Pool, userID, title, body, actionURL string) {
	publicKey := os.Getenv("VAPID_PUBLIC_KEY")
	privateKey := os.Getenv("VAPID_PRIVATE_KEY_PEM")
	subject := os.Getenv("VAPID_SUBJECT")
	if publicKey == "" || privateKey == "" || subject == "" {
		return
	}
	rows, err := db.Query(context.Background(), `SELECT id,endpoint,p256dh,auth_secret FROM push_subscriptions WHERE user_id=$1`, userID)
	if err != nil {
		return
	}
	type sub struct{ id, endpoint, p256dh, auth string }
	items := make([]sub, 0)
	for rows.Next() {
		var s sub
		if rows.Scan(&s.id, &s.endpoint, &s.p256dh, &s.auth) == nil {
			items = append(items, s)
		}
	}
	rows.Close()
	for _, s := range items {
		status, err := push.Send(push.Subscription{Endpoint: s.endpoint, P256DH: s.p256dh, Auth: s.auth}, push.Message{Title: title, Body: body, URL: actionURL}, publicKey, privateKey, subject)
		if err != nil {
			continue
		}
		if status == 404 || status == 410 {
			_, _ = db.Exec(context.Background(), `DELETE FROM push_subscriptions WHERE id=$1`, s.id)
		} else if status >= 200 && status < 300 {
			_, _ = db.Exec(context.Background(), `UPDATE push_subscriptions SET last_used_at=now() WHERE id=$1`, s.id)
		}
	}
}

func notifyAdminsOfEscalation(db *pgxpool.Pool, issue string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.Query(ctx, `SELECT id FROM users WHERE active_role='ADMIN'`)
	if err != nil {
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
	for _, id := range ids {
		go sendPushNotificationToUser(db, id, "Urgent trip report", issue, "/admin/escalations")
	}
}

func notifyAdminsOfEvent(db *pgxpool.Pool, title, body, actionURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.Query(ctx, `SELECT id FROM users WHERE active_role='ADMIN'`)
	if err != nil {
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
	for _, id := range ids {
		_, _ = db.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url) VALUES($1,'SUPPORT',$2,$3,$4)`, id, title, body, actionURL)
		go sendPushNotificationToUser(db, id, title, body, actionURL)
	}
}
