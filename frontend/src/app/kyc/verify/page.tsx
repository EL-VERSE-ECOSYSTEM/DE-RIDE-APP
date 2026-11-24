'use client';

import { FormEvent, useState } from 'react';
import Link from 'next/link';
import { ArrowLeft, ArrowRight, CheckCircle2, FileCheck2, ShieldCheck, UploadCloud } from 'lucide-react';
import { apiRequest } from '@/lib/api-client';
import { uploadMedia } from '@/lib/media-client';

type FormFiles = { drivers: File | null; license: File | null; roadworthiness: File | null; profile: File | null; dispatchBox: File | null; dispatchPermit: File | null };

const uploadFields: { key: keyof FormFiles; title: string; hint: string; category: 'KYC_DOCUMENT' | 'PROFILE_PHOTO'; required: boolean }[] = [
  { key:'drivers',title:"Driver's licence",hint:'Clear photo or scan, maximum 5 MB',category:'KYC_DOCUMENT',required:true },
  { key:'license',title:'Vehicle licence',hint:'Clear photo or scan, maximum 5 MB',category:'KYC_DOCUMENT',required:true },
  { key:'roadworthiness',title:'Roadworthiness certificate',hint:'Clear photo or scan, maximum 5 MB',category:'KYC_DOCUMENT',required:true },
  { key:'profile',title:'Profile photo',hint:'Optional. Images are compressed and removed after 30 days.',category:'PROFILE_PHOTO',required:false },
];

