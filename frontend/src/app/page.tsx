'use client';

const portals = [
  { name: 'Passenger', detail: 'Find parks, book seats and keep your tickets close.', href: '/passenger/explore', icon: '↗', accent: 'coral', tag: 'TRAVELER' },
  { name: 'Bus driver', detail: 'Manage your trip, boarding and passenger manifest.', href: '/driver/dashboard', icon: '▰', accent: 'teal', tag: 'BUS & ROUTE' },
  { name: 'Biker', detail: 'See passenger requests and manage active rides.', href: '/biker/radar', icon: '⌁', accent: 'mint', tag: 'LAST MILE' },
  { name: 'Dispatch rider', detail: 'Manage parcel deliveries and proof of handoff.', href: '/dispatch-rider/deliveries', icon: '▱', accent: 'amber', tag: 'DELIVERY' },
  { name: 'Park operator', detail: 'Coordinate terminal bays, queues and departures.', href: '/park-operator/terminal', icon: '▦', accent: 'teal', tag: 'TERMINAL' },
  { name: 'Admin', detail: 'Open the restricted DE RIDE operations workspace.', href: '/admin/command-map', icon: '⌖', accent: 'coral', tag: 'OPERATIONS' },
];

const launchHubs = ['Lagos', 'Ogun'];

export default function Home() {
  function readPageAloud() {
    if (typeof window === 'undefined' || !('speechSynthesis' in window)) return;
    window.speechSynthesis.cancel();
    const speech = new SpeechSynthesisUtterance('Your next move starts here. One place for bus travel, bike rides and parcel dispatch. Choose your workspace to get started.');
    const language = (document.querySelector('select[aria-label="Choose language"]') as HTMLSelectElement | null)?.value ?? 'EN';
    speech.lang = ({ EN: 'en-NG', YO: 'yo-NG', HA: 'ha-NG', IG: 'ig-NG', PCM: 'en-NG' } as Record<string, string>)[language] ?? 'en-NG';
    window.speechSynthesis.speak(speech);
  }

  return (
    <main className="home-shell">
      <header className="topbar">
        <a className="brand" href="/" aria-label="DE RIDE home">
          <span className="brand-mark" aria-hidden="true">D</span>
          <span>DE <b>RIDE</b><small>MOVE WITH CONFIDENCE</small></span>
        </a>
        <div className="top-actions">
          <label className="language-picker"><span className="sr-only">Language</span><select defaultValue="EN" aria-label="Choose language"><option>EN</option><option>YO</option><option>HA</option><option>IG</option><option>PCM</option></select></label>
          <button className="voice-button" type="button" onClick={readPageAloud} aria-label="Read page aloud" title="Read page aloud">◖))</button>
          <a className="sign-in-link" href="/login">Sign in <span aria-hidden="true">→</span></a>
        </div>
      </header>

      <section className="hero" aria-labelledby="hero-title">
        <div className="hero-copy">
          <div className="eyebrow"><span className="eyebrow-dot" /> NIGERIA, ON THE MOVE</div>
          <h1 id="hero-title">Your next move<br /><span>starts here.</span></h1>
          <p>One place for bus travel, bike rides and parcel dispatch. Choose your workspace to get started.</p>
          <div className="hero-actions"><a className="button-primary" href="/passenger/explore">Explore DE RIDE <span>↗</span></a><a className="text-link" href="/login">Already have an account?</a></div>
          <div className="hub-note"><span className="hub-icon">⌖</span><span><b>Launching in Lagos &amp; Ogun</b><small>Built for journeys across all 36 states and the FCT</small></span></div>
        </div>
        <div className="route-art" aria-label="Illustration of a connected route across Nigeria">
          <div className="route-orbit orbit-one" /><div className="route-orbit orbit-two" />
          <div className="route-line" />
          <span className="map-pin pin-lagos"><i />LAGOS</span><span className="map-pin pin-ogun"><i />OGUN</span>
          <div className="route-center"><span className="route-center-icon">↗</span><b>GO YOUR WAY</b><small>One ride at a time</small></div>
          <span className="route-coordinate">06° 27′ N<br />03° 23′ E</span>
          <span className="route-label">YOUR CITY. YOUR JOURNEY.</span>
        </div>
      </section>

      <section className="portal-section" aria-labelledby="portal-title">
        <div className="section-heading"><div><div className="eyebrow muted-eyebrow">ONE PLATFORM · SIX WORKSPACES</div><h2 id="portal-title">Where are you headed?</h2></div><span className="section-aside">Choose a workspace <span aria-hidden="true">↓</span></span></div>
        <div className="portal-grid">
          {portals.map((portal, index) => <a className={`portal-card accent-${portal.accent}`} href={portal.href} key={portal.href}>
            <div className="portal-card-top"><span className="portal-icon" aria-hidden="true">{portal.icon}</span><span className="portal-index">0{index + 1}</span></div>
            <span className="portal-tag">{portal.tag}</span><h3>{portal.name}</h3><p>{portal.detail}</p>
            <span className="portal-arrow" aria-hidden="true">↗</span>
          </a>)}
        </div>
      </section>

      <footer className="home-footer"><a className="footer-brand" href="/">DE RIDE</a><span>Connecting people, parks and possibilities.</span><div className="footer-hubs">{launchHubs.map(hub => <span key={hub}><i />{hub}</span>)}</div></footer>
    </main>
  );
}
