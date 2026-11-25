'use client';

import {useEffect,useState} from 'react';
import Image from 'next/image';
import {LoaderCircle,ShieldCheck,TriangleAlert} from 'lucide-react';

export function PremiumTicketQR({payload,code}:{payload:unknown;code:string}){
  const[dataURL,setDataURL]=useState('');const[error,setError]=useState('');
  useEffect(()=>{let alive=true;setDataURL('');setError('');if(!payload){setError('A signed QR pass is not available for this ticket.');return()=>{alive=false}}const encoded=typeof payload==='string'?payload:JSON.stringify(payload);void import('qrcode').then(qr=>qr.toDataURL(encoded,{errorCorrectionLevel:'H',margin:2,width:360,color:{dark:'#142126',light:'#ffffff'}})).then(url=>{if(alive)setDataURL(url)}).catch(()=>{if(alive)setError('This ticket QR could not be generated on this device.')});return()=>{alive=false}},[payload]);
  return <section className="premium-qr-card" aria-label="Signed DE RIDE boarding pass"><div className="premium-qr-head"><span><ShieldCheck size={16}/> AUTHENTIC DE RIDE PASS</span><small>RS256 · SHA-256</small></div>{dataURL?<div className="premium-qr-image"><Image src={dataURL} alt={`Signed boarding QR code for ticket ${code}`} width={280} height={280} unoptimized/><span className="premium-qr-logo"><Image src="/icons/deride.svg" alt="DE RIDE" width={42} height={42}/></span></div>:error?<div className="premium-qr-state is-error"><TriangleAlert size={19}/><span>{error}</span></div>:<div className="premium-qr-state"><LoaderCircle className="premium-qr-spinner" size={23}/><span>Preparing signed boarding pass…</span></div>}<div className="premium-qr-code"><small>MANUAL BACKUP CODE</small><strong>{code}</strong></div><p>Keep this pass private. Driver scans verify the signature against DE RIDE’s public key.</p></section>
}
