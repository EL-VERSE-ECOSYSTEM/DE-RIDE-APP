import './globals.css';
import type { Metadata } from 'next';
export const metadata: Metadata = { title:'DE RIDE', description:'Move with confidence across Nigeria', manifest:'/manifest.json', themeColor:'#1B2428' };
export default function RootLayout({children}:{children:React.ReactNode}) { return <html lang="en"><body>{children}</body></html>; }
