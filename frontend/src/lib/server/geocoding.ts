import { NextResponse } from 'next/server';

const APP_URL = process.env.NEXT_PUBLIC_APP_URL || 'https://de-ride-app.onrender.com';

export async function forwardGeocodingRequest(upstream: URL, kind: 'photon' | 'nominatim', signal?: AbortSignal) {
  try {
    const headers: HeadersInit = { Accept: 'application/json' };
    if (kind === 'nominatim') {
      // The browser cannot set User-Agent. Make the request from the server and
      // identify this app as required by the public Nominatim service policy.
      headers['User-Agent'] = `DE-RIDE-Web/1.0 (+${APP_URL})`;
    }
    const response = await fetch(upstream, { headers, signal });
    if (!response.ok) {
      return NextResponse.json({ error: `${kind} lookup failed` }, { status: 502 });
    }
    return new Response(await response.arrayBuffer(), {
      status: 200,
      headers: {
        'Content-Type': response.headers.get('content-type') || 'application/json; charset=utf-8',
        'Cache-Control': kind === 'nominatim' ? 'public, max-age=60, stale-while-revalidate=300' : 'public, max-age=30, stale-while-revalidate=120',
      },
    });
  } catch {
    return NextResponse.json({ error: `${kind} service is temporarily unavailable` }, { status: 502 });
  }
}
