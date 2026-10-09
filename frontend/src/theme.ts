import { useEffect, useState } from 'react';
import { useStore } from './store';

const media = typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: dark)') : null;

/** Resolves the effective theme ("light" | "dark") from settings. */
export function useTheme(): 'light' | 'dark' {
  const pref = useStore((s) => s.settings.theme);
  const [system, setSystem] = useState(media?.matches ? 'dark' : 'light');
  useEffect(() => {
    if (!media) return;
    const fn = () => setSystem(media.matches ? 'dark' : 'light');
    media.addEventListener('change', fn);
    return () => media.removeEventListener('change', fn);
  }, []);
  return pref === 'system' ? (system as 'light' | 'dark') : pref;
}
