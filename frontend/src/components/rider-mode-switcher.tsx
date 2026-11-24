'use client';

import { useState } from 'react';
import { apiRequest } from '@/lib/api-client';

export function RiderModeSwitcher({ target }: { target: 'BIKER' | 'DISPATCH_RIDER' }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  async function switchMode() {
    const token = sessionStorage.getItem('deride_access_token');
    if (!token) { window.location.assign('/login'); return; }
    setBusy(true); setMessage('');
    try {
      const response = await apiRequest('/users/switch-rider-mode', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ mode: target }) });
      const result = await response.json();
      if (!response.ok) throw new Error(result.message || result.error || 'Mode switch failed');
      sessionStorage.setItem('deride_access_token', result.access_token);
      if (result.refresh_token) sessionStorage.setItem('deride_refresh_token', result.refresh_token);
      window.location.assign(target === 'BIKER' ? '/biker/radar' : '/dispatch-rider/deliveries');
    } catch (error) { setMessage(error instanceof Error ? error.message : 'Mode switch failed'); }
    finally { setBusy(false); }
  }
  return <div><button type="button" onClick={switchMode} disabled={busy} style={{ minHeight: 48, padding: '0 16px', background: '#1A7D82', border: 0, borderRadius: 8, color: 'white', cursor: 'pointer' }}>{busy ? 'Switching…' : `Switch to ${target === 'BIKER' ? 'Biker' : 'Dispatch Rider'} mode`}</button>{message && <p role="status">{message}</p>}</div>;
}
