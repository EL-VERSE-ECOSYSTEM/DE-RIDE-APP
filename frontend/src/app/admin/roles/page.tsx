'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { ArrowLeft, ShieldCheck, UserRoundCheck } from 'lucide-react';
import { apiRequest } from '@/lib/api-client';

type Request = { id: string; user_id: string; display_name: string; email: string; role: string; requested_at: string };

export default function AdminRolesPage() {
  const [items, setItems] = useState<Request[]>([]);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState('');
  const [scope, setScope] = useState<Record<string, { park_id: string; trip_id: string }>>({});

  async function load() {
    const response = await apiRequest('/admin/role-requests');
    const data = await response.json();
    if (!response.ok) throw new Error(data.message || data.error || 'Role queue unavailable.');
    setItems(data.requests || []);
  }

  useEffect(() => { void load().catch((e) => setError(e.message)); }, []);

  async function review(item: Request, decision: 'APPROVED' | 'REJECTED') {
    setBusy(item.id); setError(''); setNotice('');
    const response = await apiRequest(`/admin/role-requests/${item.id}/review`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ decision, note: decision === 'REJECTED' ? 'Role request declined by administrator.' : 'Role request approved.', ...(scope[item.id] || {}) }),
    });
    const data = await response.json(); setBusy('');
    if (!response.ok) { setError(data.message || data.error || 'Review could not be saved.'); return; }
    setNotice(`${item.role} request ${decision.toLowerCase()}.`);
    await load();
  }

  return <main className="transaction-page"><header className="transaction-header"><Link href="/admin/command-map" className="wallet-brand"><span>DR</span> DE <i>RIDE</i></Link><Link href="/admin/command-map" className="transaction-back"><ArrowLeft size={15}/> Admin</Link></header><section className="transaction-hero"><div className="transaction-kicker"><ShieldCheck size={15}/> ACCESS CONTROL</div><h1>Role requests</h1><p>Operational roles require verified identity or an explicit park or trip assignment. Every decision is recorded.</p></section>{error && <p className="wallet-alert" role="alert">{error}</p>}{notice && <p className="wallet-success" role="status">{notice}</p>}<section className="wallet-card support-list">{items.length ? items.map((item) => <article className="dispute-card" key={item.id}><div className="wallet-card-heading"><span><b>{item.display_name} · {item.role.replaceAll('_', ' ')}</b><small>{item.email} · Requested {new Date(item.requested_at).toLocaleString()}</small></span></div>{['DISPATCHER','PARK_OPERATOR'].includes(item.role) && <div className="support-admin-controls">{item.role === 'DISPATCHER' && <label>Trip assignment (optional)<input value={scope[item.id]?.trip_id || ''} onChange={(e) => setScope((old) => ({ ...old, [item.id]: { ...old[item.id], trip_id: e.target.value } }))}/></label>}<label>Park assignment<input value={scope[item.id]?.park_id || ''} onChange={(e) => setScope((old) => ({ ...old, [item.id]: { ...old[item.id], park_id: e.target.value } }))}/></label></div>}<div className="dispute-actions"><button className="wallet-primary" disabled={busy === item.id} onClick={() => void review(item, 'APPROVED')}><UserRoundCheck size={15}/> Approve</button><button className="wallet-secondary" disabled={busy === item.id} onClick={() => void review(item, 'REJECTED')}>Decline</button></div></article>) : <div className="wallet-empty">No pending role requests.</div>}</section></main>;
}
