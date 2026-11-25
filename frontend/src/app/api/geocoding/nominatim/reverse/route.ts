import { NextRequest, NextResponse } from 'next/server';
import { nominatimUrl } from '@/lib/geocoding-urls';
import { forwardGeocodingRequest } from '@/lib/server/geocoding';

export const runtime = 'nodejs';

export async function GET(request: NextRequest) {
  const latitude = Number(request.nextUrl.searchParams.get('lat'));
  const longitude = Number(request.nextUrl.searchParams.get('lon'));
  if (!Number.isFinite(latitude) || latitude < -90 || latitude > 90 || !Number.isFinite(longitude) || longitude < -180 || longitude > 180) {
    return NextResponse.json({ error: 'Valid latitude and longitude are required.' }, { status: 400 });
  }
  const params = new URLSearchParams({ lat: String(latitude), lon: String(longitude), format: 'jsonv2', addressdetails: '1', zoom: '16' });
  return forwardGeocodingRequest(nominatimUrl('reverse', params), 'nominatim', request.signal);
}
