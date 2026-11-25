'use client';

import { useEffect, useRef, useState } from 'react';
import { MapPinned } from 'lucide-react';
import Image from 'next/image';
import { GeocodedPlace, reverseNominatim } from './geocoding';

type Park = { id: string; name: string; state_code?: string; latitude?: number | null; longitude?: number | null };
type MapLibreMap = import('maplibre-gl').Map;
type MapLibreMarker = import('maplibre-gl').Marker;

export function ParkMap({ parks, origin, destination, onReverse }: { parks: Park[]; origin: GeocodedPlace | null; destination: GeocodedPlace | null; onReverse?: (place: GeocodedPlace | null, error?: string) => void }) {
  const container = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const onReverseRef = useRef(onReverse);
  onReverseRef.current = onReverse;
  const markersRef = useRef<MapLibreMarker[]>([]);
  const [mapReady, setMapReady] = useState(false);
  const [mapError, setMapError] = useState('');
  const [reverseLoading, setReverseLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let map: MapLibreMap | null = null;
    void import('maplibre-gl').then(maplibregl => {
      if (cancelled || !container.current) return;
      maplibregl.setWorkerUrl(new URL('maplibre-gl/dist/maplibre-gl-worker.mjs', import.meta.url).toString());
      const createdMap = new maplibregl.Map({ container: container.current, style: 'https://tiles.openfreemap.org/styles/dark', center: [3.3792, 6.5244], zoom: 11, attributionControl: false });
      map = createdMap;
      mapRef.current = createdMap;
      let styleLoaded = false;
      createdMap.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right');
      createdMap.on('load', () => { styleLoaded = true; if (!cancelled) { setMapReady(true); setMapError(''); } });
      createdMap.on('error', () => { if (!cancelled && !styleLoaded) setMapError('Map style could not load. Check your connection and try again.'); });
      createdMap.on('click', async event => {
        if (cancelled || !onReverseRef.current) return;
        setReverseLoading(true);
        try { onReverseRef.current(await reverseNominatim(event.lngLat.lat, event.lngLat.lng)); }
        catch { onReverseRef.current(null, 'Could not identify that map location.'); }
        finally { if (!cancelled) setReverseLoading(false); }
      });
    }).catch(() => setMapError('Map renderer could not be loaded.'));
    return () => { cancelled = true; markersRef.current.forEach(marker => marker.remove()); markersRef.current = []; map?.remove(); mapRef.current = null; };
  }, []);

  useEffect(() => {
    const map = mapRef.current;
    if (!map || !mapReady) return;
    let cancelled = false;
    markersRef.current.forEach(marker => marker.remove());
    markersRef.current = [];
    import('maplibre-gl').then(maplibregl => {
      if (cancelled) return;
      const addMarker = (longitude: number, latitude: number, label: string, kind: 'park'|'selected') => {
        const element = document.createElement('div');
        element.className = `maplibre-marker maplibre-marker-${kind}`;
        element.title = label;
        element.setAttribute('aria-label', label);
        const marker = new maplibregl.Marker({ element, anchor: 'bottom' }).setLngLat([longitude, latitude]).setPopup(new maplibregl.Popup({ offset: 24 }).setText(label)).addTo(map);
        markersRef.current.push(marker);
      };
      parks.forEach(park => { if (Number.isFinite(park.longitude) && Number.isFinite(park.latitude)) addMarker(Number(park.longitude), Number(park.latitude), `${park.name}${park.state_code ? ` · ${park.state_code}` : ''}`, 'park'); });
      const parkCoordinates: [number, number][] = parks
        .filter(park => Number.isFinite(park.longitude) && Number.isFinite(park.latitude))
        .map(park => [Number(park.longitude), Number(park.latitude)]);
      if (origin) addMarker(origin.longitude, origin.latitude, `Origin · ${origin.label}`, 'selected');
      if (destination) addMarker(destination.longitude, destination.latitude, `Destination · ${destination.label}`, 'selected');
      const points = [origin, destination].filter((place): place is GeocodedPlace => place !== null);
      if (points.length === 2) {
        const coordinates: [number,number][] = [[points[0].longitude,points[0].latitude],[points[1].longitude,points[1].latitude]];
        const source = map.getSource('route-preview') as import('maplibre-gl').GeoJSONSource | undefined;
        if (source) source.setData({ type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates } });
        else map.addSource('route-preview', { type: 'geojson', data: { type: 'Feature', properties: {}, geometry: { type: 'LineString', coordinates } } });
        if (!map.getLayer('route-preview-line')) map.addLayer({ id: 'route-preview-line', type: 'line', source: 'route-preview', paint: { 'line-color': '#F46A6A', 'line-width': 3, 'line-dasharray': [2,2] } });
        const bounds = new maplibregl.LngLatBounds(coordinates[0], coordinates[0]);
        bounds.extend(coordinates[1]);
        map.fitBounds(bounds, { padding: 44, maxZoom: 10, duration: 650 });
      } else {
        const source = map.getSource('route-preview') as import('maplibre-gl').GeoJSONSource | undefined;
        source?.setData({ type: 'FeatureCollection', features: [] });
        if (points.length === 1) map.flyTo({ center: [points[0].longitude,points[0].latitude], zoom: 11, duration: 650 });
        else if (parkCoordinates.length === 1) map.flyTo({ center: parkCoordinates[0], zoom: 11, duration: 650 });
        else if (parkCoordinates.length > 1) {
          const bounds = new maplibregl.LngLatBounds(parkCoordinates[0], parkCoordinates[0]);
          parkCoordinates.slice(1).forEach(coordinates => bounds.extend(coordinates));
          map.fitBounds(bounds, { padding: 48, maxZoom: 10, duration: 650 });
        }
      }
    }).catch(() => setMapError('Map markers could not be displayed.'));
    return () => { cancelled = true; };
  }, [parks, origin, destination, mapReady]);

  return <div className="maplibre-wrap"><div ref={container} className="maplibre-canvas" role="application" aria-label="Interactive map showing DE RIDE parks and selected locations"/><div className="maplibre-attribution"><MapPinned size={12}/><a href="https://openfreemap.org/" target="_blank" rel="noreferrer">OpenFreeMap</a><span>·</span><a href="https://openmaptiles.org/" target="_blank" rel="noreferrer">OpenMapTiles</a><span>·</span><a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">© OpenStreetMap</a></div>{!mapReady && !mapError && <div className="maplibre-loading"><Image src="/icons/deride.svg" alt="" width={24} height={24}/><span>Loading live park map…</span></div>}{mapError && <div className="maplibre-message">{mapError}</div>}{reverseLoading && <div className="map-reverse-status">Resolving selected location…</div>}</div>;
}
