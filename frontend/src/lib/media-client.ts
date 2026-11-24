import { apiRequest } from '@/lib/api-client';

type UploadPolicy = {
  cloud_name: string; api_key: string; upload_preset: string; upload_url: string;
  timestamp: number; folder: string; public_id: string; type: string;
  allowed_formats: string; transformation: string; signature: string; max_bytes: number;
};

type UploadConfirmation = { id: string; public_id: string; category: string; secure_url: string; bytes: number; retained: boolean; expires_at: string | null };

export async function uploadMedia(file: File, category: 'PROFILE_PHOTO' | 'KYC_DOCUMENT' | 'VEHICLE_PHOTO' | 'CHAT_ATTACHMENT' | 'SUPPORT_ATTACHMENT'): Promise<UploadConfirmation> {
  const allowed = category === 'KYC_DOCUMENT' || category === 'SUPPORT_ATTACHMENT'
    ? ['image/jpeg','image/png','image/webp','application/pdf']
    : ['image/jpeg','image/png','image/webp'];
  if (!allowed.includes(file.type)) throw new Error('Choose a JPG, PNG, WebP image' + (allowed.includes('application/pdf') ? ' or PDF.' : '.'));
  const response = await apiRequest('/media/cloudinary-sign', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ category }) });
  const policy = await response.json() as UploadPolicy & { error?: string; message?: string };
  if (!response.ok) throw new Error(policy.message || policy.error || 'Could not prepare a secure upload.');
  if (file.size > policy.max_bytes) throw new Error('Files must be 5 MB or smaller.');
  const form = new FormData();
  form.set('file', file);
  form.set('api_key', policy.api_key);
  form.set('timestamp', String(policy.timestamp));
  form.set('folder', policy.folder);
  form.set('public_id', policy.public_id);
  form.set('type', policy.type);
  form.set('upload_preset', policy.upload_preset);
  form.set('allowed_formats', policy.allowed_formats);
  if (policy.transformation) form.set('transformation', policy.transformation);
  form.set('signature', policy.signature);
  const uploaded = await fetch(policy.upload_url, { method: 'POST', body: form });
  const result = await uploaded.json() as { public_id?: string; error?: { message?: string } };
  if (!uploaded.ok || result.public_id !== policy.public_id) throw new Error(result.error?.message || 'The media provider rejected this upload.');
  const confirmation = await apiRequest('/media/cloudinary-confirm', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ public_id: result.public_id }) });
  const asset = await confirmation.json() as UploadConfirmation & { error?: string; message?: string };
  if (!confirmation.ok) throw new Error(asset.message || asset.error || 'The uploaded file could not be registered.');
  return asset;
}
