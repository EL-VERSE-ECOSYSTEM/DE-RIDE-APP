import type {MetadataRoute} from 'next';
const publicRoutes=['/','/login','/register/passenger','/register/driver','/register/biker','/register/dispatcher','/register/park-operator','/terms','/privacy','/refund-policy'];
export default function sitemap():MetadataRoute.Sitemap{const base=(process.env.NEXT_PUBLIC_APP_URL||'http://localhost:3000').replace(/\/$/,'');return publicRoutes.map(path=>({url:`${base}${path}`,lastModified:new Date(),changeFrequency:path==='/'?'daily':'monthly',priority:path==='/'?1:path==='/login'?0.7:0.5}))}
