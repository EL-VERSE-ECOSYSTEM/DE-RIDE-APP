package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxCloudinaryAssetBytes = 5 * 1024 * 1024

type mediaPolicy struct {
	category  string
	resource  string
	format    string
	allowed   string
	kyc       bool
	transform string
}

var mediaPolicies = map[string]mediaPolicy{
	"PROFILE_PHOTO":      {category: "PROFILE_PHOTO", resource: "image", format: "jpg", allowed: "jpg,png,webp", transform: "c_limit,h_512,w_512,q_auto:good"},
	"KYC_DOCUMENT":       {category: "KYC_DOCUMENT", resource: "image", format: "jpg", allowed: "jpg,jpeg,png,webp,pdf", kyc: true, transform: "c_limit,h_1800,w_1800,q_auto:good"},
	"VEHICLE_PHOTO":      {category: "VEHICLE_PHOTO", resource: "image", format: "jpg", allowed: "jpg,png,webp", transform: "c_limit,h_1600,w_1600,q_auto:good"},
	"CHAT_ATTACHMENT":    {category: "CHAT_ATTACHMENT", resource: "image", format: "jpg", allowed: "jpg,png,webp", transform: "c_limit,h_1400,w_1400,q_auto:good"},
	"SUPPORT_ATTACHMENT": {category: "SUPPORT_ATTACHMENT", resource: "image", format: "jpg", allowed: "jpg,png,webp,pdf", transform: "c_limit,h_1600,w_1600,q_auto:good"},
}

