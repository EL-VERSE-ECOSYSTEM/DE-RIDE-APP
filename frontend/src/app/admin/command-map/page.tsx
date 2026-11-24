import { AppDashboard } from '@/components/app-dashboard';
import {AdminLiveTrips} from '@/components/admin-live-trips';
export default function Admin(){return <><AppDashboard requiredRole="ADMIN"/><AdminLiveTrips/></>}
