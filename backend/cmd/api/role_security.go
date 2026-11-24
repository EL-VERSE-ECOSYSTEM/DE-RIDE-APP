package main

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

var requestableRoles = map[string]bool{"DRIVER": true, "DISPATCHER": true, "BIKER": true, "DISPATCH_RIDER": true, "PARK_OPERATOR": true}

func registerRoleSecurityRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Get("/role-requests", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		rows, err := db.Query(c.UserContext(), `SELECT id,requested_role,status,requested_at,reviewed_at,review_note FROM role_requests WHERE user_id=$1 ORDER BY requested_at DESC LIMIT 50`, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_ROLE_REQUESTS_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, role, status string
			var created time.Time
			var reviewed *time.Time
			var note *string
			if err = rows.Scan(&id, &role, &status, &created, &reviewed, &note); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "role": role, "status": status, "requested_at": created, "reviewed_at": reviewed, "review_note": note})
		}
		return c.JSON(fiber.Map{"requests": items})
	})
	secure.Post("/role-requests", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var in struct {
			Role string `json:"role"`
		}
		if c.BodyParser(&in) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		in.Role = strings.ToUpper(strings.TrimSpace(in.Role))
		if !requestableRoles[in.Role] {
			return c.Status(422).JSON(errJSON("ERR_ROLE_REQUEST_INVALID"))
		}
		if in.Role == "BIKER" || in.Role == "DISPATCH_RIDER" {
			return c.Status(422).JSON(errJSON("ERR_RIDER_ROLE_REQUIRES_KYC"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var granted bool
		if err = tx.QueryRow(c.UserContext(), `SELECT $2=ANY(roles) FROM users WHERE id=$1`, me.UserID, in.Role).Scan(&granted); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		if granted {
			return c.Status(409).JSON(errJSON("ERR_ROLE_ALREADY_GRANTED"))
		}
		var id string
		err = tx.QueryRow(c.UserContext(), `INSERT INTO role_requests(user_id,requested_role) VALUES($1,$2) RETURNING id`, me.UserID, in.Role).Scan(&id)
		if err != nil {
			return c.Status(409).JSON(errJSON("ERR_ROLE_REQUEST_ALREADY_PENDING"))
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO role_audit_log(user_id,actor_id,action,role,note) VALUES($1,$1,'REQUESTED',$2,'User requested an additional workspace role')`, me.UserID, in.Role); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_AUDIT_FAILED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_REQUEST_CREATE_FAILED"))
		}
		return c.Status(201).JSON(fiber.Map{"id": id, "role": in.Role, "status": "PENDING"})
	})
	secure.Post("/users/active-role", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var in struct {
			Role string `json:"role"`
		}
		if c.BodyParser(&in) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		in.Role = strings.ToUpper(strings.TrimSpace(in.Role))
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var roles []string
		var biker, dispatch bool
		if err = tx.QueryRow(c.UserContext(), `SELECT roles,biker_approved,dispatch_approved FROM users WHERE id=$1 FOR UPDATE`, me.UserID).Scan(&roles, &biker, &dispatch); err != nil {
			return c.Status(404).JSON(errJSON("ERR_USER_NOT_FOUND"))
		}
		granted := false
		for _, role := range roles {
			if role == in.Role {
				granted = true
				break
			}
		}
		if !granted {
			return c.Status(403).JSON(errJSON("ERR_ROLE_NOT_GRANTED"))
		}
		if in.Role == "BIKER" && !biker || in.Role == "DISPATCH_RIDER" && !dispatch {
			return c.Status(403).JSON(errJSON("ERR_ROLE_NOT_VERIFIED"))
		}
		if in.Role == "DISPATCHER" || in.Role == "PARK_OPERATOR" {
			var assigned bool
			if err = tx.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM trip_operator_assignments WHERE user_id=$1) OR EXISTS(SELECT 1 FROM park_operator_assignments WHERE user_id=$1)`, me.UserID).Scan(&assigned); err != nil || !assigned {
				return c.Status(403).JSON(errJSON("ERR_OPERATIONAL_ASSIGNMENT_REQUIRED"))
			}
		}
		if in.Role == "BIKER" || in.Role == "DISPATCH_RIDER" {
			var active int
			if err = tx.QueryRow(c.UserContext(), `SELECT (SELECT count(*) FROM ride_orders WHERE (passenger_id=$1 OR biker_id=$1) AND status IN ('ACCEPTED','IN_PROGRESS'))+(SELECT count(*) FROM parcel_orders WHERE (sender_id=$1 OR rider_id=$1) AND status IN ('ACCEPTED','IN_PROGRESS'))`, me.UserID).Scan(&active); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			if active > 0 {
				return c.Status(409).JSON(errJSON("ERR_ACTIVE_ORDER_MODE_LOCK"))
			}
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE users SET active_role=$2,updated_at=now() WHERE id=$1`, me.UserID, in.Role); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_SWITCH_FAILED"))
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE auth_refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, me.UserID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_SESSION_REVOKE"))
		}
		pair, err := createTokenPair(c.UserContext(), tx, me.UserID, in.Role)
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_TOKEN_ISSUE"))
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO role_audit_log(user_id,actor_id,action,role,note) VALUES($1,$1,'ACTIVATED',$2,'User changed active workspace role')`, me.UserID, in.Role); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_AUDIT_FAILED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_SWITCH_FAILED"))
		}
		pair["active_role"] = in.Role
		return c.JSON(pair)
	})
	secure.Get("/admin/role-requests", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT r.id,r.user_id,u.display_name,u.email,r.requested_role,r.requested_at FROM role_requests r JOIN users u ON u.id=r.user_id WHERE r.status='PENDING' ORDER BY r.requested_at LIMIT 100`)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_ROLE_QUEUE_UNAVAILABLE"))
		}
		defer rows.Close()
		items := make([]fiber.Map, 0)
		for rows.Next() {
			var id, user, name, email, role string
			var created time.Time
			if err = rows.Scan(&id, &user, &name, &email, &role, &created); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			items = append(items, fiber.Map{"id": id, "user_id": user, "display_name": name, "email": email, "role": role, "requested_at": created})
		}
		return c.JSON(fiber.Map{"requests": items})
	})
	secure.Post("/admin/role-requests/:requestID/review", func(c *fiber.Ctx) error {
		admin := c.Locals("identity").(identity)
		if admin.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var in struct {
			Decision string `json:"decision"`
			Note     string `json:"note"`
			ParkID   string `json:"park_id"`
			TripID   string `json:"trip_id"`
		}
		if c.BodyParser(&in) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		in.Decision = strings.ToUpper(strings.TrimSpace(in.Decision))
		in.Note = strings.TrimSpace(in.Note)
		if (in.Decision != "APPROVED" && in.Decision != "REJECTED") || in.Decision == "REJECTED" && len(in.Note) < 4 {
			return c.Status(422).JSON(errJSON("ERR_ROLE_REVIEW_INVALID"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var userID, role string
		if err = tx.QueryRow(c.UserContext(), `SELECT user_id,requested_role FROM role_requests WHERE id=$1 AND status='PENDING' FOR UPDATE`, c.Params("requestID")).Scan(&userID, &role); err != nil {
			return c.Status(404).JSON(errJSON("ERR_ROLE_REQUEST_NOT_PENDING"))
		}
		if in.Decision == "APPROVED" {
			switch role {
			case "DRIVER":
				var ok bool
				err = tx.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM vehicle_registrations WHERE owner_id=$1 AND verification_status='APPROVED' AND vehicle_type NOT IN ('BIKE','DISPATCH_BIKE'))`, userID).Scan(&ok)
				if err != nil || !ok {
					return c.Status(409).JSON(errJSON("ERR_APPROVED_VEHICLE_REQUIRED"))
				}
			case "BIKER":
				var ok bool
				err = tx.QueryRow(c.UserContext(), `SELECT biker_approved FROM users WHERE id=$1`, userID).Scan(&ok)
				if err != nil || !ok {
					return c.Status(409).JSON(errJSON("ERR_BIKER_KYC_REQUIRED"))
				}
			case "DISPATCH_RIDER":
				var ok bool
				err = tx.QueryRow(c.UserContext(), `SELECT dispatch_approved FROM users WHERE id=$1`, userID).Scan(&ok)
				if err != nil || !ok {
					return c.Status(409).JSON(errJSON("ERR_DISPATCH_KYC_INCOMPLETE"))
				}
			case "DISPATCHER":
				if in.TripID != "" {
					if _, err = tx.Exec(c.UserContext(), `INSERT INTO trip_operator_assignments(user_id,trip_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, in.TripID); err != nil {
						return c.Status(422).JSON(errJSON("ERR_ASSIGNMENT_INVALID"))
					}
				} else if in.ParkID != "" {
					if _, err = tx.Exec(c.UserContext(), `INSERT INTO park_operator_assignments(user_id,park_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, in.ParkID); err != nil {
						return c.Status(422).JSON(errJSON("ERR_ASSIGNMENT_INVALID"))
					}
				} else {
					return c.Status(422).JSON(errJSON("ERR_OPERATIONAL_SCOPE_REQUIRED"))
				}
			case "PARK_OPERATOR":
				if in.ParkID == "" {
					return c.Status(422).JSON(errJSON("ERR_OPERATIONAL_SCOPE_REQUIRED"))
				}
				if _, err = tx.Exec(c.UserContext(), `INSERT INTO park_operator_assignments(user_id,park_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, in.ParkID); err != nil {
					return c.Status(422).JSON(errJSON("ERR_ASSIGNMENT_INVALID"))
				}
			}
			if _, err = tx.Exec(c.UserContext(), `UPDATE users SET roles=CASE WHEN $2=ANY(roles) THEN roles ELSE array_append(roles,$2) END,updated_at=now() WHERE id=$1`, userID, role); err != nil {
				return c.Status(500).JSON(errJSON("ERR_ROLE_GRANT_FAILED"))
			}
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE role_requests SET status=$2,reviewed_by=$3,reviewed_at=now(),review_note=NULLIF($4,'') WHERE id=$1`, c.Params("requestID"), in.Decision, admin.UserID, in.Note); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_REVIEW_SAVE_FAILED"))
		}
		action := "REJECTED"
		if in.Decision == "APPROVED" {
			action = "GRANTED"
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO role_audit_log(user_id,actor_id,action,role,note) VALUES($1,$2,$3,$4,NULLIF($5,''))`, userID, admin.UserID, action, role, in.Note); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_AUDIT_FAILED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_REVIEW_SAVE_FAILED"))
		}
		return c.JSON(fiber.Map{"user_id": userID, "role": role, "status": in.Decision})
	})
	secure.Post("/admin/users/:userID/suspension", func(c *fiber.Ctx) error {
		admin := c.Locals("identity").(identity)
		if admin.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var in struct {
			Suspended bool   `json:"suspended"`
			Reason    string `json:"reason"`
		}
		if c.BodyParser(&in) != nil || len(strings.TrimSpace(in.Reason)) < 4 || c.Params("userID") == admin.UserID {
			return c.Status(422).JSON(errJSON("ERR_SUSPENSION_REQUEST_INVALID"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var role string
		if err = tx.QueryRow(c.UserContext(), `UPDATE users SET suspended_at=CASE WHEN $2 THEN COALESCE(suspended_at,now()) ELSE NULL END,updated_at=now() WHERE id=$1 RETURNING active_role`, c.Params("userID"), in.Suspended).Scan(&role); err != nil {
			return c.Status(404).JSON(errJSON("ERR_USER_NOT_FOUND"))
		}
		if in.Suspended {
			if _, err = tx.Exec(c.UserContext(), `UPDATE auth_refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, c.Params("userID")); err != nil {
				return c.Status(500).JSON(errJSON("ERR_SESSION_REVOKE"))
			}
		}
		action := "RESTORED"
		if in.Suspended {
			action = "SUSPENDED"
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO role_audit_log(user_id,actor_id,action,role,note) VALUES($1,$2,$3,$4,$5)`, c.Params("userID"), admin.UserID, action, role, strings.TrimSpace(in.Reason)); err != nil {
			return c.Status(500).JSON(errJSON("ERR_ROLE_AUDIT_FAILED"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"user_id": c.Params("userID"), "suspended": in.Suspended})
	})
}
