import { NextRequest, NextResponse } from 'next/server';
import { nominatimUrl } from '@/lib/geocoding-urls';
import { forwardGeocodingRequest } from '@/lib/server/geocoding';

export const runtime = 'nodejs';

export async function GET(request: NextRequest) {
  const query = request.nextUrl.searchParams.get('q')?.trim() ?? '';
  if (!query || query.length > 160) {
    return NextResponse.json({ error: 'Search must be between 1 and 160 characters.' }, { status: 400 });
  }
  const params = new URLSearchParams({ q: query, country: 'Nigeria', format: 'jsonv2', addressdetails: '1', limit: '1' });
  return forwardGeocodingRequest(nominatimUrl('search', params), 'nominatim', request.signal);
}
