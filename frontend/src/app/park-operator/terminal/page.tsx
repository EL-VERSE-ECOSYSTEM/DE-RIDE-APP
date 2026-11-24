import { AppDashboard } from '@/components/app-dashboard';
import {TripLifecycle} from '@/components/trip-lifecycle';
export default function Park(){return <><AppDashboard requiredRole="PARK_OPERATOR"/><div className="wallet-page"><div className="wallet-content"><TripLifecycle gpsSource="DISPATCHER_APP"/></div></div></>}
