package main

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerSupportRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Get("/my-escalations", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		rows, err := db.Query(c.UserContext(), `SELECT id,trip_id,issue,status,created_at FROM escalations WHERE reporter_id=$1 ORDER BY created_at DESC LIMIT 100`, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_ESCALATIONS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, issue, status string
			var trip *string
			var created time.Time
			if err = rows.Scan(&id, &trip, &issue, &status, &created); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "trip_id": trip, "issue": issue, "status": status, "created_at": created})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"escalations": items})
	})
	secure.Get("/admin/escalations", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT e.id,e.reporter_id,u.display_name,e.trip_id,e.issue,e.status,e.created_at,e.frozen_at,e.preserved_chat_log FROM escalations e JOIN users u ON u.id=e.reporter_id WHERE e.status IN ('OPEN','IN_PROGRESS') ORDER BY e.created_at LIMIT 200`)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_ESCALATIONS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, reporter, name, issue, status string
			var trip *string
			var created time.Time
			var frozen *time.Time
			var preserved any
			if err = rows.Scan(&id, &reporter, &name, &trip, &issue, &status, &created, &frozen, &preserved); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "reporter_id": reporter, "reporter_name": name, "trip_id": trip, "issue": issue, "status": status, "created_at": created, "frozen_at": frozen, "preserved_chat_log": preserved})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"escalations": items})
	})
	secure.Post("/admin/escalations/:escalationID/resolve", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		tag, err := db.Exec(c.UserContext(), `UPDATE escalations SET status='RESOLVED' WHERE id=$1 AND status IN ('OPEN','IN_PROGRESS')`, c.Params("escalationID"))
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_ESCALATION_UPDATE_FAILED"))
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(errJSON("ERR_ESCALATION_NOT_OPEN"))
		}
		return c.JSON(fiber.Map{"status": "RESOLVED", "resolved_by": me.UserID})
	})
	secure.Get("/admin/support/cases", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT s.id,s.requester_id,u.display_name,s.subject,s.category,s.status,s.priority,s.created_at,s.updated_at FROM support_cases s JOIN users u ON u.id=s.requester_id WHERE s.status NOT IN ('RESOLVED','CLOSED') ORDER BY CASE s.priority WHEN 'URGENT' THEN 0 WHEN 'HIGH' THEN 1 ELSE 2 END,s.updated_at LIMIT 200`)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_SUPPORT_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, userID, name, subject, category, status, priority string
			var created, updated time.Time
			if err = rows.Scan(&id, &userID, &name, &subject, &category, &status, &priority, &created, &updated); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "requester_id": userID, "requester_name": name, "subject": subject, "category": category, "status": status, "priority": priority, "created_at": created, "updated_at": updated})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"cases": items})
	})
	secure.Get("/support/cases", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		rows, err := db.Query(c.UserContext(), `SELECT id,subject,category,status,priority,created_at,updated_at FROM support_cases WHERE requester_id=$1 ORDER BY updated_at DESC LIMIT 100`, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_SUPPORT_UNAVAILABLE"))
		}
		defer rows.Close()
		cases := make([]fiber.Map, 0)
		for rows.Next() {
			var id, subject, category, status, priority string
			var created, updated time.Time
			if err = rows.Scan(&id, &subject, &category, &status, &priority, &created, &updated); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			cases = append(cases, fiber.Map{"id": id, "subject": subject, "category": category, "status": status, "priority": priority, "created_at": created, "updated_at": updated})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"cases": cases})
	})
	secure.Post("/support/cases", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			Subject     string `json:"subject"`
			Category    string `json:"category"`
			TripID      string `json:"trip_id"`
			RideOrderID string `json:"ride_order_id"`
			Message     string `json:"message"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Subject = strings.TrimSpace(req.Subject)
		req.Category = strings.ToUpper(strings.TrimSpace(req.Category))
		req.Message = strings.TrimSpace(req.Message)
		allowed := map[string]bool{"ACCOUNT": true, "PAYMENT": true, "TRIP": true, "KYC": true, "SAFETY": true, "OTHER": true}
		if len(req.Subject) < 4 || len(req.Subject) > 180 || !allowed[req.Category] || len(req.Message) < 1 || len(req.Message) > 5000 {
			return c.Status(422).JSON(errJSON("ERR_SUPPORT_CASE_INVALID"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var caseID string
		err = tx.QueryRow(c.UserContext(), `INSERT INTO support_cases(requester_id,trip_id,ride_order_id,subject,category) VALUES($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,$4,$5) RETURNING id`, me.UserID, req.TripID, req.RideOrderID, req.Subject, req.Category).Scan(&caseID)
		if err != nil {
			return c.Status(422).JSON(errJSON("ERR_SUPPORT_CASE_CREATE_FAILED"))
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO support_messages(case_id,sender_id,body) VALUES($1,$2,$3)`, caseID, me.UserID, req.Message); err != nil {
			return c.Status(500).JSON(errJSON("ERR_SUPPORT_MESSAGE_CREATE_FAILED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_SUPPORT_COMMIT_FAILED"))
		}
		go notifyAdminsOfEvent(db, "New customer support case", req.Subject, "/admin/support")
		return c.Status(201).JSON(fiber.Map{"id": caseID, "status": "OPEN"})
	})
	secure.Get("/support/cases/:caseID/messages", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		caseID := c.Params("caseID")
		var role string
		if err := db.QueryRow(c.UserContext(), `SELECT active_role FROM users WHERE id=$1`, me.UserID).Scan(&role); err != nil {
			return c.Status(401).JSON(errJSON("ERR_AUTH_REQUIRED"))
		}
		admin := role == "ADMIN"
		var canRead bool
		if err := db.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM support_cases WHERE id=$1 AND (requester_id=$2 OR $3))`, caseID, me.UserID, admin).Scan(&canRead); err != nil || !canRead {
			return c.Status(404).JSON(errJSON("ERR_SUPPORT_CASE_NOT_FOUND"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT m.id,m.sender_id,m.body,m.attachment_media_id,m.created_at FROM support_messages m JOIN support_cases s ON s.id=m.case_id WHERE s.id=$1 AND (s.requester_id=$2 OR $3) AND ($3 OR NOT m.internal_note) ORDER BY m.created_at ASC LIMIT 500`, caseID, me.UserID, admin)
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_SUPPORT_CASE_NOT_FOUND"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, sender, body string
			var media *string
			var created time.Time
			if err = rows.Scan(&id, &sender, &body, &media, &created); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "sender_id": sender, "body": body, "attachment_media_id": media, "created_at": created})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"messages": items})
	})
	secure.Post("/support/cases/:caseID/messages", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			Body         string `json:"body"`
			AttachmentID string `json:"attachment_media_id"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Body = strings.TrimSpace(req.Body)
		if len(req.Body) < 1 || len(req.Body) > 5000 {
			return c.Status(422).JSON(errJSON("ERR_SUPPORT_MESSAGE_INVALID"))
		}
		var id string
		err := db.QueryRow(c.UserContext(), `INSERT INTO support_messages(case_id,sender_id,body,attachment_media_id) SELECT s.id,$2,$3,NULLIF($4,'')::uuid FROM support_cases s WHERE s.id=$1 AND s.requester_id=$2 AND s.status NOT IN ('RESOLVED','CLOSED') AND ($4='' OR EXISTS(SELECT 1 FROM media_assets a WHERE a.id=NULLIF($4,'')::uuid AND a.user_id=$2 AND a.asset_category='SUPPORT_ATTACHMENT' AND a.upload_status='UPLOADED' AND a.deletion_status='ACTIVE')) RETURNING id`, c.Params("caseID"), me.UserID, req.Body, req.AttachmentID).Scan(&id)
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_SUPPORT_CASE_NOT_WRITABLE"))
		}
		_, _ = db.Exec(c.UserContext(), `UPDATE support_cases SET status='OPEN',updated_at=now() WHERE id=$1`, c.Params("caseID"))
		go notifyAdminsOfEvent(db, "Customer replied to support", "A customer sent a new message to a support case.", "/admin/support")
		return c.Status(201).JSON(fiber.Map{"id": id, "status": "SENT"})
	})
	secure.Post("/admin/support/cases/:caseID/messages", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var req struct {
			Body     string `json:"body"`
			Internal bool   `json:"internal_note"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Body = strings.TrimSpace(req.Body)
		if len(req.Body) < 1 || len(req.Body) > 5000 {
			return c.Status(422).JSON(errJSON("ERR_SUPPORT_MESSAGE_INVALID"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var ownerID string
		if err = tx.QueryRow(c.UserContext(), `SELECT requester_id FROM support_cases WHERE id=$1 AND status NOT IN ('RESOLVED','CLOSED') FOR UPDATE`, c.Params("caseID")).Scan(&ownerID); err != nil {
			return c.Status(404).JSON(errJSON("ERR_SUPPORT_CASE_NOT_FOUND"))
		}
		var id string
		if err = tx.QueryRow(c.UserContext(), `INSERT INTO support_messages(case_id,sender_id,body,internal_note) VALUES($1,$2,$3,$4) RETURNING id`, c.Params("caseID"), me.UserID, req.Body, req.Internal).Scan(&id); err != nil {
			return c.Status(500).JSON(errJSON("ERR_SUPPORT_MESSAGE_CREATE_FAILED"))
		}
		if !req.Internal {
			_, err = tx.Exec(c.UserContext(), `UPDATE support_cases SET status='WAITING_FOR_USER',assigned_admin_id=$1,updated_at=now() WHERE id=$2`, me.UserID, c.Params("caseID"))
			if err != nil {
				return c.Status(500).JSON(errJSON("ERR_SUPPORT_CASE_UPDATE_FAILED"))
			}
			_, _ = tx.Exec(c.UserContext(), `INSERT INTO app_notifications(user_id,category,title,body,action_url) VALUES($1,'SUPPORT','Support replied',$2,'/support')`, ownerID, "A support agent replied to your case.")
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_SUPPORT_COMMIT_FAILED"))
		}
		if !req.Internal {
			go sendPushNotificationToUser(db, ownerID, "Support replied", "A support agent replied to your case.", "/support")
		}
		return c.Status(201).JSON(fiber.Map{"id": id, "status": "SENT"})
	})
	secure.Patch("/admin/support/cases/:caseID", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var req struct {
			Status   string `json:"status"`
			Priority string `json:"priority"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Status = strings.ToUpper(strings.TrimSpace(req.Status))
		req.Priority = strings.ToUpper(strings.TrimSpace(req.Priority))
		validStatus := map[string]bool{"OPEN": true, "IN_PROGRESS": true, "WAITING_FOR_USER": true, "RESOLVED": true, "CLOSED": true}
		validPriority := map[string]bool{"LOW": true, "NORMAL": true, "HIGH": true, "URGENT": true}
		if !validStatus[req.Status] || !validPriority[req.Priority] {
			return c.Status(422).JSON(errJSON("ERR_SUPPORT_CASE_UPDATE_INVALID"))
		}
		tag, err := db.Exec(c.UserContext(), `UPDATE support_cases SET status=$1,priority=$2,assigned_admin_id=$3,updated_at=now(),resolved_at=CASE WHEN $1 IN ('RESOLVED','CLOSED') THEN now() ELSE NULL END WHERE id=$4`, req.Status, req.Priority, me.UserID, c.Params("caseID"))
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_SUPPORT_CASE_UPDATE_FAILED"))
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(errJSON("ERR_SUPPORT_CASE_NOT_FOUND"))
		}
		return c.JSON(fiber.Map{"status": req.Status, "priority": req.Priority})
	})
	secure.Post("/disputes", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			EscalationID    string `json:"escalation_id"`
			PaymentIntentID string `json:"payment_intent_id"`
			Reason          string `json:"reason"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Reason = strings.TrimSpace(req.Reason)
		if len(req.Reason) < 10 || len(req.Reason) > 3000 || (req.EscalationID == "" && req.PaymentIntentID == "") {
			return c.Status(422).JSON(errJSON("ERR_DISPUTE_INVALID"))
		}
		var id string
		err := db.QueryRow(c.UserContext(), `INSERT INTO disputes(escalation_id,payment_intent_id,opened_by,reason) SELECT NULLIF($1,'')::uuid,NULLIF($2,'')::uuid,$3,$4 WHERE ($1='' OR EXISTS(SELECT 1 FROM escalations WHERE id=NULLIF($1,'')::uuid AND reporter_id=$3)) AND ($2='' OR EXISTS(SELECT 1 FROM payment_intents WHERE id=NULLIF($2,'')::uuid AND user_id=$3)) RETURNING id`, req.EscalationID, req.PaymentIntentID, me.UserID, req.Reason).Scan(&id)
		if err != nil {
			return c.Status(422).JSON(errJSON("ERR_DISPUTE_REFERENCE_INVALID"))
		}
		return c.Status(201).JSON(fiber.Map{"id": id, "status": "OPEN"})
	})
	secure.Get("/admin/disputes", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT d.id,d.opened_by,u.display_name,d.payment_intent_id,d.escalation_id,d.reason,d.status,d.funds_held,d.created_at FROM disputes d JOIN users u ON u.id=d.opened_by WHERE d.status IN ('OPEN','UNDER_REVIEW') ORDER BY d.created_at LIMIT 200`)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DISPUTES_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, owner, name, reason, status string
			var pi, esc *string
			var held bool
			var created time.Time
			if err = rows.Scan(&id, &owner, &name, &pi, &esc, &reason, &status, &held, &created); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "opened_by": owner, "display_name": name, "payment_intent_id": pi, "escalation_id": esc, "reason": reason, "status": status, "funds_held": held, "created_at": created})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"disputes": items})
	})
	secure.Post("/admin/disputes/:disputeID/resolve", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var req struct {
			Decision   string `json:"decision"`
			Resolution string `json:"resolution"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Decision = strings.ToUpper(strings.TrimSpace(req.Decision))
		req.Resolution = strings.TrimSpace(req.Resolution)
		if (req.Decision != "RESOLVED" && req.Decision != "REJECTED") || len(req.Resolution) < 5 || len(req.Resolution) > 3000 {
			return c.Status(422).JSON(errJSON("ERR_DISPUTE_DECISION_INVALID"))
		}
		tag, err := db.Exec(c.UserContext(), `UPDATE disputes SET status=$1,resolution=$2,assigned_admin_id=$3,resolved_at=now() WHERE id=$4 AND status IN ('OPEN','UNDER_REVIEW')`, req.Decision, req.Resolution, me.UserID, c.Params("disputeID"))
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DISPUTE_UPDATE_FAILED"))
		}
		if tag.RowsAffected() == 0 {
			return c.Status(404).JSON(errJSON("ERR_DISPUTE_NOT_OPEN"))
		}
		return c.JSON(fiber.Map{"status": req.Decision})
	})
}
