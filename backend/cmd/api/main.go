package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/deride/ecosystem/backend/internal/cache"
	"github.com/deride/ecosystem/backend/internal/realtime"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type identity struct {
	UserID string `json:"sub"`
	Role   string `json:"role"`
}

func main() {
	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("database config: %v", err)
	}
	poolCfg.MaxConns = envInt32("DB_MAX_CONNS", 25)
	poolCfg.MinConns = envInt32("DB_MIN_CONNS", 2)
	db, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Fatalf("connect to Postgres: %v", err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		log.Fatalf("ping Postgres: %v", err)
	}
	store := cache.NewMemoryStore(30 * time.Second)
	defer store.Close()
	hub := realtime.NewHub()
	app := fiber.New(fiber.Config{AppName: "DE RIDE API", ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, BodyLimit: 6 * 1024 * 1024})
	origins := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS"))
	if origins == "" {
		origins = "http://localhost:3000"
	}
	app.Use(cors.New(cors.Config{AllowOrigins: origins, AllowMethods: "GET,POST,PATCH,PUT,DELETE,OPTIONS", AllowHeaders: "Origin,Content-Type,Accept,Authorization,Idempotency-Key", MaxAge: 600}))
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "same-origin")
		return c.Next()
	})
	app.Get("/healthz", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok", "service": "deride-api"}) })
	go purgeExpiredTripMessages(db)
	v1 := app.Group("/api/v1")
	registerAuthRoutes(v1, db)
	go processExpiredMedia(db)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			processExpiredMedia(db)
		}
	}()
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			processPendingPaystackEvents(db)
		}
	}()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			processPendingVTUOrders(db)
		}
	}()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			processExpiredTicketCheckouts(db)
		}
	}()
	v1.Get("/health", func(c *fiber.Ctx) error {
		probe, done := context.WithTimeout(c.UserContext(), 2*time.Second)
		defer done()
		if err := db.Ping(probe); err != nil {
			return c.Status(503).JSON(fiber.Map{"status": "degraded", "database": "unavailable"})
		}
		return c.JSON(fiber.Map{"status": "ok", "database": "connected"})
	})
	v1.Get("/parks", func(c *fiber.Ctx) error {
		state := normalizeStateCode(c.Query("state"))
		if state == "!" {
			return c.Status(422).JSON(errJSON("ERR_STATE_INVALID"))
		}
		var lat, lng *float64
		if c.Query("lat") != "" || c.Query("lng") != "" {
			la, e1 := strconv.ParseFloat(c.Query("lat"), 64)
			lo, e2 := strconv.ParseFloat(c.Query("lng"), 64)
			if e1 != nil || e2 != nil || la < -90 || la > 90 || lo < -180 || lo > 180 {
				return c.Status(422).JSON(errJSON("ERR_COORDINATES_INVALID"))
			}
			lat, lng = &la, &lo
		}
		radius, err := strconv.Atoi(c.Query("radius_m"))
		if c.Query("radius_m") == "" {
			radius = 25000
		}
		if (err != nil && c.Query("radius_m") != "") || radius < 100 || radius > 100000 {
			return c.Status(422).JSON(errJSON("ERR_RADIUS_INVALID"))
		}
		rows, err := db.Query(c.UserContext(), `SELECT id,name,state_code,launch_enabled,ST_Y(location::geometry),ST_X(location::geometry) FROM bus_parks WHERE ($1='' OR state_code=$1) AND ($2::double precision IS NULL OR ST_DWithin(location,ST_SetSRID(ST_MakePoint($3,$2),4326)::geography,$4)) ORDER BY launch_enabled DESC,CASE WHEN $2::double precision IS NULL THEN 0 ELSE ST_Distance(location,ST_SetSRID(ST_MakePoint($3,$2),4326)::geography) END,name LIMIT 100`, state, lat, lng, radius)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer rows.Close()
		parks := make([]fiber.Map, 0)
		for rows.Next() {
			var id, name, code string
			var launch bool
			var lat, lon *float64
			if err = rows.Scan(&id, &name, &code, &launch, &lat, &lon); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			parks = append(parks, fiber.Map{"id": id, "name": name, "state_code": strings.TrimSpace(code), "launch_enabled": launch, "latitude": lat, "longitude": lon})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"parks": parks})
	})
	v1.Get("/parks/summary", func(c *fiber.Ctx) error {
		var active, nationwide, boarding, onlineRiders int64
		var waitMinutes *float64
		err := db.QueryRow(c.UserContext(), `SELECT
			(SELECT count(*) FROM bus_parks WHERE launch_enabled),
			(SELECT count(*) FROM bus_parks),
			(SELECT count(*) FROM trips WHERE status='OPEN' AND departure_at >= now()),
			(SELECT count(DISTINCT biker_id) FROM ride_orders WHERE status IN ('ACCEPTED','IN_PROGRESS') AND biker_id IS NOT NULL),
			(SELECT avg(greatest(0,extract(epoch FROM (departure_at-now()))/60)) FROM trips WHERE status='OPEN' AND departure_at >= now())`).Scan(&active, &nationwide, &boarding, &onlineRiders, &waitMinutes)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		return c.JSON(fiber.Map{"active_parks": active, "nationwide_parks": nationwide, "verified_buses": boarding, "online_riders": onlineRiders, "average_wait_saved": waitMinutes})
	})
	secure := v1.Group("", authenticate(os.Getenv("JWT_ACCESS_SECRET"), db))
	registerMediaRoutes(secure, db)
	registerPaymentRoutes(v1, secure, db)
	registerFinanceFlowRoutes(secure, db)
	registerAccountRoutes(secure, db)
	registerKYCRoutes(secure, db)
	registerSupportRoutes(secure, db)
	registerVTURoutes(secure, db)
	registerAmeboRoutes(secure, db)
	registerRiderOrderRoutes(secure, db)
	registerTicketScanRoutes(v1, secure, db)
	registerTripRoutes(secure, db)
	registerTripLifecycleRoutes(secure, db)
	registerGPSTelemetryRoutes(secure, db)
	registerRoleSecurityRoutes(secure, db)
	secure.Get("/me", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		_, _ = db.Exec(c.UserContext(), `INSERT INTO wallet_accounts(user_id,balance_ngn) VALUES($1,0) ON CONFLICT(user_id) DO NOTHING`, me.UserID)
		grant := ameboDailyGrant(c.UserContext(), db, me.UserID)
		_, _ = db.Exec(c.UserContext(), `INSERT INTO amebo_wallets(user_id,free_tokens,free_expires_at,purchased_tokens) VALUES($1,$2,(date_trunc('day',now() AT TIME ZONE 'Africa/Lagos')+interval '1 day') AT TIME ZONE 'Africa/Lagos',0) ON CONFLICT(user_id) DO NOTHING`, me.UserID, grant)
		_, _ = db.Exec(c.UserContext(), `UPDATE amebo_wallets SET free_tokens=$2,free_expires_at=(date_trunc('day',now() AT TIME ZONE 'Africa/Lagos')+interval '1 day') AT TIME ZONE 'Africa/Lagos' WHERE user_id=$1 AND free_expires_at<=now()`, me.UserID, grant)
		var id, email, displayName, role string
		var roles []string
		var emailVerified, bikerApproved, dispatchApproved, gpsOptIn bool
		var createdAt time.Time
		var balance int64
		var freeTokens, purchasedTokens int
		var freeExpiresAt *time.Time
		err := db.QueryRow(c.UserContext(), `SELECT u.id,u.email,u.display_name,u.active_role,u.roles,u.email_verified,u.biker_approved,u.dispatch_approved,u.gps_opt_in,u.created_at,COALESCE(w.balance_ngn,0),COALESCE(a.free_tokens,0),COALESCE(a.purchased_tokens,0),a.free_expires_at FROM users u LEFT JOIN wallet_accounts w ON w.user_id=u.id LEFT JOIN amebo_wallets a ON a.user_id=u.id WHERE u.id=$1`, me.UserID).Scan(&id, &email, &displayName, &role, &roles, &emailVerified, &bikerApproved, &dispatchApproved, &gpsOptIn, &createdAt, &balance, &freeTokens, &purchasedTokens, &freeExpiresAt)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_PROFILE_UNAVAILABLE"))
		}
		return c.JSON(fiber.Map{"id": id, "email": email, "display_name": displayName, "active_role": role, "roles": roles, "email_verified": emailVerified, "biker_approved": bikerApproved, "dispatch_approved": dispatchApproved, "gps_opt_in": gpsOptIn, "created_at": createdAt, "wallet_balance_ngn": balance, "amebo_free_tokens": freeTokens, "amebo_purchased_tokens": purchasedTokens, "amebo_free_expires_at": freeExpiresAt})
	})
	secure.Patch("/me/preferences", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			GPSOptIn *bool  `json:"gps_opt_in"`
			Language string `json:"language_pref"`
		}
		if c.BodyParser(&req) != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		req.Language = strings.ToUpper(strings.TrimSpace(req.Language))
		if req.Language != "" && !map[string]bool{"EN": true, "YO": true, "HA": true, "IG": true, "PCM": true}[req.Language] {
			return c.Status(422).JSON(errJSON("ERR_LANGUAGE_INVALID"))
		}
		tag, err := db.Exec(c.UserContext(), `UPDATE users SET gps_opt_in=COALESCE($2,gps_opt_in),language_pref=COALESCE(NULLIF($3,''),language_pref),updated_at=now() WHERE id=$1`, me.UserID, req.GPSOptIn, req.Language)
		if err != nil || tag.RowsAffected() == 0 {
			return c.Status(503).JSON(errJSON("ERR_PREFERENCES_UPDATE_FAILED"))
		}
		return c.JSON(fiber.Map{"status": "UPDATED", "gps_opt_in": req.GPSOptIn, "language_pref": req.Language})
	})
	secure.Get("/wallet/transactions", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		rows, err := db.Query(c.UserContext(), `SELECT id,entry_type,amount_ngn,created_at FROM ledger_entries WHERE user_id=$1 ORDER BY created_at DESC LIMIT 50`, me.UserID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_WALLET_UNAVAILABLE"))
		}
		defer rows.Close()
		entries := make([]fiber.Map, 0)
		for rows.Next() {
			var id, entryType string
			var amount int64
			var createdAt time.Time
			if err = rows.Scan(&id, &entryType, &amount, &createdAt); err != nil {
				return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
			}
			entries = append(entries, fiber.Map{"id": id, "type": entryType, "amount_ngn": amount, "created_at": createdAt})
		}
		if err = rows.Err(); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(fiber.Map{"transactions": entries})
	})
	secure.Post("/ws-ticket", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		ticket := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": me.UserID, "role": me.Role, "token_type": "websocket", "jti": uuid.NewString(), "exp": time.Now().Add(30 * time.Second).Unix()})
		signed, err := ticket.SignedString([]byte(os.Getenv("JWT_ACCESS_SECRET")))
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_WS_TICKET_ISSUE"))
		}
		return c.JSON(fiber.Map{"ticket": signed, "expires_in_seconds": 30})
	})
	secure.Get("/ws", websocket.New(func(conn *websocket.Conn) {
		me, ok := conn.Locals("identity").(identity)
		if !ok {
			_ = conn.WriteJSON(fiber.Map{"error": "ERR_AUTH_REQUIRED"})
			return
		}
		client := &realtime.Client{Conn: conn.Conn}
		if me.Role == "ADMIN" {
			hub.Join("admin:sos", client)
			hub.Join("admin:live", client)
		}
		defer hub.Leave(client)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var event struct {
				Type         string  `json:"event"`
				TripID       string  `json:"trip_id"`
				Content      string  `json:"content"`
				AttachmentID string  `json:"attachment_media_id"`
				Category     string  `json:"category"`
				Description  string  `json:"description"`
				Latitude     float64 `json:"lat"`
				Longitude    float64 `json:"lng"`
				Speed        float64 `json:"speed"`
				Source       string  `json:"source"`
			}
			if json.Unmarshal(raw, &event) != nil || event.TripID == "" {
				_ = client.Send(fiber.Map{"event": "error", "code": "ERR_INVALID_EVENT"})
				continue
			}
			ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
			switch event.Type {
			case "gps:telemetry_push":
				if err = storeUserTelemetry(ctx, db, me, event.TripID, event.Latitude, event.Longitude, event.Speed, event.Source); err != nil {
					_ = client.Send(fiber.Map{"event": "error", "code": err.Error()})
				} else {
					update := fiber.Map{"event": "gps:telemetry", "trip_id": event.TripID, "lat": event.Latitude, "lng": event.Longitude, "speed": event.Speed, "source": strings.ToUpper(event.Source), "updated_at": time.Now().UTC()}
					hub.Broadcast("trip:"+event.TripID, update)
					hub.Broadcast("admin:live", update)
				}
			case "room:join":
				var allowed bool
				err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM trips t WHERE t.id=$1 AND (t.driver_id=$2 OR EXISTS(SELECT 1 FROM tickets k WHERE k.trip_id=t.id AND k.passenger_id=$2) OR EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=t.id AND a.user_id=$2) OR EXISTS(SELECT 1 FROM park_operator_assignments a WHERE a.park_id=t.park_id AND a.user_id=$2) OR ($3='ADMIN' AND EXISTS(SELECT 1 FROM escalations e WHERE e.trip_id=t.id AND e.status IN ('OPEN','IN_PROGRESS')))))`, event.TripID, me.UserID, me.Role).Scan(&allowed)
				if err == nil && allowed {
					hub.Join("trip:"+event.TripID, client)
					err = client.Send(fiber.Map{"event": "room:joined", "trip_id": event.TripID})
				} else {
					err = client.Send(fiber.Map{"event": "error", "code": "ERR_TRIP_ACCESS_DENIED"})
				}
			case "chat:send_message":
				if (len(event.Content) == 0 && event.AttachmentID == "") || len(event.Content) > 2000 {
					err = client.Send(fiber.Map{"event": "error", "code": "ERR_INVALID_MESSAGE"})
					break
				}
				var messageID string
				err = db.QueryRow(ctx, `INSERT INTO messages(trip_id,sender_id,body,attachment_media_id) SELECT t.id,$2,$3,NULLIF($4,'')::uuid FROM trips t WHERE t.id=$1 AND (t.driver_id=$2 OR EXISTS(SELECT 1 FROM tickets k WHERE k.trip_id=t.id AND k.passenger_id=$2) OR EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=t.id AND a.user_id=$2) OR EXISTS(SELECT 1 FROM park_operator_assignments a WHERE a.park_id=t.park_id AND a.user_id=$2) OR ($5='ADMIN' AND EXISTS(SELECT 1 FROM escalations e WHERE e.trip_id=t.id AND e.status IN ('OPEN','IN_PROGRESS')))) AND ($4='' OR EXISTS(SELECT 1 FROM media_assets a WHERE a.id=NULLIF($4,'')::uuid AND a.user_id=$2 AND a.asset_category='CHAT_ATTACHMENT' AND a.upload_status='UPLOADED' AND a.deletion_status='ACTIVE')) AND NOT(t.status IN ('ARRIVED','CANCELLED') AND t.arrived_at IS NOT NULL AND now()>t.arrived_at+interval '6 minutes') RETURNING id`, event.TripID, me.UserID, event.Content, event.AttachmentID, me.Role).Scan(&messageID)
				if err == nil {
					hub.Broadcast("trip:"+event.TripID, fiber.Map{"event": "chat:message", "id": messageID, "trip_id": event.TripID, "sender_id": me.UserID, "content": event.Content, "attachment_media_id": event.AttachmentID, "created_at": time.Now().UTC()})
				} else {
					err = client.Send(fiber.Map{"event": "error", "code": "ERR_CHAT_UNAVAILABLE"})
				}
			case "escalation:create":
				issue := strings.TrimSpace(event.Category + ": " + event.Description)
				if len(issue) < 4 || len(issue) > 2000 {
					err = client.Send(fiber.Map{"event": "error", "code": "ERR_INVALID_ESCALATION"})
					break
				}
				var escalationID string
				if math.IsNaN(event.Latitude) || math.IsNaN(event.Longitude) || event.Latitude < -90 || event.Latitude > 90 || event.Longitude < -180 || event.Longitude > 180 {
					event.Latitude = 0
					event.Longitude = 0
				}
				err = db.QueryRow(ctx, `INSERT INTO escalations(reporter_id,trip_id,issue,status,gps_snapshot,preserved_chat_log) SELECT $2,t.id,$3,'OPEN',CASE WHEN $4::double precision=0 AND $5::double precision=0 THEN NULL ELSE ST_SetSRID(ST_MakePoint($5,$4),4326)::geography END,COALESCE((SELECT jsonb_agg(jsonb_build_object('sender_id',m.sender_id,'body',m.body,'created_at',m.created_at) ORDER BY m.created_at) FROM (SELECT sender_id,body,created_at FROM messages WHERE trip_id=t.id ORDER BY created_at DESC LIMIT 20) m),'[]'::jsonb) FROM trips t WHERE t.id=$1 AND (t.driver_id=$2 OR EXISTS(SELECT 1 FROM tickets k WHERE k.trip_id=t.id AND k.passenger_id=$2)) RETURNING id`, event.TripID, me.UserID, issue, event.Latitude, event.Longitude).Scan(&escalationID)
				if err == nil {
					alert := fiber.Map{"event": "escalation:urgent_alert", "escalation_id": escalationID, "trip_id": event.TripID, "reporter_id": me.UserID, "issue": issue}
					hub.Broadcast("admin:sos", alert)
					hub.Broadcast("trip:"+event.TripID, alert)
					_, _ = db.Exec(ctx, `INSERT INTO app_notifications(user_id,category,title,body,action_url,payload) SELECT id,'SAFETY','Urgent trip report',$1,'/admin/escalations',jsonb_build_object('escalation_id',$2,'trip_id',$3) FROM users WHERE active_role='ADMIN'`, issue, escalationID, event.TripID)
					go notifyAdminsOfEscalation(db, issue)
				} else {
					err = client.Send(fiber.Map{"event": "error", "code": "ERR_ESCALATION_UNAVAILABLE"})
				}
			default:
				err = client.Send(fiber.Map{"event": "error", "code": "ERR_UNKNOWN_EVENT"})
			}
			done()
			if err != nil {
				return
			}
		}
	}, websocket.Config{Origins: strings.Split(origins, ",")}))
	secure.Post("/users/switch-rider-mode", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var in struct {
			Mode string `json:"mode"`
		}
		if err := c.BodyParser(&in); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		if in.Mode != "BIKER" && in.Mode != "DISPATCH_RIDER" {
			return c.Status(400).JSON(errJSON("ERR_INVALID_MODE"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var biker, dispatch bool
		var roles []string
		var active int
		err = tx.QueryRow(c.UserContext(), `SELECT biker_approved,dispatch_approved,roles FROM users WHERE id=$1 FOR UPDATE`, me.UserID).Scan(&biker, &dispatch, &roles)
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_USER_NOT_FOUND"))
		}
		if in.Mode == "BIKER" && !biker || in.Mode == "DISPATCH_RIDER" && !dispatch {
			return c.Status(403).JSON(errJSON("ERR_RIDER_MODE_NOT_APPROVED"))
		}
		granted := false
		for _, r := range roles {
			if r == in.Mode {
				granted = true
				break
			}
		}
		if !granted {
			return c.Status(403).JSON(errJSON("ERR_ROLE_NOT_GRANTED"))
		}
		if err = tx.QueryRow(c.UserContext(), `SELECT (SELECT count(*) FROM ride_orders WHERE biker_id=$1 AND status IN ('ACCEPTED','IN_PROGRESS'))+(SELECT count(*) FROM parcel_orders WHERE rider_id=$1 AND status IN ('ACCEPTED','IN_PROGRESS'))`, me.UserID).Scan(&active); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		if active > 0 {
			return c.Status(409).JSON(errJSON("ERR_ACTIVE_RIDER_JOB"))
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE users SET active_role=$2 WHERE id=$1`, me.UserID, in.Mode); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE auth_refresh_tokens SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, me.UserID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_SESSION_REVOKE"))
		}
		pair, err := createTokenPair(c.UserContext(), tx, me.UserID, in.Mode)
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_TOKEN_ISSUE"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		pair["active_role"] = in.Mode
		return c.JSON(pair)
	})
	secure.Post("/trips/:id/capacity", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		if me.Role != "DRIVER" && me.Role != "DISPATCHER" && me.Role != "PARK_OPERATOR" && me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_ROLE_FORBIDDEN"))
		}
		var in struct {
			Capacity    int       `json:"capacity"`
			DepartureAt time.Time `json:"departure_at"`
		}
		if err := c.BodyParser(&in); err != nil || in.Capacity < 0 || in.DepartureAt.IsZero() {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		key := c.Params("id")
		res, err := db.Exec(c.UserContext(), `UPDATE trips SET capacity=$1,departure_at=$2,last_capacity_update_at=now() WHERE id=$3 AND ($4='ADMIN' OR driver_id=$5 OR ($4='DISPATCHER' AND EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=trips.id AND a.user_id=$5)) OR ($4='PARK_OPERATOR' AND EXISTS(SELECT 1 FROM park_operator_assignments a WHERE a.park_id=trips.park_id AND a.user_id=$5))) AND (last_capacity_update_at IS NULL OR last_capacity_update_at <= now()-interval '120 seconds')`, in.Capacity, in.DepartureAt, key, me.Role, me.UserID)
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		if res.RowsAffected() == 0 {
			var last *time.Time
			var exists bool
			err = db.QueryRow(c.UserContext(), `SELECT last_capacity_update_at FROM trips WHERE id=$1 AND ($2='ADMIN' OR driver_id=$3 OR ($2='DISPATCHER' AND EXISTS(SELECT 1 FROM trip_operator_assignments a WHERE a.trip_id=trips.id AND a.user_id=$3)) OR ($2='PARK_OPERATOR' AND EXISTS(SELECT 1 FROM park_operator_assignments a WHERE a.park_id=trips.park_id AND a.user_id=$3)))`, key, me.Role, me.UserID).Scan(&last)
			exists = err == nil
			if exists && last != nil {
				remaining := int(time.Until(last.Add(120 * time.Second)).Seconds())
				if remaining < 1 {
					remaining = 1
				}
				return c.Status(429).JSON(fiber.Map{"error": "ERR_CAPACITY_COOLDOWN_ACTIVE", "message": "capacity or departure update is cooling down", "retry_after_seconds": remaining})
			}
			return c.Status(404).JSON(errJSON("ERR_TRIP_NOT_FOUND_OR_FORBIDDEN"))
		}
		store.SetWithTTL("trip:last-update:"+key, []byte(time.Now().UTC().Format(time.RFC3339)), 120*time.Second)
		return c.JSON(fiber.Map{"ok": true, "capacity": in.Capacity, "departure_at": in.DepartureAt, "cooldown_seconds": 120})
	})
	_ = v1
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.Listen(":" + port))
}

func normalizeStateCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) <= 2 {
		return value
	}
	states := map[string]string{"ABIA": "AB", "ADAMAWA": "AD", "AKWA IBOM": "AK", "ANAMBRA": "AN", "BAUCHI": "BA", "BAYELSA": "BY", "BENUE": "BE", "BORNO": "BO", "CROSS RIVER": "CR", "DELTA": "DE", "EBONYI": "EB", "EDO": "ED", "EKITI": "EK", "ENUGU": "EN", "GOMBE": "GO", "IMO": "IM", "JIGAWA": "JI", "KADUNA": "KD", "KANO": "KN", "KATSINA": "KT", "KEBBI": "KE", "KOGI": "KO", "KWARA": "KW", "LAGOS": "LA", "NASARAWA": "NA", "NIGER": "NI", "OGUN": "OG", "ONDO": "ON", "OSUN": "OS", "OYO": "OY", "PLATEAU": "PL", "RIVERS": "RI", "SOKOTO": "SO", "TARABA": "TA", "YOBE": "YO", "ZAMFARA": "ZA", "FCT": "FC", "ABUJA": "FC"}
	if code := states[value]; code != "" {
		return code
	}
	return "!"
}

func authenticate(secret string, db *pgxpool.Pool) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if secret == "" {
			return c.Status(503).JSON(errJSON("ERR_AUTH_NOT_CONFIGURED"))
		}
		parts := strings.Fields(c.Get("Authorization"))
		ticketAuth := false
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			if c.Path() != "/api/v1/ws" || c.Query("ticket") == "" {
				return c.Status(401).JSON(errJSON("ERR_AUTH_REQUIRED"))
			}
			parts = []string{"Bearer", c.Query("ticket")}
			ticketAuth = true
		}
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return c.Status(401).JSON(errJSON("ERR_AUTH_REQUIRED"))
		}
		t, err := jwt.Parse(parts[1], func(t *jwt.Token) (any, error) {
			if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !t.Valid {
			return c.Status(401).JSON(errJSON("ERR_INVALID_TOKEN"))
		}
		claims, ok := t.Claims.(jwt.MapClaims)
		if !ok {
			return c.Status(401).JSON(errJSON("ERR_INVALID_TOKEN"))
		}
		sub, _ := claims["sub"].(string)
		role, _ := claims["role"].(string)
		tokenType, _ := claims["token_type"].(string)
		if ticketAuth && tokenType != "websocket" || !ticketAuth && tokenType == "websocket" {
			return c.Status(401).JSON(errJSON("ERR_INVALID_TOKEN"))
		}
		if sub == "" || role == "" {
			return c.Status(401).JSON(errJSON("ERR_INVALID_TOKEN"))
		}
		if !ticketAuth {
			sessionID, _ := claims["sid"].(string)
			if sessionID != "" {
				var active bool
				if err := db.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM auth_refresh_tokens WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>now())`, sessionID, sub).Scan(&active); err != nil || !active {
					return c.Status(401).JSON(errJSON("ERR_SESSION_REVOKED"))
				}
			}
		}
		var activeRole string
		var roles []string
		var bikerApproved, dispatchApproved bool
		var suspendedAt *time.Time
		if err := db.QueryRow(c.UserContext(), `SELECT active_role,roles,biker_approved,dispatch_approved,suspended_at FROM users WHERE id=$1`, sub).Scan(&activeRole, &roles, &bikerApproved, &dispatchApproved, &suspendedAt); err != nil {
			return c.Status(401).JSON(errJSON("ERR_USER_NOT_FOUND"))
		}
		if activeRole != role {
			return c.Status(401).JSON(errJSON("ERR_STALE_ROLE_TOKEN"))
		}
		if suspendedAt != nil {
			return c.Status(403).JSON(errJSON("ERR_ACCOUNT_SUSPENDED"))
		}
		granted := false
		for _, grantedRole := range roles {
			if grantedRole == activeRole {
				granted = true
				break
			}
		}
		if !granted {
			return c.Status(403).JSON(errJSON("ERR_ROLE_NOT_GRANTED"))
		}
		if activeRole == "BIKER" && !bikerApproved {
			return c.Status(403).JSON(errJSON("ERR_BIKER_KYC_REQUIRED"))
		}
		if activeRole == "DISPATCH_RIDER" && !dispatchApproved {
			return c.Status(403).JSON(errJSON("ERR_DISPATCH_KYC_INCOMPLETE"))
		}
		if activeRole == "DISPATCHER" || activeRole == "PARK_OPERATOR" {
			var assigned bool
			err := db.QueryRow(c.UserContext(), `SELECT EXISTS(SELECT 1 FROM trip_operator_assignments WHERE user_id=$1) OR EXISTS(SELECT 1 FROM park_operator_assignments WHERE user_id=$1)`, sub).Scan(&assigned)
			if err != nil || !assigned {
				return c.Status(403).JSON(errJSON("ERR_OPERATIONAL_ASSIGNMENT_REQUIRED"))
			}
		}
		c.Locals("identity", identity{sub, activeRole})
		return c.Next()
	}
}
func errJSON(code string) fiber.Map {
	return fiber.Map{"error": code, "message": strings.ReplaceAll(strings.ToLower(code), "_", " ")}
}
func envInt32(k string, fallback int32) int32 {
	var n int32
	if _, err := fmt.Sscan(os.Getenv(k), &n); err != nil || n < 1 {
		return fallback
	}
	return n
}

func purgeExpiredTripMessages(db *pgxpool.Pool) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = db.Exec(ctx, `UPDATE escalations e SET preserved_chat_log=COALESCE((SELECT jsonb_agg(jsonb_build_object('id',m.id,'sender_id',m.sender_id,'content',m.body,'attachment_media_id',m.attachment_media_id,'created_at',m.created_at) ORDER BY m.created_at) FROM messages m WHERE m.trip_id=e.trip_id),'[]'::jsonb),frozen_at=now() WHERE e.status IN ('OPEN','IN_PROGRESS') AND e.frozen_at IS NULL AND EXISTS(SELECT 1 FROM trips t WHERE t.id=e.trip_id AND t.status IN ('ARRIVED','CANCELLED') AND t.arrived_at IS NOT NULL AND now()>t.arrived_at+interval '6 minutes')`)
		_, _ = db.Exec(ctx, `DELETE FROM messages m USING trips t WHERE m.trip_id=t.id AND t.status IN ('ARRIVED','CANCELLED') AND t.arrived_at IS NOT NULL AND now()>t.arrived_at+interval '6 minutes' AND NOT EXISTS(SELECT 1 FROM escalations e WHERE e.trip_id=t.id AND e.status IN ('OPEN','IN_PROGRESS'))`)
		cancel()
	}
}
