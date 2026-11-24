'use client';

import { FormEvent, useEffect, useState } from 'react';
import Link from 'next/link';
import { ArrowLeft, ArrowRight, Eye, EyeOff, LockKeyhole, ShieldCheck, Sparkles } from 'lucide-react';

type Mode = 'sign-in' | 'sign-up' | 'verify';
const roles = ['PASSENGER', 'DRIVER', 'DISPATCHER', 'BIKER', 'DISPATCH_RIDER', 'PARK_OPERATOR'];
const destination: Record<string, string> = { PASSENGER: '/passenger/explore', DRIVER: '/driver/dashboard', DISPATCHER: '/park-operator/terminal', BIKER: '/biker/radar', DISPATCH_RIDER: '/dispatch-rider/deliveries', PARK_OPERATOR: '/park-operator/terminal' };
const api = (process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080/api/v1').replace(/\/$/, '');

export default function Login() {
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [otp, setOtp] = useState('');
  const [role, setRole] = useState('PASSENGER');
  const [mode, setMode] = useState<Mode>('sign-in');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [redirect, setRedirect] = useState('');
  useEffect(() => {
    const next = new URLSearchParams(window.location.search).get('next') || '';
    if (next.startsWith('/') && !next.startsWith('//')) setRedirect(next);
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setMessage('');
    try {
      const path = mode === 'sign-up' ? '/auth/register' : mode === 'verify' ? '/auth/verify-otp' : '/auth/login';
      const body = mode === 'sign-up' ? { email, phone_number: phone, password, name, role } : mode === 'verify' ? { email, code: otp } : { email, password };
      const response = await fetch(`${api}${path}`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
      const result = await response.json();
      if (!response.ok) throw new Error(result.message || result.error || result.detail || 'Request failed. Please try again.');
      if (mode === 'sign-up') { setMode('verify'); setMessage('We sent a six-digit verification code to your email. It expires in 10 minutes.'); return; }
      if (mode === 'verify' || mode === 'sign-in') {
        sessionStorage.setItem('deride_access_token', result.access_token);
        sessionStorage.setItem('deride_refresh_token', result.refresh_token);
        window.location.assign(redirect || destination[result.active_role] || '/passenger/explore');
      }
    } catch (error) { setMessage(error instanceof Error ? error.message : 'Authentication failed.'); }
    finally { setBusy(false); }
  }

  const heading = mode === 'sign-in' ? 'Welcome back' : mode === 'sign-up' ? 'Create your account' : 'Verify your email';

  return <main className="auth-page">
    <section className="auth-visual"><Link href="/" className="auth-brand"><span>DR</span><b>DE <i>RIDE</i><small>MOVE WITH CONFIDENCE</small></b></Link><div className="auth-visual-copy"><span className="auth-overline"><Sparkles size={14}/> YOUR NEXT JOURNEY STARTS HERE</span><h1>More than a ride.<br/><em>A better way to move.</em></h1><p>Plan with confidence, connect with trusted transport teams, and keep every trip detail close at hand.</p><div className="auth-points"><span><ShieldCheck size={17}/> Email-verified accounts</span><span><LockKeyhole size={17}/> Protected sessions</span></div></div><div className="auth-visual-foot">LAGOS <i/> OGUN <i/> ALL NIGERIA</div></section>
    <section className="auth-form-area"><div className="auth-card"><Link href="/" className="auth-back"><ArrowLeft size={16}/> Back to DE RIDE</Link><div className="auth-heading"><span className="auth-overline">{mode === 'verify' ? 'ONE LAST STEP' : mode === 'sign-up' ? 'JOIN THE NETWORK' : 'ACCOUNT ACCESS'}</span><h2>{heading}</h2><p>{mode === 'verify' ? `Enter the verification code sent to ${email || 'your email address'}.` : mode === 'sign-up' ? 'Set up your profile to get started with DE RIDE.' : 'Sign in to continue to your DE RIDE workspace.'}</p></div>
      <form onSubmit={submit} className="auth-form">
        {mode === 'sign-up' && <><label>Full name<input required autoComplete="name" maxLength={120} value={name} onChange={event => setName(event.target.value)} placeholder="Your name"/></label><label>Mobile number<input required type="tel" autoComplete="tel" minLength={10} maxLength={18} value={phone} onChange={event => setPhone(event.target.value)} placeholder="+234 801 234 5678"/></label></>}
        <label>Email address<input required type="email" autoComplete="email" value={email} onChange={event => setEmail(event.target.value)} placeholder="name@example.com"/></label>
        {mode !== 'verify' && <>
          {mode === 'sign-up' && <label>How will you use DE RIDE?<select value={role} onChange={event => setRole(event.target.value)}>{roles.map(item => <option key={item} value={item}>{item.replaceAll('_', ' ').toLowerCase().replace(/\b\w/g, c => c.toUpperCase())}</option>)}</select></label>}
          <label>Password<span className="auth-password"><input required type={showPassword ? 'text' : 'password'} minLength={10} maxLength={128} autoComplete={mode === 'sign-up' ? 'new-password' : 'current-password'} value={password} onChange={event => setPassword(event.target.value)} placeholder={mode === 'sign-up' ? 'At least 10 characters' : 'Enter your password'}/><button type="button" aria-label={showPassword ? 'Hide password' : 'Show password'} onClick={() => setShowPassword(!showPassword)}>{showPassword ? <EyeOff size={17}/> : <Eye size={17}/>}</button></span>{mode === 'sign-up' && <small>Use at least 10 characters.</small>}</label>
        </>}
        {mode === 'verify' && <label>Six-digit verification code<input required inputMode="numeric" pattern="[0-9]{6}" maxLength={6} value={otp} onChange={event => setOtp(event.target.value.replace(/\D/g, '').slice(0, 6))} placeholder="000000" className="otp-input"/></label>}
        <button type="submit" disabled={busy} className="auth-submit">{busy ? 'Please wait…' : mode === 'verify' ? 'Verify and continue' : mode === 'sign-up' ? 'Create account' : 'Sign in'} {!busy && <ArrowRight size={17}/>}</button>
      </form>
      {message && <p className={message.startsWith('We sent') ? 'auth-message success' : 'auth-message'} role="status" aria-live="polite">{message}</p>}
      {mode !== 'verify' ? <p className="auth-switch">{mode === 'sign-in' ? 'New to DE RIDE?' : 'Already have an account?'} <button type="button" onClick={() => { setMode(mode === 'sign-in' ? 'sign-up' : 'sign-in'); setMessage(''); }}>{mode === 'sign-in' ? 'Create account' : 'Sign in'}</button></p> : <p className="auth-switch"><button type="button" onClick={() => { setMode('sign-up'); setMessage(''); }}>Back to registration</button></p>}
      <div className="auth-legal"><LockKeyhole size={13}/> Your session is protected with rotating credentials. <Link href="mailto:elversetechnologies@gmail.com">Need help?</Link></div>
    </div></section>
  </main>;
}
