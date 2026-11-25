import { NextRequest, NextResponse } from 'next/server';
import { photonSearchUrl } from '@/lib/geocoding-urls';
import { forwardGeocodingRequest } from '@/lib/server/geocoding';

export const runtime = 'nodejs';

export async function GET(request: NextRequest) {
  const query = request.nextUrl.searchParams.get('q')?.trim() ?? '';
  if (query.length < 2 || query.length > 160) {
    return NextResponse.json({ error: 'Search must be between 2 and 160 characters.' }, { status: 400 });
  }
  const language = request.nextUrl.searchParams.get('lang') ?? 'en';
  return forwardGeocodingRequest(photonSearchUrl(query, language), 'photon', request.signal);
}