func registerMediaRoutes(secure fiber.Router, db *pgxpool.Pool) {
	secure.Post("/me/profile-photo", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var req struct {
			MediaID string `json:"media_id"`
		}
		if err := c.BodyParser(&req); err != nil || req.MediaID == "" {
			return c.Status(400).JSON(errJSON("ERR_PROFILE_MEDIA_INVALID"))
		}
		result, err := db.Exec(c.UserContext(), `UPDATE users u SET profile_photo_media_id=m.id,profile_photo_url=m.secure_url,cloudinary_public_id=m.public_id FROM media_assets m WHERE u.id=$1 AND m.id=$2 AND m.user_id=u.id AND m.asset_category='PROFILE_PHOTO' AND m.upload_status='UPLOADED' AND m.deletion_status='ACTIVE'`, me.UserID, req.MediaID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_PROFILE_UPDATE_FAILED"))
		}
		if result.RowsAffected() == 0 {
			return c.Status(422).JSON(errJSON("ERR_PROFILE_MEDIA_INVALID"))
		}
		return c.JSON(fiber.Map{"status": "UPDATED"})
	})
	secure.Get("/media/:mediaID/access", func(c *fiber.Ctx) error {
		me := c.Locals("identity").(identity)
		var publicID, resourceType, format, ownerID string
		var sharedWithUser bool
		err := db.QueryRow(c.UserContext(), `SELECT a.public_id,a.resource_type,a.format,a.user_id,
			EXISTS(SELECT 1 FROM messages m JOIN trips t ON t.id=m.trip_id WHERE m.attachment_media_id=a.id AND (t.driver_id=$2 OR EXISTS(SELECT 1 FROM tickets k WHERE k.trip_id=t.id AND k.passenger_id=$2))) OR
			EXISTS(SELECT 1 FROM support_messages m JOIN support_cases s ON s.id=m.case_id WHERE m.attachment_media_id=a.id AND s.requester_id=$2)
			FROM media_assets a WHERE a.id=$1 AND a.upload_status='UPLOADED' AND a.deletion_status='ACTIVE' AND a.asset_category IN ('CHAT_ATTACHMENT','SUPPORT_ATTACHMENT') OR a.id=$1 AND a.upload_status='UPLOADED' AND a.deletion_status='ACTIVE' AND a.user_id=$2`, c.Params("mediaID"), me.UserID).Scan(&publicID, &resourceType, &format, &ownerID, &sharedWithUser)
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_MEDIA_NOT_FOUND"))
		}
		if ownerID != me.UserID && !sharedWithUser && me.Role != "ADMIN" {
			return c.Status(403).JSON(errJSON("ERR_MEDIA_ACCESS_FORBIDDEN"))
		}
		cloud, apiKey, secret, _ := cloudinaryConfig()
		if cloud == "" || apiKey == "" || secret == "" {
			return c.Status(503).JSON(errJSON("ERR_MEDIA_SERVICE_NOT_CONFIGURED"))
		}
		expires := time.Now().Add(10 * time.Minute).Unix()
		timestamp := time.Now().Unix()
		params := map[string]string{"expires_at": fmt.Sprint(expires), "format": format, "public_id": publicID, "timestamp": fmt.Sprint(timestamp), "type": "authenticated"}
		signature := cloudinarySignature(params, secret)
		values := url.Values{}
		for key, value := range params {
			values.Set(key, value)
		}
		values.Set("api_key", apiKey)
		values.Set("signature", signature)
		link := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/%s/download?%s", url.PathEscape(cloud), url.PathEscape(resourceType), values.Encode())
		return c.JSON(fiber.Map{"url": link, "expires_at": time.Unix(expires, 0).UTC()})
	})
	secure.Post("/media/cloudinary-sign", func(c *fiber.Ctx) error {
		identity := c.Locals("identity").(identity)
		var req struct {
			Category string `json:"category"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		policy, ok := mediaPolicies[strings.ToUpper(strings.TrimSpace(req.Category))]
		if !ok {
			return c.Status(422).JSON(errJSON("ERR_MEDIA_CATEGORY_INVALID"))
		}
		cloud, apiKey, secret, preset := cloudinaryConfig()
		if cloud == "" || apiKey == "" || secret == "" || preset == "" {
			return c.Status(503).JSON(errJSON("ERR_MEDIA_SERVICE_NOT_CONFIGURED"))
		}
		assetID, err := uuid.NewRandom()
		if err != nil {
			return c.Status(500).JSON(errJSON("ERR_MEDIA_ID_CREATE"))
		}
		publicID := fmt.Sprintf("deride/users/%s/%s/%s", identity.UserID, strings.ToLower(policy.category), assetID.String())
		timestamp := time.Now().Unix()
		params := map[string]string{
			"allowed_formats": policy.allowed,
			"folder":          fmt.Sprintf("deride/users/%s/%s", identity.UserID, strings.ToLower(policy.category)),
			"overwrite":       "false",
			"public_id":       publicID,
			"type":            "authenticated",
			"upload_preset":   preset,
			"timestamp":       fmt.Sprint(timestamp),
		}
		if policy.transform != "" {
			params["transformation"] = policy.transform
		}
		signature := cloudinarySignature(params, secret)
		initialExpiry := time.Now().Add(24 * time.Hour)
		_, err = db.Exec(c.UserContext(), `INSERT INTO media_assets(user_id,public_id,asset_category,resource_type,format,bytes,secure_url,is_kyc,expires_at,upload_status,delivery_type) VALUES($1,$2,$3,$4,$5,0,NULL,$6,$7,'PENDING','authenticated')`, identity.UserID, publicID, policy.category, policy.resource, policy.format, policy.kyc, nullableTime(initialExpiry))
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_MEDIA_RESERVATION_FAILED"))
		}
		return c.JSON(fiber.Map{"cloud_name": cloud, "api_key": apiKey, "upload_preset": preset, "upload_url": fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/%s/upload", cloud, policy.resource), "timestamp": timestamp, "folder": params["folder"], "public_id": publicID, "type": "authenticated", "allowed_formats": policy.allowed, "transformation": policy.transform, "signature": signature, "max_bytes": maxCloudinaryAssetBytes, "is_kyc": policy.kyc})
	})
	secure.Post("/media/cloudinary-confirm", func(c *fiber.Ctx) error {
		identity := c.Locals("identity").(identity)
		var req struct {
			PublicID string `json:"public_id"`
		}
		if err := c.BodyParser(&req); err != nil || req.PublicID == "" {
			return c.Status(400).JSON(errJSON("ERR_INVALID_REQUEST"))
		}
		var category, resourceType, deliveryType string
		var isKYC bool
		err := db.QueryRow(c.UserContext(), `SELECT asset_category,resource_type,delivery_type,is_kyc FROM media_assets WHERE user_id=$1 AND public_id=$2 AND upload_status='PENDING'`, identity.UserID, req.PublicID).Scan(&category, &resourceType, &deliveryType, &isKYC)
		if err != nil {
			return c.Status(404).JSON(errJSON("ERR_MEDIA_RESERVATION_NOT_FOUND"))
		}
		asset, err := fetchCloudinaryAsset(c.UserContext(), resourceType, deliveryType, req.PublicID)
		if err != nil {
			return c.Status(422).JSON(errJSON("ERR_MEDIA_UPLOAD_NOT_FOUND"))
		}
		if asset.Bytes <= 0 || asset.Bytes > maxCloudinaryAssetBytes || !validMediaFormat(category, asset.Format) {
			_ = destroyCloudinaryAsset(c.UserContext(), resourceType, deliveryType, req.PublicID)
			_, _ = db.Exec(c.UserContext(), `UPDATE media_assets SET upload_status='REJECTED',deletion_status='DELETED',deleted_at=now() WHERE public_id=$1 AND user_id=$2`, req.PublicID, identity.UserID)
			return c.Status(422).JSON(errJSON("ERR_MEDIA_FILE_REJECTED"))
		}
		expiresAt := time.Now().Add(30 * 24 * time.Hour)
		if isKYC {
			expiresAt = time.Time{}
		}
		var assetUUID string
		err = db.QueryRow(c.UserContext(), `UPDATE media_assets SET bytes=$1,format=$2,secure_url=$3,upload_status='UPLOADED',uploaded_at=now(),expires_at=$4 WHERE user_id=$5 AND public_id=$6 AND upload_status='PENDING' RETURNING id`, asset.Bytes, strings.ToLower(asset.Format), asset.SecureURL, nullableTime(expiresAt), identity.UserID, req.PublicID).Scan(&assetUUID)
		if err != nil {
			return c.Status(503).JSON(errJSON("ERR_MEDIA_REGISTER_FAILED"))
		}
		return c.JSON(fiber.Map{"id": assetUUID, "public_id": req.PublicID, "category": category, "secure_url": asset.SecureURL, "bytes": asset.Bytes, "retained": isKYC, "expires_at": nullableTime(expiresAt)})
	})
}

type cloudinaryAsset struct {
	Bytes     int64  `json:"bytes"`
	Format    string `json:"format"`
	SecureURL string `json:"secure_url"`
	PublicID  string `json:"public_id"`
}

func cloudinaryConfig() (cloud, apiKey, secret, preset string) {
	return strings.TrimSpace(os.Getenv("CLOUDINARY_CLOUD_NAME")), strings.TrimSpace(os.Getenv("CLOUDINARY_API_KEY")), os.Getenv("CLOUDINARY_API_SECRET"), strings.TrimSpace(os.Getenv("CLOUDINARY_UPLOAD_PRESET"))
}

func cloudinarySignature(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(mac.Sum(nil))
}

func fetchCloudinaryAsset(ctx context.Context, resourceType, deliveryType, publicID string) (cloudinaryAsset, error) {
	cloud, apiKey, secret, _ := cloudinaryConfig()
	var asset cloudinaryAsset
	endpoint := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/resources/%s/upload/%s?type=%s", url.PathEscape(cloud), url.PathEscape(resourceType), url.PathEscape(publicID), url.QueryEscape(deliveryType))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return asset, err
	}
	request.SetBasicAuth(apiKey, secret)
	response, err := (&http.Client{Timeout: 12 * time.Second}).Do(request)
	if err != nil {
		return asset, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return asset, fmt.Errorf("cloudinary resource status %d", response.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&asset)
	if err == nil && asset.PublicID != publicID {
		err = errors.New("Cloudinary public ID mismatch")
	}
	return asset, err
}

func destroyCloudinaryAsset(ctx context.Context, resourceType, deliveryType, publicID string) error {
	cloud, apiKey, secret, _ := cloudinaryConfig()
	timestamp := fmt.Sprint(time.Now().Unix())
	params := map[string]string{"public_id": publicID, "timestamp": timestamp, "type": deliveryType, "invalidate": "true"}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	values.Set("api_key", apiKey)
	values.Set("signature", cloudinarySignature(params, secret))
	endpoint := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/%s/destroy", url.PathEscape(cloud), url.PathEscape(resourceType))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 12 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Cloudinary delete status %d", response.StatusCode)
	}
	var result struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return err
	}
	if result.Result != "ok" && result.Result != "not found" {
		return fmt.Errorf("Cloudinary delete result %q", result.Result)
	}
	return nil
}

func validMediaFormat(category, format string) bool {
	format = strings.ToLower(strings.TrimPrefix(format, "."))
	allowed := map[string]bool{"jpg": true, "jpeg": true, "png": true, "webp": true}
	if category == "KYC_DOCUMENT" || category == "SUPPORT_ATTACHMENT" {
		allowed["pdf"] = true
	}
	return allowed[format]
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func processExpiredMedia(db *pgxpool.Pool) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `WITH due AS (SELECT id FROM media_assets WHERE (is_kyc=false OR upload_status='PENDING') AND expires_at<=now() AND deletion_status IN ('ACTIVE','DELETE_FAILED') AND upload_status IN ('UPLOADED','PENDING') ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT 20) UPDATE media_assets m SET deletion_status='DELETE_PENDING' FROM due WHERE m.id=due.id RETURNING m.id,m.public_id,m.resource_type,m.delivery_type,m.upload_status`)
	if err != nil {
		return
	}
	type deletion struct{ id, publicID, resourceType, deliveryType, uploadStatus string }
	items := make([]deletion, 0)
	for rows.Next() {
		var d deletion
		if rows.Scan(&d.id, &d.publicID, &d.resourceType, &d.deliveryType, &d.uploadStatus) == nil {
			items = append(items, d)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return
	}
	if tx.Commit(ctx) != nil {
		return
	}
	for _, item := range items {
		deleteCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		err := destroyCloudinaryAsset(deleteCtx, item.resourceType, item.deliveryType, item.publicID)
		done()
		status := "DELETED"
		var deletedAt any = time.Now().UTC()
		if err != nil {
			status = "DELETE_FAILED"
			deletedAt = nil
		}
		if status == "DELETED" && item.uploadStatus == "PENDING" {
			_, _ = db.Exec(context.Background(), `UPDATE media_assets SET upload_status='REJECTED',deletion_status=$1,deleted_at=$2 WHERE id=$3`, status, deletedAt, item.id)
		} else {
			_, _ = db.Exec(context.Background(), `UPDATE media_assets SET deletion_status=$1,deleted_at=$2 WHERE id=$3`, status, deletedAt, item.id)
		}
		if status == "DELETED" {
			_, _ = db.Exec(context.Background(), `UPDATE users SET profile_photo_media_id=NULL,profile_photo_url=NULL,cloudinary_public_id=NULL WHERE profile_photo_media_id=$1`, item.id)
		}
	}
}

// Keep cryptographic signatures deterministic and reject invalid comparison
// paths with constant-time comparison where callers verify signatures.
func secureSignatureEqual(got, want string) bool {
	return hmac.Equal([]byte(strings.ToLower(got)), []byte(strings.ToLower(want)))
}
