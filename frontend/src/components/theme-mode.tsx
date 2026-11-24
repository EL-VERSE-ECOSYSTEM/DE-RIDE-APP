'use client';

import { useEffect, useState } from 'react';
import { Sun, Moon } from 'lucide-react';

const STORAGE_KEY = 'de-ride-auto-theme';
function themeForHour(hour: number) { return hour >= 7 && hour < 19 ? 'light' : 'dark'; }

export function ThemeMode() {
  const [automatic, setAutomatic] = useState(true);
  const [theme, setTheme] = useState<'light' | 'dark'>('dark');

  useEffect(() => {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    const enabled = saved !== 'light' && saved !== 'dark' && saved !== 'off';
    const selected: 'light' | 'dark' = saved === 'light' || saved === 'dark' ? saved : themeForHour(new Date().getHours());
    setAutomatic(enabled);
    setTheme(selected);
    document.documentElement.dataset.theme = selected;
    const timer = window.setInterval(() => {
      if (!['light', 'dark', 'off'].includes(window.localStorage.getItem(STORAGE_KEY) ?? '')) {
        const next = themeForHour(new Date().getHours());
        setTheme(next);
        document.documentElement.dataset.theme = next;
      }
    }, 60_000);
    return () => window.clearInterval(timer);
  }, []);

  function toggle() {
    if (automatic) {
      setAutomatic(false);
      window.localStorage.setItem(STORAGE_KEY, theme);
    } else {
      const next = themeForHour(new Date().getHours());
      setAutomatic(true);
      setTheme(next);
      document.documentElement.dataset.theme = next;
      window.localStorage.setItem(STORAGE_KEY, 'on');
    }
  }

  return <button className="theme-toggle" type="button" onClick={toggle} aria-pressed={automatic} aria-label={`Automatic day and night theme ${automatic ? 'on' : 'off'}`} title={`Automatic theme ${automatic ? 'on' : 'off'}`}>
    {theme === 'light' ? <Sun size={16}/> : <Moon size={16}/>}<span>Auto {automatic ? 'On' : 'Off'}</span>
  </button>;
}
