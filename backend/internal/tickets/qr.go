package tickets

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strings"
	"time"
)

var statePattern = regexp.MustCompile(`^[A-Z]{2}$`)

type Payload struct {
	TicketID    string `json:"ticket_id"`
	TripID      string `json:"trip_id"`
	PassengerID string `json:"passenger_id"`
	Seat        string `json:"seat"`
	StateCode   string `json:"state_code"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

type SignedPayload struct {
	Payload   Payload `json:"payload"`
	Signature string  `json:"signature"`
	KeyID     string  `json:"key_id"`
}

func Sign(payload Payload, privatePEM, keyID string) (SignedPayload, error) {
	if payload.TicketID == "" || payload.TripID == "" || payload.PassengerID == "" || payload.Seat == "" || !statePattern.MatchString(payload.StateCode) {
		return SignedPayload{}, errors.New("invalid QR ticket claims")
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(privatePEM, `\n`, "\n")))
	if block == nil {
		return SignedPayload{}, errors.New("invalid QR private key PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if key, e := x509.ParseECPrivateKey(block.Bytes); e == nil {
			parsed = key
		} else {
			return SignedPayload{}, err
		}
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve.Params().Name != "P-256" {
		return SignedPayload{}, errors.New("QR key must be ECDSA P-256")
	}
	body, err := json.Marshal(map[string]any{"ticket_id": payload.TicketID, "trip_id": payload.TripID, "passenger_id": payload.PassengerID, "seat": payload.Seat, "state_code": payload.StateCode, "iat": payload.IssuedAt, "exp": payload.ExpiresAt})
	if err != nil {
		return SignedPayload{}, err
	}
	digest := sha256.Sum256(body)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return SignedPayload{}, err
	}
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])
	return SignedPayload{payload, base64.RawURLEncoding.EncodeToString(raw), keyID}, nil
}

func Verify(signed SignedPayload, publicPEM, expectedKeyID string, now time.Time) error {
	if expectedKeyID != "" && signed.KeyID != expectedKeyID {
		return errors.New("ticket key id is not active")
	}
	if signed.Payload.TicketID == "" || signed.Payload.TripID == "" || signed.Payload.PassengerID == "" || signed.Payload.Seat == "" || !statePattern.MatchString(signed.Payload.StateCode) || signed.Payload.ExpiresAt <= now.Unix() {
		return errors.New("ticket claims are invalid or expired")
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(publicPEM, `\n`, "\n")))
	if block == nil {
		return errors.New("invalid public key PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return err
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve.Params().Name != "P-256" {
		return errors.New("ticket key must be ECDSA P-256")
	}
	body, err := json.Marshal(map[string]any{"ticket_id": signed.Payload.TicketID, "trip_id": signed.Payload.TripID, "passenger_id": signed.Payload.PassengerID, "seat": signed.Payload.Seat, "state_code": signed.Payload.StateCode, "iat": signed.Payload.IssuedAt, "exp": signed.Payload.ExpiresAt})
	if err != nil {
		return err
	}
	decoded, err := base64.RawURLEncoding.DecodeString(signed.Signature)
	if err != nil || len(decoded) != 64 {
		return errors.New("invalid ticket signature encoding")
	}
	digest := sha256.Sum256(body)
	if !ecdsa.Verify(key, digest[:], new(big.Int).SetBytes(decoded[:32]), new(big.Int).SetBytes(decoded[32:])) {
		return errors.New("invalid ticket signature")
	}
	return nil
}

func SignWithEnvironment(payload Payload) (SignedPayload, error) {
	key := os.Getenv("QR_PRIVATE_KEY_PEM")
	id := os.Getenv("QR_KEY_ID")
	if key == "" || id == "" {
		return SignedPayload{}, errors.New("QR signing is not configured")
	}
	now := time.Now().UTC()
	if payload.IssuedAt == 0 {
		payload.IssuedAt = now.Unix()
	}
	if payload.ExpiresAt == 0 {
		payload.ExpiresAt = now.Add(365 * 24 * time.Hour).Unix()
	}
	return Sign(payload, key, id)
}

func GenerateManualCode(state string) (string, error) {
	state = strings.ToUpper(state)
	if !statePattern.MatchString(state) {
		return "", errors.New("state code must contain two letters")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	digits := fmt.Sprintf("%04d", n.Int64())
	return manualCode(state, digits), nil
}

func manualCode(state, digits string) string {
	value := 0
	for _, char := range state + digits {
		value = (value*31 + int(char)) % 97
	}
	return fmt.Sprintf("DR-%s-%s-%02d", state, digits, value)
}

func VerifyManualCode(code string) bool {
	parts := strings.Split(strings.ToUpper(strings.TrimSpace(code)), "-")
	if len(parts) != 4 || parts[0] != "DR" || !statePattern.MatchString(parts[1]) || len(parts[2]) != 4 || len(parts[3]) != 2 {
		return false
	}
	for _, r := range parts[2] + parts[3] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return manualCode(parts[1], parts[2]) == strings.ToUpper(strings.TrimSpace(code))
}
