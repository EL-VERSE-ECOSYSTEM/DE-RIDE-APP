export type GeocodedPlace = {
  label: string;
  longitude: number;
  latitude: number;
  city?: string;
  state?: string;
  country?: string;
};

type PhotonFeature = {
  geometry?: { coordinates?: [number, number] };
  properties?: { name?: string; street?: string; housenumber?: string; district?: string; city?: string; county?: string; state?: string; country?: string };
};

function placeFromPhoton(feature: PhotonFeature): GeocodedPlace | null {
  const coordinates = feature.geometry?.coordinates;
  const props = feature.properties;
  if (!coordinates || !props || !Number.isFinite(coordinates[0]) || !Number.isFinite(coordinates[1])) return null;
  const label = [props.name, props.street && `${props.housenumber ?? ''} ${props.street}`.trim(), props.district, props.city ?? props.county, props.state]
    .filter((value, index, values): value is string => Boolean(value) && values.indexOf(value) === index).join(', ');
  return { label: label || props.country || 'Selected place', longitude: coordinates[0], latitude: coordinates[1], city: props.city ?? props.county, state: props.state, country: props.country };
}

export async function searchPhoton(query: string, signal?: AbortSignal): Promise<GeocodedPlace[]> {
  const language = typeof document === 'undefined' ? 'en' : document.documentElement.lang.split('-')[0];
  const params = new URLSearchParams({ q: query, lang: language });
  const response = await fetch(`/api/geocoding/photon?${params}`, { signal, headers: { Accept: 'application/json' } });
  if (!response.ok) throw new Error('Photon search is unavailable');
  const result = await response.json() as { features?: PhotonFeature[] };
  return (result.features ?? []).map(placeFromPhoton).filter((place): place is GeocodedPlace => place !== null);
}

type NominatimResult = { lat: string; lon: string; display_name: string; address?: { city?: string; town?: string; village?: string; county?: string; state?: string; country?: string } };
const nominatimCacheKey = 'deride:nominatim:v1';
let lastNominatimRequest = 0;

async function cachedNominatim<T>(url: URL, signal?: AbortSignal): Promise<T> {
  const key = `${nominatimCacheKey}:${url.toString()}`;
  try {
    const cached = localStorage.getItem(key);
    if (cached) return JSON.parse(cached) as T;
  } catch { /* Storage can be unavailable in private browsing. */ }
  const wait = Math.max(0, 1100 - (Date.now() - lastNominatimRequest));
  if (wait) await new Promise(resolve => window.setTimeout(resolve, wait));
  lastNominatimRequest = Date.now();
  const response = await fetch(url, { signal, headers: { Accept: 'application/json' } });
  if (!response.ok) throw new Error('Nominatim lookup is unavailable');
  const result = await response.json() as T;
  try { localStorage.setItem(key, JSON.stringify(result)); } catch { /* Cache remains best-effort. */ }
  return result;
}

export async function searchNominatimStructured(destination: string, state: string, signal?: AbortSignal): Promise<GeocodedPlace | null> {
  const params = new URLSearchParams({ q: `${destination}, ${state === 'All 36 States + FCT' ? 'Nigeria' : state}, Nigeria`, format: 'jsonv2', addressdetails: '1', limit: '1' });
  const results = await cachedNominatim<NominatimResult[]>(new URL(`/api/geocoding/nominatim/search?${params}`, window.location.origin), signal);
  const result = results[0];
  if (!result) return null;
  const address = result.address ?? {};
  return { label: result.display_name, latitude: Number(result.lat), longitude: Number(result.lon), city: address.city ?? address.town ?? address.village ?? address.county, state: address.state, country: address.country };
}

export async function reverseNominatim(latitude: number, longitude: number, signal?: AbortSignal): Promise<GeocodedPlace | null> {
  const params = new URLSearchParams({ lat: String(latitude), lon: String(longitude) });
  const url = new URL(`/api/geocoding/nominatim/reverse?${params}`, window.location.origin);
  const result = await cachedNominatim<NominatimResult>(url, signal);
  if (!result?.display_name) return null;
  const address = result.address ?? {};
  return { label: result.display_name, latitude: Number(result.lat), longitude: Number(result.lon), city: address.city ?? address.town ?? address.village ?? address.county, state: address.state, country: address.country };
}
