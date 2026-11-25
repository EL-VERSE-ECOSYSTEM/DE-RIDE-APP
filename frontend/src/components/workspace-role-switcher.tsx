'use client';

import {useState} from 'react';
import {ArrowLeftRight,LoaderCircle,TriangleAlert} from 'lucide-react';
import {apiRequest} from '@/lib/api-client';
import {useLanguage} from '@/components/language-provider';

const roleLabels:Record<string,string>={PASSENGER:'Passenger',DRIVER:'Bus driver',BIKER:'Biker',DISPATCH_RIDER:'Dispatch rider',DISPATCHER:'Dispatcher',PARK_OPERATOR:'Park operator',ADMIN:'DE RIDE Admin'};
const roleRoutes:Record<string,string>={PASSENGER:'/passenger/explore',DRIVER:'/driver/dashboard',BIKER:'/biker/radar',DISPATCH_RIDER:'/dispatch-rider/deliveries',DISPATCHER:'/park-operator/terminal',PARK_OPERATOR:'/park-operator/terminal',ADMIN:'/admin/command-map'};
export function WorkspaceRoleSwitcher({roles,activeRole}:{roles:string[];activeRole:string}){const{t}=useLanguage();const[busy,setBusy]=useState(false);const[error,setError]=useState('');const available=roles.filter(role=>roleRoutes[role]);if(available.length<2)return null;
 async function change(role:string){if(role===activeRole)return;setBusy(true);setError('');try{const r=await apiRequest('/users/active-role',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({role})});const d=await r.json();if(!r.ok)throw new Error(d.message||d.error||'Workspace switch was not accepted.');sessionStorage.setItem('deride_access_token',d.access_token);if(d.refresh_token)sessionStorage.setItem('deride_refresh_token',d.refresh_token);location.assign(roleRoutes[role])}catch(e){setError(e instanceof Error?e.message:'Workspace switch failed.')}finally{setBusy(false)}}
 return <div className="workspace-role-switcher"><label><ArrowLeftRight size={15}/><span>{t('Switch workspace')}</span><select aria-label={t('Switch workspace')} value={activeRole} disabled={busy} onChange={e=>void change(e.target.value)}>{available.map(role=><option key={role} value={role}>{t(roleLabels[role])}</option>)}</select>{busy&&<LoaderCircle className="role-switch-spin" size={15}/>}</label>{error&&<small role="alert"><TriangleAlert size={13}/>{error}</small>}</div>
}
