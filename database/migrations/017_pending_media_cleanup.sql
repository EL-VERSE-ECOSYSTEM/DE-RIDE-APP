-- A KYC document is retained indefinitely only after the server verifies and
-- confirms the upload. Abandoned unsigned reservations expire like other media.
ALTER TABLE media_assets DROP CONSTRAINT IF EXISTS media_assets_check;
ALTER TABLE media_assets DROP CONSTRAINT IF EXISTS media_assets_lifecycle_expiry_check;
UPDATE media_assets SET expires_at=COALESCE(created_at,now())+interval '24 hours'
WHERE upload_status='PENDING' AND expires_at IS NULL;
ALTER TABLE media_assets ADD CONSTRAINT media_assets_lifecycle_expiry_check CHECK (
  (upload_status='PENDING' AND expires_at IS NOT NULL)
  OR (upload_status='UPLOADED' AND ((is_kyc AND expires_at IS NULL) OR (NOT is_kyc AND expires_at IS NOT NULL)))
  OR upload_status='REJECTED'
);
CREATE INDEX IF NOT EXISTS media_assets_pending_expiry_idx ON media_assets(expires_at)
  WHERE upload_status='PENDING' AND deletion_status IN ('ACTIVE','DELETE_FAILED');
