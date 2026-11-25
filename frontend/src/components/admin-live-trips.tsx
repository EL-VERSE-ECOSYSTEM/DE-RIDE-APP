'use client';

import {useEffect,useState} from 'react';
import {MapPin,RefreshCw,Wifi} from 'lucide-react';
import {apiRequest,openAuthenticatedWebSocket} from '@/lib/api-client';
import {ParkMap} from '@/components/landing/ParkMap';
type Row={trip_id:string;status:string;gps_source:string;gps_updated_at:string|null;latitude:number|null;longitude:number|null;park_name:string;departure_at:string|null};
export function AdminLiveTrips(){const[rows,setRows]=useState<Row[]>([]),[error,setError]=useState('');
 async function load(){const r=await apiRequest('/admin/live-trips');const d=await r.json();if(!r.ok)throw new Error(d.message||d.error||'Live trip feed unavailable.');setRows(d.trips||[])}
 useEffect(()=>{let active=true;void load().catch(e=>setError(e.message));let socket:WebSocket|undefined;void openAuthenticatedWebSocket().then(ws=>{if(!active){ws.close();return}socket=ws;ws.onmessage=()=>{void load().catch(()=>{})}}).catch(()=>{});const timer=window.setInterval(()=>void load().catch(()=>{}),30000);return()=>{active=false;window.clearInterval(timer);socket?.close()}},[]);
 const mappedTrips=rows.filter(row=>row.latitude!==null&&row.longitude!==null);
 return <section className="wallet-page"><div className="wallet-content"><div className="wallet-ledger"><div className="wallet-ledger-head"><div><span><Wifi size={13}/> LIVE COMMAND FEED</span><h2>Active nationwide trips</h2></div><button aria-label="Refresh live trips" onClick={()=>void load()}><RefreshCw size={15}/></button></div>{mappedTrips.length>0&&<div className="admin-live-map"><ParkMap parks={mappedTrips.map(row=>({id:row.trip_id,name:`${row.park_name} · ${row.status}`,latitude:row.latitude,longitude:row.longitude}))} origin={null} destination={null}/></div>}{error&&<p className="wallet-alert" role="alert">{error}</p>}{rows.length?rows.map(row=><article className="wallet-ledger-row" key={row.trip_id}><span className="wallet-ledger-icon"><MapPin size={17}/></span><span><b>{row.park_name} · {row.status}</b><small>{row.gps_source} · {row.gps_updated_at?new Date(row.gps_updated_at).toLocaleTimeString('en-NG'):'No live GPS report'}</small></span>{row.latitude!==null&&row.longitude!==null?<a className="wallet-secondary" href={`https://www.openstreetmap.org/?mlat=${row.latitude}&mlon=${row.longitude}#map=15/${row.latitude}/${row.longitude}`} target="_blank" rel="noreferrer">View position</a>:<small>Location unavailable</small>}</article>):<div className="wallet-ledger-empty">No live trips are currently registered.</div>}</div></div></section>
}
