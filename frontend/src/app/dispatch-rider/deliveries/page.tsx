import { AppDashboard } from '@/components/app-dashboard';
import { RiderJobs } from '@/components/rider-jobs';
export default function DispatchRider(){return <><AppDashboard requiredRole="DISPATCH_RIDER"/><RiderJobs mode="DISPATCH_RIDER"/></>}
