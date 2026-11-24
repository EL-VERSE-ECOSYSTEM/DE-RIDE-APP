package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

var allowedRegistrationRoles = map[string]bool{"PASSENGER": true, "DRIVER": true, "DISPATCHER": true, "BIKER": true, "DISPATCH_RIDER": true, "PARK_OPERATOR": true}

func registerAuthRoutes(v1 fiber.Router, db *pgxpool.Pool) {
	v1.Post("/auth/register", func(c *fiber.Ctx) error {
		var body struct {
			Name        string `json:"name"`
			Email       string `json:"email"`
			PhoneNumber string `json:"phone_number"`
			Password    string `json:"password"`
			Role        string `json:"role"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		body.Email = strings.ToLower(strings.TrimSpace(body.Email))
		body.PhoneNumber = normalizeNigerianPhone(body.PhoneNumber)
		body.Role = strings.ToUpper(strings.TrimSpace(body.Role))
		if body.Role == "" {
			body.Role = "PASSENGER"
		}
		if _, err := mail.ParseAddress(body.Email); err != nil || body.PhoneNumber == "" || len(body.Password) < 10 || len(body.Password) > 128 || !allowedRegistrationRoles[body.Role] || len(body.Name) > 120 {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REGISTRATION"))
		}
		if len(os.Getenv("JWT_ACCESS_SECRET")) < 32 || len(os.Getenv("JWT_REFRESH_SECRET")) < 32 {
			return c.Status(503).JSON(errJSON("ERR_AUTH_NOT_CONFIGURED"))
		}
		digest, err := hashPassword(body.Password)
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_PASSWORD_HASH"))
		}
		code, err := otpCode()
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_OTP_CREATE"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		requestedRole := body.Role
		activeRole := body.Role
		if requestedRole == "DRIVER" || requestedRole == "DISPATCHER" || requestedRole == "PARK_OPERATOR" || requestedRole == "BIKER" || requestedRole == "DISPATCH_RIDER" {
			activeRole = "PASSENGER"
		}
		var userID string
		err = tx.QueryRow(c.UserContext(), `INSERT INTO users(email,password_hash,active_role,roles,display_name,phone_number) VALUES($1,$2,$3,ARRAY[$3]::text[],$4,$5) RETURNING id`, body.Email, digest, activeRole, strings.TrimSpace(body.Name), body.PhoneNumber).Scan(&userID)
		if err != nil {
			return c.Status(409).JSON(errJSON("ERR_EMAIL_ALREADY_REGISTERED"))
		}
		if requestedRole != activeRole {
			if _, err = tx.Exec(c.UserContext(), `INSERT INTO role_requests(user_id,requested_role) VALUES($1,$2)`, userID, requestedRole); err != nil {
				return c.Status(500).JSON(errJSON("ERR_ROLE_REQUEST_CREATE_FAILED"))
			}
			if _, err = tx.Exec(c.UserContext(), `INSERT INTO role_audit_log(user_id,actor_id,action,role,note) VALUES($1,$1,'REQUESTED',$2,'Initial account role selection')`, userID, requestedRole); err != nil {
				return c.Status(500).JSON(errJSON("ERR_ROLE_AUDIT_FAILED"))
			}
		}
		otpExpiryMinutes := envInt32("OTP_EXPIRY_MINUTES", 10)
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO email_otps(user_id,otp_hash,expires_at) VALUES($1,$2,now()+make_interval(mins=>$3))`, userID, otpHash(code), otpExpiryMinutes); err != nil {
			return c.Status(500).JSON(errJSON("ERR_OTP_CREATE"))
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO wallet_accounts(user_id,balance_ngn) VALUES($1,0) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_WALLET_INITIALIZE"))
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO amebo_wallets(user_id,free_tokens,free_expires_at,purchased_tokens) VALUES($1,10,(date_trunc('day',now() AT TIME ZONE 'Africa/Lagos') + interval '1 day') AT TIME ZONE 'Africa/Lagos',0) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_TOKEN_WALLET_INITIALIZE"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		if err = sendOTP(body.Email, code); err != nil {
			return c.Status(503).JSON(errJSON("ERR_EMAIL_DELIVERY_FAILED"))
		}
		return c.Status(202).JSON(fiber.Map{"status": "OTP_SENT", "expires_in_seconds": int(otpExpiryMinutes) * 60})
	})
	v1.Post("/auth/verify-otp", func(c *fiber.Ctx) error {
		var body struct {
			Email string `json:"email"`
			Code  string `json:"code"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		body.Email = strings.ToLower(strings.TrimSpace(body.Email))
		if len(body.Code) != 6 {
			return c.Status(400).JSON(errJSON("ERR_INVALID_OTP"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var userID, stored string
		var attempts int
		err = tx.QueryRow(c.UserContext(), `SELECT u.id,o.otp_hash,o.attempts FROM users u JOIN email_otps o ON o.user_id=u.id WHERE u.email=$1 AND o.consumed_at IS NULL AND o.expires_at>now() ORDER BY o.created_at DESC LIMIT 1 FOR UPDATE OF o`, body.Email).Scan(&userID, &stored, &attempts)
		if err != nil {
			return c.Status(400).JSON(errJSON("ERR_OTP_INVALID_OR_EXPIRED"))
		}
		if attempts >= int(envInt32("OTP_MAX_ATTEMPTS", 5)) {
			return c.Status(429).JSON(errJSON("ERR_OTP_ATTEMPTS_EXCEEDED"))
		}
		if !hmac.Equal([]byte(stored), []byte(otpHash(body.Code))) {
			_, _ = tx.Exec(c.UserContext(), `UPDATE email_otps SET attempts=attempts+1 WHERE user_id=$1 AND consumed_at IS NULL`, userID)
			_ = tx.Commit(c.UserContext())
			return c.Status(400).JSON(errJSON("ERR_OTP_INVALID_OR_EXPIRED"))
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE email_otps SET consumed_at=now() WHERE user_id=$1 AND consumed_at IS NULL`, userID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE users SET email_verified=true WHERE id=$1`, userID); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return issueToken(c, userID, "", db)
	})
	v1.Post("/auth/login", func(c *fiber.Ctx) error {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		body.Email = strings.ToLower(strings.TrimSpace(body.Email))
		var id, digest, role string
		var verified bool
		if err := db.QueryRow(c.UserContext(), `SELECT id,password_hash,active_role,email_verified FROM users WHERE email=$1`, body.Email).Scan(&id, &digest, &role, &verified); err != nil || !verifyPassword(body.Password, digest) {
			return c.Status(401).JSON(errJSON("ERR_INVALID_CREDENTIALS"))
		}
		if !verified {
			return c.Status(403).JSON(errJSON("ERR_EMAIL_NOT_VERIFIED"))
		}
		return issueToken(c, id, role, db)
	})
	v1.Post("/auth/refresh", func(c *fiber.Ctx) error {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.BodyParser(&body); err != nil || body.RefreshToken == "" {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		secret := os.Getenv("JWT_REFRESH_SECRET")
		if len(secret) < 32 {
			return c.Status(503).JSON(errJSON("ERR_AUTH_NOT_CONFIGURED"))
		}
		parsed, err := jwt.Parse(body.RefreshToken, func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !parsed.Valid {
			return c.Status(401).JSON(errJSON("ERR_INVALID_REFRESH_TOKEN"))
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok || claims["token_type"] != "refresh" {
			return c.Status(401).JSON(errJSON("ERR_INVALID_REFRESH_TOKEN"))
		}
		userID, _ := claims["sub"].(string)
		jti, _ := claims["jti"].(string)
		if userID == "" || jti == "" {
			return c.Status(401).JSON(errJSON("ERR_INVALID_REFRESH_TOKEN"))
		}
		tx, err := db.Begin(c.UserContext())
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		defer tx.Rollback(c.UserContext())
		var revokedUser string
		if err = tx.QueryRow(c.UserContext(), `UPDATE auth_refresh_tokens SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND expires_at>now() RETURNING user_id`, jti, userID).Scan(&revokedUser); err != nil {
			return c.Status(401).JSON(errJSON("ERR_REFRESH_TOKEN_REVOKED"))
		}
		var role string
		var verified bool
		if err = tx.QueryRow(c.UserContext(), `SELECT active_role,email_verified FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&role, &verified); err != nil || !verified {
			return c.Status(401).JSON(errJSON("ERR_USER_NOT_FOUND"))
		}
		response, err := createTokenPair(c.UserContext(), tx, userID, role)
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_TOKEN_ISSUE"))
		}
		if err = tx.Commit(c.UserContext()); err != nil {
			return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
		}
		return c.JSON(response)
	})
	v1.Post("/auth/logout", func(c *fiber.Ctx) error {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := c.BodyParser(&body); err != nil || body.RefreshToken == "" {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		secret := os.Getenv("JWT_REFRESH_SECRET")
		parsed, err := jwt.Parse(body.RefreshToken, func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !parsed.Valid {
			return c.Status(401).JSON(errJSON("ERR_INVALID_REFRESH_TOKEN"))
		}
		claims, ok := parsed.Claims.(jwt.MapClaims)
		if !ok || claims["token_type"] != "refresh" {
			return c.Status(401).JSON(errJSON("ERR_INVALID_REFRESH_TOKEN"))
		}
		userID, _ := claims["sub"].(string)
		jti, _ := claims["jti"].(string)
		if userID == "" || jti == "" {
			return c.Status(401).JSON(errJSON("ERR_INVALID_REFRESH_TOKEN"))
		}
		result, err := db.Exec(c.UserContext(), `UPDATE auth_refresh_tokens SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, jti, userID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
		}
		if result.RowsAffected() == 0 {
			return c.Status(401).JSON(errJSON("ERR_REFRESH_TOKEN_REVOKED"))
		}
		return c.JSON(fiber.Map{"status": "SIGNED_OUT"})
	})
}

func issueToken(c *fiber.Ctx, userID, role string, db *pgxpool.Pool) error {
	if role == "" {
		if err := db.QueryRow(c.UserContext(), `SELECT active_role FROM users WHERE id=$1`, userID).Scan(&role); err != nil {
			return c.Status(401).JSON(errJSON("ERR_USER_NOT_FOUND"))
		}
	}
	if len(os.Getenv("JWT_ACCESS_SECRET")) < 32 || len(os.Getenv("JWT_REFRESH_SECRET")) < 32 {
		return c.Status(503).JSON(errJSON("ERR_AUTH_NOT_CONFIGURED"))
	}
	tx, err := db.Begin(c.UserContext())
	if err != nil {
		return c.Status(503).JSON(errJSON("ERR_DATABASE_UNAVAILABLE"))
	}
	defer tx.Rollback(c.UserContext())
	response, err := createTokenPair(c.UserContext(), tx, userID, role)
	if err != nil {
		return c.Status(500).JSON(errJSON("ERR_TOKEN_ISSUE"))
	}
	if err = tx.Commit(c.UserContext()); err != nil {
		return c.Status(500).JSON(errJSON("ERR_DATABASE_QUERY"))
	}
	return c.JSON(response)
}

func createTokenPair(ctx context.Context, tx pgx.Tx, userID, role string) (fiber.Map, error) {
	if len(os.Getenv("JWT_ACCESS_SECRET")) < 32 || len(os.Getenv("JWT_REFRESH_SECRET")) < 32 {
		return nil, fmt.Errorf("JWT secrets must be at least 32 characters")
	}
	now := time.Now().UTC()
	accessExpiry := now.Add(60 * time.Minute)
	refreshExpiry := now.Add(30 * 24 * time.Hour)
	refreshID, err := newUUID()
	if err != nil {
		return nil, err
	}
	access := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID, "role": role, "sid": refreshID, "exp": accessExpiry.Unix(), "iat": now.Unix()})
	accessToken, err := access.SignedString([]byte(os.Getenv("JWT_ACCESS_SECRET")))
	if err != nil {
		return nil, err
	}
	refresh := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID, "token_type": "refresh", "jti": refreshID, "exp": refreshExpiry.Unix(), "iat": now.Unix()})
	refreshToken, err := refresh.SignedString([]byte(os.Getenv("JWT_REFRESH_SECRET")))
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO auth_refresh_tokens(id,user_id,expires_at) VALUES($1,$2,$3)`, refreshID, userID, refreshExpiry); err != nil {
		return nil, err
	}
	return fiber.Map{"access_token": accessToken, "refresh_token": refreshToken, "token_type": "Bearer", "expires_in": 3600, "refresh_expires_in": 2592000, "active_role": role}, nil
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const rounds uint32 = 3
	digest := argon2.IDKey([]byte(password), salt, rounds, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$v=19$m=65536,t=%d,p=2$%s$%s", rounds, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(digest)), nil
}
func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return false
	}
	var rounds uint32
	var memory uint32 = 65536
	var threads uint8 = 2
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &rounds, &threads); err != nil || rounds < 1 || rounds > 6 || memory < 32768 || memory > 131072 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, rounds, memory, threads, uint32(len(want)))
	return hmac.Equal(got, want)
}
func otpCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
func otpHash(code string) string {
	mac := hmac.New(sha256.New, []byte(os.Getenv("JWT_ACCESS_SECRET")))
	mac.Write([]byte(code))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func normalizeNigerianPhone(value string) string {
	value = strings.TrimSpace(value)
	var digits strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
			continue
		}
		if char == '+' || char == ' ' || char == '-' || char == '(' || char == ')' {
			continue
		}
		return ""
	}
	number := digits.String()
	if strings.HasPrefix(number, "0") && len(number) == 11 {
		number = "234" + number[1:]
	}
	if strings.HasPrefix(number, "234") && len(number) >= 13 && len(number) <= 15 {
		return "+" + number
	}
	return ""
}

