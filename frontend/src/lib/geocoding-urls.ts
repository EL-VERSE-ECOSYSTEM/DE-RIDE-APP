const PUBLIC_PHOTON = 'https://photon.komoot.io/api/';
const PUBLIC_NOMINATIM = 'https://nominatim.openstreetmap.org';

function geocoderOrigin(): string | null {
  const configured = process.env.GEOCODER_DOMAIN?.trim();
  if (!configured) return null;

  try {
    const localHost = /^(localhost|127\.0\.0\.1)(:\d+)?$/i.test(configured);
    const candidate = /^[a-z][a-z\d+.-]*:\/\//i.test(configured)
      ? configured
      : `${localHost ? 'http' : 'https'}://${configured}`;
    const url = new URL(candidate);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return null;
    if (url.pathname !== '/' || url.search || url.hash) return null;
    return url.origin;
  } catch {
    return null;
  }
}

export function photonSearchUrl(query: string, language = 'en'): URL {
  const origin = geocoderOrigin();
  const url = new URL(origin ? `${origin}/photon/api/` : PUBLIC_PHOTON);
  const supportedLanguage = /^[a-z]{2,3}$/i.test(language) ? language.toLowerCase() : 'en';
  url.search = new URLSearchParams({ q: query, limit: '6', lang: supportedLanguage, countrycode: 'NG' }).toString();
  return url;
}

export function nominatimUrl(operation: 'search' | 'reverse', params: URLSearchParams): URL {
  const origin = geocoderOrigin();
  const base = origin ? `${origin}/nominatim` : PUBLIC_NOMINATIM;
  const url = new URL(`${base}/${operation}`);
  url.search = params.toString();
  return url;
}
