'use client';

import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';

import type { UserPreferences } from './preferences.schema';
import { serializeSparse } from './preferences.schema';

interface PreferencesContextValue {
  preferences: UserPreferences;
  /**
   * Apply a preferences update immediately (optimistic) and schedule a
   * debounced PUT to the backend (500 ms).
   */
  updatePreferences: (updater: (prev: UserPreferences) => UserPreferences) => void;
  /**
   * False when the first read of the preferences failed. Then `preferences`
   * holds only the defaults, and a PUT replaces the stored document with them.
   * A feature that saves without an explicit user action must not save then.
   */
  loaded: boolean;
}

const PreferencesContext = createContext<PreferencesContextValue | null>(null);

interface PreferencesProviderProps {
  initialPreferences: UserPreferences;
  /** Whether the read of `initialPreferences` succeeded. */
  loaded?: boolean;
  children: ReactNode;
}

export function PreferencesProvider({
  initialPreferences,
  loaded: loadedProp,
  children,
}: PreferencesProviderProps) {
  // Both values describe the initial snapshot. A server refresh preserves this
  // state, so new props must not mark fallback preferences as safely loaded.
  const [preferences, setPreferences] = useState<UserPreferences>(initialPreferences);
  const [loaded] = useState(loadedProp !== false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isMounted = useRef(false);

  useEffect(() => {
    // Skip the initial mount — preferences were just seeded from SSR.
    if (!isMounted.current) {
      isMounted.current = true;
      return;
    }
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      const body = JSON.stringify(serializeSparse(preferences));
      void fetch('/api/preferences', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body,
      }).catch(() => {});
    }, 500);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [preferences]);

  const updatePreferences = useCallback((updater: (prev: UserPreferences) => UserPreferences) => {
    setPreferences((prev) => updater(prev));
  }, []);

  return (
    <PreferencesContext.Provider value={{ preferences, updatePreferences, loaded }}>
      {children}
    </PreferencesContext.Provider>
  );
}

export function usePreferences(): PreferencesContextValue {
  const ctx = useContext(PreferencesContext);
  if (!ctx) throw new Error('usePreferences must be used inside <PreferencesProvider>');
  return ctx;
}
