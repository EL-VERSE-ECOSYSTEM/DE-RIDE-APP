'use client';

import Image from 'next/image';
import {useLanguage} from '@/components/language-provider';
export default function Loading(){const{t}=useLanguage();return <main className="brand-loading" role="status" aria-live="polite"><span className="brand-loader-ring"><Image src="/icons/loader.svg" alt="" width={68} height={68} unoptimized/></span><b>DE RIDE</b><small>{t('Preparing your workspace…')}</small></main>}
