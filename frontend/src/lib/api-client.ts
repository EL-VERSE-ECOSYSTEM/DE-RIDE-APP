const API_BASE = (process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080/api/v1').replace(/\/$/, '');
let refreshInFlight: Promise<string | null> | null = null;

async function rotateAccessToken(): Promise<string | null> {
  if (refreshInFlight) return refreshInFlight;
  const refresh = sessionStorage.getItem('deride_refresh_token');
  if (!refresh) return null;
  refreshInFlight = (async () => {
    const response = await fetch(`${API_BASE}/auth/refresh`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ refresh_token: refresh }) });
    if (!response.ok) { clearSession(); return null; }
    const tokens = await response.json();
    sessionStorage.setItem('deride_access_token', tokens.access_token);
    sessionStorage.setItem('deride_refresh_token', tokens.refresh_token);
    return tokens.access_token as string;
  })();
  try { return await refreshInFlight; } finally { refreshInFlight = null; }
}

export function clearSession() {
  sessionStorage.removeItem('deride_access_token');
  sessionStorage.removeItem('deride_refresh_token');
}

export async function signOut(): Promise<void> {
  const refresh = sessionStorage.getItem('deride_refresh_token');
  try {
    if (refresh) await fetch(`${API_BASE}/auth/logout`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ refresh_token: refresh }) });
  } finally { clearSession(); }
}

export type AccountProfile = {
  id: string; email: string; display_name: string; active_role: string; roles: string[];
  email_verified: boolean; biker_approved: boolean; dispatch_approved: boolean; gps_opt_in: boolean;
  created_at: string; wallet_balance_ngn: number; amebo_free_tokens: number;
  amebo_purchased_tokens: number; amebo_free_expires_at: string | null;
};

export async function apiRequest(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  const token = sessionStorage.getItem('deride_access_token');
  if (token) headers.set('Authorization', `Bearer ${token}`);
  let response = await fetch(`${API_BASE}${path}`, { ...init, cache: init.cache ?? 'no-store', headers });
  if (response.status === 401 && path !== '/auth/refresh') {
    const rotated = await rotateAccessToken();
    if (rotated) { headers.set('Authorization', `Bearer ${rotated}`); response = await fetch(`${API_BASE}${path}`, { ...init, cache: init.cache ?? 'no-store', headers }); }
  }
  return response;
}

// Browsers cannot attach an Authorization header to a WebSocket handshake. Get
// a short-lived ticket over the authenticated API, then use it for the upgrade.
export async function openAuthenticatedWebSocket(): Promise<WebSocket> {
  if (typeof window === 'undefined') throw new Error('WebSockets are only available in the browser');
  const response = await apiRequest('/ws-ticket', { method: 'POST' });
  if (!response.ok) throw new Error('Could not authorize realtime connection');
  const { ticket } = await response.json() as { ticket: string };
  const endpoint = process.env.NEXT_PUBLIC_WS_URL || `${API_BASE.replace(/^http/, 'ws')}/ws`;
  const url = new URL(endpoint);
  url.searchParams.set('ticket', ticket);
  return new WebSocket(url);
}