func sendOTP(to, code string) error {
	host := os.Getenv("SMTP_HOST")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	port := os.Getenv("SMTP_PORT")
	if host == "" || user == "" || pass == "" {
		return fmt.Errorf("SMTP credentials are not configured")
	}
	if port == "" {
		port = "465"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return err
	}
	defer conn.Close()
	tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err = tlsConn.HandshakeContext(ctx); err != nil {
		return err
	}
	client, err := smtp.NewClient(tlsConn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	auth := smtp.PlainAuth("", user, pass, host)
	if err = client.Auth(auth); err != nil {
		return err
	}
	if err = client.Mail(user); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	fromName := os.Getenv("SMTP_FROM_NAME")
	if fromName == "" {
		fromName = "DE RIDE"
	}
	fromEmail := os.Getenv("SMTP_FROM_EMAIL")
	if fromEmail == "" {
		fromEmail = user
	}
	message := fmt.Sprintf("From: %s <%s>\r\nTo: %s\r\nSubject: Your DE RIDE verification code\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\nYour verification code is %s. It expires in %d minutes. If you did not request it, ignore this email.\r\n", fromName, fromEmail, to, code, envInt32("OTP_EXPIRY_MINUTES", 10))
	if _, err = writer.Write([]byte(message)); err != nil {
		return err
	}
	return writer.Close()
}
