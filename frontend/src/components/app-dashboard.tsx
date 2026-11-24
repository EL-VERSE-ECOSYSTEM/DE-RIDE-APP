'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { ArrowDownLeft, ArrowUpRight, Bell, BusFront, CircleHelp, Clock3, Compass, LogOut, MapPin, Package, ShieldCheck, Sparkles, Wallet, Smartphone, MessageCircle } from 'lucide-react';
import { AccountProfile, apiRequest, signOut } from '@/lib/api-client';
import { RiderModeSwitcher } from '@/components/rider-mode-switcher';

type Entry = { id: string; type: string; amount_ngn: number; created_at: string };
type Park = { id: string; name: string; state_code: string; launch_enabled: boolean; latitude: number | null; longitude: number | null };
type RoleConfig = { title: string; subtitle: string; icon: typeof BusFront; links: { label: string; href: string; icon: typeof MapPin; note: string }[] };

const configs: Record<string, RoleConfig> = {
  PASSENGER: { title: 'Passenger dashboard', subtitle: 'Your journeys, account and travel tools in one place.', icon: Compass, links: [
    { label: 'Explore registered parks', href: '/passenger/explore', icon: MapPin, note: 'Browse park locations and launch hubs' },
    { label: 'Book bus tickets', href: '/tickets', icon: BusFront, note: 'Find trips, choose a seat and check out' },
    { label: 'Bike & parcel delivery', href: '/mobility', icon: Package, note: 'Request a bike ride or send a parcel' },
    { label: 'Buy mobile data', href: '/data', icon: Smartphone, note: 'Browse MTN, Airtel, Glo and 9mobile bundles' },
    { label: 'Amebo AI', href: '/amebo', icon: Sparkles, note: 'Travel chat and rollover token packs' },
    { label: 'Trip support', href: '/support', icon: CircleHelp, note: 'Get help with a booking or account' },
  ] },
  DRIVER: { title: 'Driver dashboard', subtitle: 'Keep your profile ready for assigned trips and service updates.', icon: BusFront, links: [
    { label: 'Driver workspace', href: '/driver/dashboard', icon: BusFront, note: 'Trip and vehicle tools' },
    { label: 'Create a bus trip', href: '/driver/trips', icon: BusFront, note: 'Publish a trip using an approved bus' },
    { label: 'Boarding scanner', href: '/driver/scan', icon: ShieldCheck, note: 'Scan signed QR tickets or check manual codes' },
    { label: 'Buy mobile data', href: '/data', icon: Smartphone, note: 'Purchase a bundle with your wallet' },
    { label: 'Amebo AI', href: '/amebo', icon: Sparkles, note: 'Travel chat and rollover token packs' },
    { label: 'Safety and support', href: '/support', icon: ShieldCheck, note: 'Contact the DE RIDE team' },
  ] },
  BIKER: { title: 'Biker dashboard', subtitle: 'Your rider profile, account balance and service access.', icon: Compass, links: [
    { label: 'Biker radar', href: '/biker/radar', icon: MapPin, note: 'Open your rider workspace' },
    { label: 'Buy mobile data', href: '/data', icon: Smartphone, note: 'Purchase a bundle with your wallet' },
    { label: 'Amebo AI', href: '/amebo', icon: Sparkles, note: 'Travel chat and rollover token packs' },
    { label: 'Switch rider mode', href: '/biker/radar', icon: ArrowUpRight, note: 'Available after both rider profiles are approved' },
  ] },
  DISPATCH_RIDER: { title: 'Dispatch rider dashboard', subtitle: 'Your delivery profile and account at a glance.', icon: Package, links: [
    { label: 'Deliveries', href: '/dispatch-rider/deliveries', icon: Package, note: 'Open the dispatch workspace' },
    { label: 'Buy mobile data', href: '/data', icon: Smartphone, note: 'Purchase a bundle with your wallet' },
    { label: 'Amebo AI', href: '/amebo', icon: Sparkles, note: 'Travel chat and rollover token packs' },
    { label: 'Rider support', href: '/support', icon: CircleHelp, note: 'Contact the DE RIDE team' },
  ] },
  DISPATCHER: { title: 'Dispatcher dashboard', subtitle: 'Terminal operations and account information.', icon: BusFront, links: [
    { label: 'Terminal workspace', href: '/park-operator/terminal', icon: MapPin, note: 'Open assigned terminal tools' },
    { label: 'Operations support', href: '/support', icon: CircleHelp, note: 'Contact the DE RIDE team' },
  ] },
  PARK_OPERATOR: { title: 'Park operator dashboard', subtitle: 'Terminal operations and account information.', icon: BusFront, links: [
    { label: 'Terminal workspace', href: '/park-operator/terminal', icon: MapPin, note: 'Open park management tools' },
    { label: 'Operations support', href: '/support', icon: CircleHelp, note: 'Contact the DE RIDE team' },
  ] },
  ADMIN: { title: 'Admin dashboard', subtitle: 'Account and command tools for DE RIDE administration.', icon: ShieldCheck, links: [
    { label: 'Command map', href: '/admin/command-map', icon: MapPin, note: 'Open administration tools' },
    { label: 'Role requests', href: '/admin/roles', icon: ShieldCheck, note: 'Review and assign operational access' },
    { label: 'Support queue', href: '/admin/support', icon: CircleHelp, note: 'Reply to customer cases' },
    { label: 'Disputes', href: '/admin/disputes', icon: ShieldCheck, note: 'Review and resolve customer disputes' },
    { label: 'Safety escalations', href: '/admin/escalations', icon: MessageCircle, note: 'Join urgent trip mediation' },
  ] },
};
const roleDestinations: Record<string, string> = { PASSENGER:'/passenger/explore', DRIVER:'/driver/dashboard', DISPATCHER:'/park-operator/terminal', PARK_OPERATOR:'/park-operator/terminal', BIKER:'/biker/radar', DISPATCH_RIDER:'/dispatch-rider/deliveries', ADMIN:'/admin/command-map' };