export default function VerifyKYCPage() {
  const [vehicleType,setVehicleType]=useState('BIKE');
  const [plate,setPlate]=useState('');const [make,setMake]=useState('');const [model,setModel]=useState('');const [year,setYear]=useState(String(new Date().getFullYear()));const [seats,setSeats]=useState('1');
  const [files,setFiles]=useState<FormFiles>({drivers:null,license:null,roadworthiness:null,profile:null,dispatchBox:null,dispatchPermit:null});
  const [busy,setBusy]=useState(false);const [progress,setProgress]=useState('');const [message,setMessage]=useState('');const [registrationId,setRegistrationId]=useState('');
  const isDispatch=vehicleType==='DISPATCH_BIKE';

  async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();setBusy(true);setMessage('');setProgress('Preparing secure uploads…');
    try{
      if(!files.drivers||!files.license||!files.roadworthiness)throw new Error('Upload all three required verification documents.');
      const media:Record<string,string>={};
      const required=[['drivers_license_media_id',files.drivers],['vehicle_license_media_id',files.license],['road_worthiness_media_id',files.roadworthiness]] as const;
      for(const [key,file] of required){setProgress(`Compressing and uploading ${key.replaceAll('_',' ')}…`);media[key]=(await uploadMedia(file,'KYC_DOCUMENT')).id;}
      if(files.profile){setProgress('Uploading profile photo…');media.profile_photo_media_id=(await uploadMedia(files.profile,'PROFILE_PHOTO')).id;const photo=await apiRequest('/me/profile-photo',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({media_id:media.profile_photo_media_id})});if(!photo.ok)throw new Error('The profile photo was uploaded but could not be attached to your account.');}
      if(isDispatch){if(!files.dispatchBox||!files.dispatchPermit)throw new Error('Dispatch bikes require a courier box photo and state permit.');setProgress('Uploading dispatch documents…');media.dispatch_box_media_id=(await uploadMedia(files.dispatchBox,'KYC_DOCUMENT')).id;media.dispatch_permit_media_id=(await uploadMedia(files.dispatchPermit,'KYC_DOCUMENT')).id;}
      setProgress('Submitting registration for review…');
      const response=await apiRequest('/vehicle-registrations',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({vehicle_type:vehicleType,plate_number:plate,seat_capacity:Number(seats),make,model,model_year:Number(year),...media})});
      const result=await response.json();if(!response.ok)throw new Error(result.message||result.error||'Registration could not be submitted.');
      setRegistrationId(result.registration_id);setMessage('Your vehicle documents are securely stored and your registration is queued for review.');setProgress('');
    }catch(error){setMessage(error instanceof Error?error.message:'Upload failed. Please try again.');setProgress('');}finally{setBusy(false);}
  }

  function fileField(field:{key:keyof FormFiles;title:string;hint:string;category:'KYC_DOCUMENT'|'PROFILE_PHOTO';required:boolean}){return <label className="kyc-file" key={field.key}><span className="kyc-file-icon"><UploadCloud size={18}/></span><span className="kyc-file-copy"><b>{field.title}{field.required&&<i>Required</i>}</b><small>{files[field.key]?.name||field.hint}</small></span><span className="kyc-file-button">{files[field.key]?'Change file':'Choose file'}</span><input type="file" accept={field.category==='KYC_DOCUMENT'?'.jpg,.jpeg,.png,.webp,.pdf,image/jpeg,image/png,image/webp,application/pdf':'.jpg,.jpeg,.png,.webp,image/jpeg,image/png,image/webp'} required={field.required} onChange={e=>setFiles(current=>({...current,[field.key]:e.target.files?.[0]||null}))}/></label>}

  return <main className="kyc-page"><header className="kyc-header"><Link href="/" className="kyc-brand"><span>DR</span>DE <i>RIDE</i></Link><Link href="/passenger/explore" className="kyc-back"><ArrowLeft size={15}/> Account</Link></header><div className="kyc-layout"><section className="kyc-intro"><span className="kyc-kicker"><ShieldCheck size={15}/> VERIFIED VEHICLES · SAFER TRIPS</span><h1>Register your<br/><em>vehicle or bike.</em></h1><p>Upload clear documents to start vehicle verification. KYC documents stay in your secure Cloudinary account until your retention obligations end. Other media is compressed and deleted after 30 days.</p><div className="kyc-assurance"><ShieldCheck/><span><b>Private document storage</b><small>Only you and authorized DE RIDE reviewers can access uploaded documents.</small></span></div><div className="kyc-assurance"><FileCheck2/><span><b>Review status</b><small>You’ll receive an in-app notification when a reviewer updates your case.</small></span></div></section>
    <section className="kyc-form-card"><div className="kyc-form-head"><span>VEHICLE ONBOARDING</span><h2>Registration details</h2><p>Enter details as they appear on your official documents.</p></div>
      {registrationId?<div className="kyc-success"><CheckCircle2 size={34}/><h3>Submitted for review</h3><p>{message}</p><small>Reference · {registrationId}</small><Link href="/passenger/explore">Return to DE RIDE <ArrowRight size={15}/></Link></div>:<form onSubmit={submit} className="kyc-form"><div className="kyc-two"><label>Vehicle type<select value={vehicleType} onChange={e=>setVehicleType(e.target.value)}><option value="BIKE">Bike</option><option value="DISPATCH_BIKE">Dispatch bike</option><option value="BUS_14_SEATER">14-seat bus</option><option value="BUS_18_SEATER">18-seat bus</option><option value="COASTER">Coaster</option><option value="SIENNA">Sienna</option></select></label><label>Plate number<input required maxLength={20} value={plate} onChange={e=>setPlate(e.target.value.toUpperCase())} placeholder="ABC 123 XY"/></label></div><div className="kyc-three"><label>Make<input required maxLength={80} value={make} onChange={e=>setMake(e.target.value)} placeholder="e.g. Honda"/></label><label>Model<input required maxLength={80} value={model} onChange={e=>setModel(e.target.value)} placeholder="e.g. Ace"/></label><label>Year<input required type="number" min="1970" max={new Date().getFullYear()+1} value={year} onChange={e=>setYear(e.target.value)}/></label></div><label>Seat capacity<input required type="number" min="1" max="100" value={seats} onChange={e=>setSeats(e.target.value)}/></label><div className="kyc-section-label">REQUIRED DOCUMENTS</div><div className="kyc-files">{uploadFields.map(fileField)}</div>{isDispatch&&<><div className="kyc-section-label">DISPATCH RIDER VERIFICATION</div><div className="kyc-files">{fileField({key:'dispatchBox',title:'Courier box photo',hint:'Show the attached delivery box clearly',category:'KYC_DOCUMENT',required:true})}{fileField({key:'dispatchPermit',title:'State dispatch permit',hint:'Clear photo or scan, maximum 5 MB',category:'KYC_DOCUMENT',required:true})}</div></>}{progress&&<p className="kyc-progress" role="status"><span className="kyc-spinner"/>{progress}</p>}{message&&!registrationId&&<p className="kyc-error" role="alert">{message}</p>}<button className="kyc-submit" disabled={busy}>{busy?'Processing securely…':'Submit for review'} <ArrowRight size={17}/></button><p className="kyc-consent">Images are compressed before Cloudinary stores them. KYC files are retained for verification. Other uploaded media expires after 30 days.</p></form>}
    </section></div></main>;
}
