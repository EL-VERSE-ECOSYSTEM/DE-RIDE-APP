const DB_NAME = 'deride-offline-v1';
const STORE = 'ticket-keys';

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1);
    request.onupgradeneeded = () => request.result.createObjectStore(STORE);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

export async function cacheQRPublicKey(pem: string, keyId = 'active'): Promise<void> {
  const db = await openDB();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction(STORE, 'readwrite');
    tx.objectStore(STORE).put({pem,key_id:keyId}, 'active');
    tx.objectStore(STORE).put(pem, `key:${keyId}`);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
  db.close();
}

async function readQRPublicKey(keyId?: string): Promise<string> {
  const configured = process.env.NEXT_PUBLIC_QR_PUBLIC_KEY_PEM;
  if (configured) {
    const pem = configured.replace(/\\n/g, '\n');
    await cacheQRPublicKey(pem, process.env.NEXT_PUBLIC_QR_KEY_ID || 'active');
    return pem;
  }
  const db = await openDB();
  const pem = await new Promise<string>((resolve, reject) => {
    const store=db.transaction(STORE, 'readonly').objectStore(STORE);
    const request = store.get(keyId?`key:${keyId}`:'active');
    request.onsuccess = () => {const value=request.result;if(typeof value==='string')resolve(value);else if(value?.pem)resolve(value.pem as string);else reject(new Error('Offline QR public key is not cached'));};
    request.onerror = () => reject(request.error);
  });
  db.close();
  return pem;
}

function decodeBase64URL(value: string): Uint8Array {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
  const binary = atob(normalized + '='.repeat((4 - normalized.length % 4) % 4));
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}

function toArrayBuffer(value: Uint8Array): ArrayBuffer {
  const buffer = new ArrayBuffer(value.byteLength);
  new Uint8Array(buffer).set(value);
  return buffer;
}

export type SignedTicket = { payload: Record<string, unknown>; signature: string; key_id?: string };

function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(',')}]`;
  if (value && typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>).sort(([a], [b]) => a.localeCompare(b));
    return `{${entries.map(([key, item]) => `${JSON.stringify(key)}:${canonicalJSON(item)}`).join(',')}}`;
  }
  return JSON.stringify(value) ?? 'null';
}

export async function verifyOfflineTicket(ticket: SignedTicket): Promise<boolean> {
  const pem = await readQRPublicKey(ticket.key_id);
  const encoded = pem.replace(/-----BEGIN PUBLIC KEY-----|-----END PUBLIC KEY-----|\s/g, '');
  const keyBytes = Uint8Array.from(atob(encoded), (c) => c.charCodeAt(0));
  const key = await crypto.subtle.importKey('spki', toArrayBuffer(keyBytes), { name: 'ECDSA', namedCurve: 'P-256' }, false, ['verify']);
  const canonical = toArrayBuffer(new TextEncoder().encode(canonicalJSON(ticket.payload)));
  const expiresAt = ticket.payload.exp;
  if (typeof expiresAt !== 'number' || expiresAt <= Math.floor(Date.now() / 1000)) return false;
  return crypto.subtle.verify({ name: 'ECDSA', hash: 'SHA-256' }, key, toArrayBuffer(decodeBase64URL(ticket.signature)), canonical);
}

export function isValidManualTicketCode(code: string): boolean {
  const match = /^DR-([A-Z]{2})-(\d{4})-(\d{2})$/.exec(code.trim().toUpperCase());
  if (!match) return false;
  const [, state, digits, checksum] = match;
  let value = 0;
  for (const char of `${state}${digits}`) value = (value * 31 + char.charCodeAt(0)) % 97;
  return String(value).padStart(2, '0') === checksum;
}

export function createManualTicketCode(state: string, digits: string): string {
  if (!/^[A-Z]{2}$/.test(state) || !/^\d{4}$/.test(digits)) throw new Error('State and four digit ticket number are required');
  let value = 0;
  for (const char of `${state}${digits}`) value = (value * 31 + char.charCodeAt(0)) % 97;
  return `DR-${state}-${digits}-${String(value).padStart(2, '0')}`;
}

export async function queueOfflineScan(code: string): Promise<void> {
  const db = await openDB();
  await new Promise<void>((resolve, reject) => {
    const tx = db.transaction('ticket-keys', 'readwrite');
    tx.objectStore('ticket-keys').put({ code, scanned_at: new Date().toISOString() }, `scan:${crypto.randomUUID()}`);
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
  });
  db.close();
}

export type OfflineScan = { code: string; scanned_at: string };
export async function readOfflineScans(): Promise<{key: IDBValidKey; scan: OfflineScan}[]> {
  const db = await openDB();
  const scans = await new Promise<{key: IDBValidKey; scan: OfflineScan}[]>((resolve, reject) => {
    const tx=db.transaction(STORE,'readonly'), store=tx.objectStore(STORE), keys=store.getAllKeys(), values=store.getAll();
    tx.oncomplete=()=>resolve(keys.result.flatMap((key,index)=>String(key).startsWith('scan:')?[{key,scan:values.result[index] as OfflineScan}]:[]));
    tx.onerror=()=>reject(tx.error);
  });
  db.close(); return scans;
}
export async function removeOfflineScan(key: IDBValidKey): Promise<void> {
  const db=await openDB(); await new Promise<void>((resolve,reject)=>{const tx=db.transaction(STORE,'readwrite');tx.objectStore(STORE).delete(key);tx.oncomplete=()=>resolve();tx.onerror=()=>reject(tx.error)});db.close();
}