const money = (amount: number) => new Intl.NumberFormat('en-NG', { style: 'currency', currency: 'NGN', maximumFractionDigits: 0 }).format(amount);
const nigerianStates=['AB','AD','AK','AN','BA','BY','BE','BO','CR','DE','EB','ED','EK','EN','GO','IM','JI','KD','KN','KT','KE','KO','KW','LA','NA','NI','OG','ON','OS','OY','PL','RI','SO','TA','YO','ZA','FC'];

export function AppDashboard({ requiredRole }: { requiredRole?: string }) {
  const path = usePathname();
  const [profile, setProfile] = useState<AccountProfile | null>(null);
  const [entries, setEntries] = useState<Entry[]>([]);
  const [parks, setParks] = useState<Park[]>([]);
  const [parkSearch, setParkSearch] = useState('');
  const [parkState, setParkState] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [gpsSaving,setGpsSaving]=useState(false);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const response = await apiRequest('/me');
        if (response.status === 401) { window.location.assign(`/login?next=${encodeURIComponent(path)}`); return; }
        const data = await response.json();
        if (!response.ok) throw new Error(data.message || 'We could not load your account.');
        if (!alive) return;
        setProfile(data as AccountProfile);
        const account = data as AccountProfile;
        const allowedForRoute = !requiredRole || account.active_role === requiredRole || account.roles.includes(requiredRole) || (requiredRole === 'BIKER' && account.biker_approved) || (requiredRole === 'DISPATCH_RIDER' && account.dispatch_approved);
        if (!allowedForRoute) { window.location.replace(roleDestinations[account.active_role] || '/'); return; }
        const ledger = await apiRequest('/wallet/transactions');
        if (ledger.ok) {
          const tx = await ledger.json();
          if (alive) setEntries(tx.transactions || []);
        }
        if (account.active_role === 'PASSENGER') {
          const parkResponse = await apiRequest('/parks');
          if (parkResponse.ok) {
            const parkData = await parkResponse.json();
            if (alive) setParks(parkData.parks || []);
          }
        }
      } catch (err) {
        if (alive) setError(err instanceof Error ? err.message : 'The account service is unavailable.');
      } finally { if (alive) setLoading(false); }
    })();
    return () => { alive = false; };
  }, [path, requiredRole]);

  async function handleSignOut() {
    await signOut();
    window.location.assign('/');
  }

  async function updateGPSOptIn(enabled:boolean){if(enabled&&!window.confirm('Allow DE RIDE to use your location during active trips? You pay for the mobile data and battery used by GPS sharing.'))return;setGpsSaving(true);setError('');try{const response=await apiRequest('/me/preferences',{method:'PATCH',headers:{'Content-Type':'application/json'},body:JSON.stringify({gps_opt_in:enabled})});const result=await response.json();if(!response.ok)throw new Error(result.message||result.error||'Location setting could not be saved.');setProfile(current=>current?{...current,gps_opt_in:enabled}:current)}catch(e){setError(e instanceof Error?e.message:'Could not save location setting.')}finally{setGpsSaving(false)}}

  const role = profile?.active_role || requiredRole || 'PASSENGER';
  const config = configs[role] || configs.PASSENGER;
  const Icon = config.icon;
  const visibleParks = parks.filter(park => (!parkState || park.state_code===parkState) && `${park.name} ${park.state_code}`.toLowerCase().includes(parkSearch.trim().toLowerCase()));

  return <main className="account-app">
    <aside className="account-rail">
      <Link href="/" className="account-brand"><span>DR</span><b>DE RIDE<small>MOVE WITH CONFIDENCE</small></b></Link>
      <div className="account-nav-label">YOUR WORKSPACE</div>
      <Link className="account-nav-active" href={path || '/'}><Icon size={18}/>{config.title}</Link>
      <Link className="account-nav-link" href="/"><Compass size={18}/>Back to explore</Link>
      <Link className="account-nav-link" href="/wallet"><Wallet size={18}/>Wallet</Link>
      <Link className="account-nav-link" href="/subscriptions"><Sparkles size={18}/>Subscriptions</Link>
      <Link className="account-nav-link" href="/data"><Smartphone size={18}/>Buy data</Link>
      <Link className="account-nav-link" href="/amebo"><Sparkles size={18}/>Amebo AI</Link>
      <Link className="account-nav-link" href="/messages"><MessageCircle size={18}/>Trip chat</Link>
      <Link className="account-nav-link" href="/transactions"><ArrowDownLeft size={18}/>Transactions</Link>
      <Link className="account-nav-link" href="/notifications"><Bell size={18}/>Notifications</Link>
      <div className="account-rail-bottom"><ShieldCheck size={17}/><span>Secure account<br/><small>Session protected</small></span></div>
    </aside>
    <section className="account-main">
      <header className="account-topbar"><div><span className="account-breadcrumb">DE RIDE <span>/</span> ACCOUNT</span><h1>{config.title}</h1></div><div className="account-top-actions"><span className="account-live"><i/> Account services</span><button className="account-logout" onClick={handleSignOut}><LogOut size={16}/>Sign out</button></div></header>
      <div className="account-content">
        {loading && <div className="account-loading"><span className="account-spinner"/>Loading your secure account…</div>}
        {error && !loading && <div className="account-alert" role="alert"><CircleHelp size={18}/><span>{error}<small>Check the API connection and try again.</small></span></div>}
        {profile && <>
          <section className="account-welcome"><div><div className="account-kicker">WELCOME BACK</div><h2>{profile.display_name || profile.email.split('@')[0]}</h2><p>{config.subtitle}</p></div><div className="account-avatar">{(profile.display_name || profile.email).slice(0,1).toUpperCase()}</div></section>
          <div className="account-stats">
            <article className="account-stat account-wallet"><div><span>WALLET BALANCE</span><Wallet size={18}/></div><strong>{money(profile.wallet_balance_ngn || 0)}</strong><small>Available in your DE RIDE wallet</small></article>
            <article className="account-stat"><div><span>ACCOUNT ROLE</span><Icon size={18}/></div><strong className="stat-text">{role.replaceAll('_', ' ')}</strong><small>{profile.email_verified ? 'Email verified' : 'Email verification required'}</small></article>
            <article className="account-stat"><div><span>AMEBO TOKENS</span><Sparkles size={18}/></div><strong>{(profile.amebo_free_tokens || 0) + (profile.amebo_purchased_tokens || 0)}</strong><small>{profile.amebo_free_tokens || 0} daily · {profile.amebo_purchased_tokens || 0} purchased</small></article>
          </div>
          <div className="account-columns">
            <section className="account-panel"><div className="account-panel-heading"><div><span className="account-kicker">QUICK ACCESS</span><h3>Where do you want to go?</h3></div><ArrowUpRight size={19}/></div><div className="account-shortcuts">{config.links.map((link) => { const ItemIcon = link.icon; return <Link key={`${link.href}-${link.label}`} href={link.href} className="account-shortcut"><span className="shortcut-icon"><ItemIcon size={19}/></span><span><b>{link.label}</b><small>{link.note}</small></span><ArrowUpRight size={16}/></Link>; })}</div></section>
            <section className="account-panel account-security"><div className="account-panel-heading"><div><span className="account-kicker">ACCOUNT HEALTH</span><h3>Security & verification</h3></div><ShieldCheck size={19}/></div><div className="account-check-row"><span className={profile.email_verified ? 'check-ok' : 'check-pending'}><ShieldCheck size={16}/></span><span><b>Email verification</b><small>{profile.email_verified ? 'Verified and ready' : 'Complete email verification to use account services'}</small></span><strong>{profile.email_verified ? 'VERIFIED' : 'PENDING'}</strong></div><div className="account-check-row"><span className="check-ok"><Clock3 size={16}/></span><span><b>Session management</b><small>Refresh token rotates when your session renews</small></span><strong>ACTIVE</strong></div><div className="account-check-row"><span className={profile.gps_opt_in ? 'check-ok' : 'check-pending'}><MapPin size={16}/></span><span><b>Location sharing</b><small>{profile.gps_opt_in ? 'You enabled GPS sharing' : 'GPS sharing is off'}</small></span><strong>{profile.gps_opt_in ? 'ON' : 'OFF'}</strong></div><button className="account-shortcut" disabled={gpsSaving} onClick={()=>void updateGPSOptIn(!profile.gps_opt_in)}>{gpsSaving?'Saving…':profile.gps_opt_in?'Turn off GPS permission':'Enable GPS permission'}<small>Active telemetry uses your mobile data and battery.</small></button></section>
          </div>
          {role === 'PASSENGER' && <section className="account-panel passenger-parks"><div className="account-panel-heading"><div><span className="account-kicker">PARK DIRECTORY</span><h3>Explore registered parks</h3></div><span className="park-count">{parks.length} locations</span></div><label className="park-search"><MapPin size={16}/><input value={parkSearch} onChange={event => setParkSearch(event.target.value)} placeholder="Search park or state" aria-label="Search parks by name or state"/><select aria-label="Filter parks by state" value={parkState} onChange={event=>setParkState(event.target.value)}><option value="">All states + FCT</option>{nigerianStates.map(code=><option key={code} value={code}>{code}</option>)}</select></label>{visibleParks.length ? <div className="park-list">{visibleParks.slice(0,8).map(park => <article className="park-row" key={park.id}><span className="park-pin"><MapPin size={16}/></span><span><b>{park.name}</b><small>{park.state_code} · {park.launch_enabled ? 'Lagos/Ogun launch hub' : 'Registered location'}</small></span>{park.latitude != null && park.longitude != null && <a href={`https://www.openstreetmap.org/?mlat=${park.latitude}&mlon=${park.longitude}#map=15/${park.latitude}/${park.longitude}`} target="_blank" rel="noreferrer" aria-label={`View ${park.name} on map`}><ArrowUpRight size={16}/></a>}</article>)}</div> : <div className="account-empty"><MapPin size={21}/><span>{parks.length ? 'No parks match that search' : 'No park locations are available yet'}<small>Park details appear here when they are provisioned in the DE RIDE service.</small></span></div>}</section>}
          {profile.biker_approved && profile.dispatch_approved && <section className="account-panel rider-mode-panel"><div><span className="account-kicker">DUAL RIDER PROFILE</span><h3>Choose your active rider mode</h3><p>Mode changes are disabled while you have an active ride or delivery.</p></div><RiderModeSwitcher target={role === 'BIKER' ? 'DISPATCH_RIDER' : 'BIKER'}/></section>}
          <section className="account-panel account-activity"><div className="account-panel-heading"><div><span className="account-kicker">WALLET</span><h3>Recent activity</h3></div><span className="account-safe-note"><ShieldCheck size={14}/> Ledger-backed</span></div>{entries.length ? <div className="ledger-list">{entries.map(entry => <div className="ledger-row" key={entry.id}><span className="ledger-icon"><ArrowDownLeft size={17}/></span><span><b>{entry.type.replaceAll('_', ' ')}</b><small>{new Date(entry.created_at).toLocaleString()}</small></span><strong className={entry.amount_ngn >= 0 ? 'ledger-positive' : 'ledger-negative'}>{entry.amount_ngn >= 0 ? '+' : ''}{money(entry.amount_ngn)}</strong></div>)}</div> : <div className="account-empty"><Wallet size={21}/><span>No wallet activity yet<small>Completed wallet activity will appear here.</small></span></div>}</section>
          <footer className="account-footer"><span>DE RIDE · Account data is loaded securely from your service.</span><Link href="mailto:elversetechnologies@gmail.com">Need help? Contact support <ArrowUpRight size={13}/></Link></footer>
        </>}
      </div>
    </section>
  </main>;
}
