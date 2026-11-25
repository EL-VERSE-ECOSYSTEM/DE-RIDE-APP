'use client';

import {createContext,useCallback,useContext,useEffect,useMemo,useState} from 'react';
import {Language,htmlLanguage,languages,translateShared} from '@/lib/translations';

type LanguageContextValue={language:Language;setLanguage:(language:Language)=>void;t:(key:string)=>string};
const LanguageContext=createContext<LanguageContextValue>({language:'EN',setLanguage:()=>undefined,t:key=>translateShared('EN',key)});
const STORAGE_KEY='de-ride-language';
export function LanguageProvider({children}:{children:React.ReactNode}){const[language,setCurrent]=useState<Language>('EN');
 const setLanguage=useCallback((value:Language)=>{const next=languages.includes(value)?value:'EN';setCurrent(next);document.documentElement.lang=htmlLanguage[next];localStorage.setItem(STORAGE_KEY,next);window.dispatchEvent(new CustomEvent('de-ride-language-change',{detail:next}))},[]);
 useEffect(()=>{const initial=localStorage.getItem(STORAGE_KEY) as Language|null;if(initial&&languages.includes(initial)){setCurrent(initial);document.documentElement.lang=htmlLanguage[initial]}const onChange=(event:Event)=>{const value=(event as CustomEvent<Language>).detail;if(languages.includes(value))setCurrent(value)};const onStorage=(event:StorageEvent)=>{if(event.key===STORAGE_KEY&&event.newValue&&languages.includes(event.newValue as Language))setCurrent(event.newValue as Language)};window.addEventListener('de-ride-language-change',onChange);window.addEventListener('storage',onStorage);return()=>{window.removeEventListener('de-ride-language-change',onChange);window.removeEventListener('storage',onStorage)}},[]);
 const value=useMemo(()=>({language,setLanguage,t:(key:string)=>translateShared(language,key)}),[language,setLanguage]);return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>
}
export function useLanguage(){return useContext(LanguageContext)}
