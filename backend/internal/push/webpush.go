package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

type Subscription struct {
	Endpoint string
	P256DH   string
	Auth     string
}
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
}

func Send(subscription Subscription, message Message, publicKey, privatePEM, subject string) (int, error) {
	endpoint, err := url.Parse(subscription.Endpoint)
	if err != nil || endpoint.Scheme != "https" || !allowedHost(endpoint.Hostname()) {
		return 0, errors.New("invalid push endpoint")
	}
	clientPublicBytes, err := base64.RawURLEncoding.DecodeString(subscription.P256DH)
	if err != nil {
		return 0, err
	}
	clientPublic, err := ecdh.P256().NewPublicKey(clientPublicBytes)
	if err != nil {
		return 0, err
	}
	authSecret, err := base64.RawURLEncoding.DecodeString(subscription.Auth)
	if err != nil || len(authSecret) < 16 {
		return 0, errors.New("invalid push auth secret")
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(privatePEM, `\n`, "\n")))
	if block == nil {
		return 0, errors.New("invalid VAPID private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return 0, err
	}
	signingKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || signingKey.Curve.Params().Name != "P-256" {
		return 0, errors.New("VAPID key must be P-256")
	}
	serverPrivate, err := ecdh.P256().NewPrivateKey(signingKey.D.FillBytes(make([]byte, 32)))
	if err != nil {
		return 0, err
	}
	serverPublic := serverPrivate.PublicKey().Bytes()
	shared, err := serverPrivate.ECDH(clientPublic)
	if err != nil {
		return 0, err
	}
	info := append([]byte("WebPush: info\x00"), clientPublicBytes...)
	info = append(info, serverPublic...)
	prk := hkdf.Extract(sha256.New, shared, authSecret)
	ikm, err := hkdfBytes(prk, info, 32)
	if err != nil {
		return 0, err
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return 0, err
	}
	prk = hkdf.Extract(sha256.New, ikm, salt)
	cek, err := hkdfBytes(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	if err != nil {
		return 0, err
	}
	nonce, err := hkdfBytes(prk, []byte("Content-Encoding: nonce\x00"), 12)
	if err != nil {
		return 0, err
	}
	plain, err := json.Marshal(message)
	if err != nil {
		return 0, err
	}
	plain = append(plain, 0x02)
	blockCipher, err := aes.NewCipher(cek)
	if err != nil {
		return 0, err
	}
	gcm, err := cipher.NewGCM(blockCipher)
	if err != nil {
		return 0, err
	}
	sealed := gcm.Seal(nil, nonce, plain, nil)
	record := make([]byte, 0, 16+4+1+len(serverPublic)+len(sealed))
	record = append(record, salt...)
	rs := make([]byte, 4)
	binary.BigEndian.PutUint32(rs, 4096)
	record = append(record, rs...)
	record = append(record, byte(len(serverPublic)))
	record = append(record, serverPublic...)
	record = append(record, sealed...)
	public, err := base64.RawURLEncoding.DecodeString(publicKey)
	if err != nil {
		return 0, err
	}
	jwt, err := vapidJWT(endpoint, signingKey, public, subject)
	if err != nil {
		return 0, err
	}
	request, err := http.NewRequest(http.MethodPost, subscription.Endpoint, strings.NewReader(string(record)))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("TTL", "3600")
	request.Header.Set("Authorization", "vapid t="+jwt+", k="+publicKey)
	response, err := (&http.Client{Timeout: 8 * time.Second}).Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	return response.StatusCode, nil
}

func hkdfBytes(prk, info []byte, size int) ([]byte, error) {
	r := hkdf.Expand(sha256.New, prk, info)
	result := make([]byte, size)
	_, err := io.ReadFull(r, result)
	return result, err
}

func vapidJWT(endpoint *url.URL, key *ecdsa.PrivateKey, publicKey []byte, subject string) (string, error) {
	now := time.Now().Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{"aud": endpoint.Scheme + "://" + endpoint.Host, "exp": now + 12*60*60, "sub": subject})
	body := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(body))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	if len(publicKey) != 65 {
		return "", fmt.Errorf("VAPID public key must be uncompressed P-256")
	}
	return body + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func allowedHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "fcm.googleapis.com" || host == "web.push.apple.com" || host == "updates.push.services.mozilla.com" || host == "push.services.mozilla.com" || strings.HasSuffix(host, ".notify.windows.com")
}
