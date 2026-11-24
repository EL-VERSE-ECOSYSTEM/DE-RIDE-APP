import 'maplibre-gl/dist/maplibre-gl.css';
import './globals.css';
import type { Metadata } from 'next';
import { ServiceWorker } from './service-worker';
import { ThemeMode } from '@/components/theme-mode';
export const metadata: Metadata = {
  title: 'DE RIDE — Move with confidence',
  description: 'Bus travel, bike rides and parcel dispatch across Nigeria. Find live parks and book your next ride.',
  manifest: '/manifest.json',
  themeColor: '#1B2428',
  icons: { icon: '/icons/deride.svg', shortcut: '/icons/deride.svg', apple: '/icons/deride.svg' },
  openGraph: { title: 'DE RIDE', description: 'Move with confidence across Nigeria.', images: [{ url: '/icons/1794927085562.jpg', width: 1408, height: 768, alt: 'DE RIDE app and logo' }] },
  twitter: { card: 'summary_large_image', title: 'DE RIDE', description: 'Move with confidence across Nigeria.', images: ['/icons/1794927085562.jpg'] },
};
export default function RootLayout({children}:{children:React.ReactNode}) { return <html lang="en"><body><ServiceWorker /><ThemeMode />{children}</body></html>; }
