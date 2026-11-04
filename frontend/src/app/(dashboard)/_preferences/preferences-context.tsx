'use client';

import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';

import type { UserPreferences } from './preferences.schema';
import { serializeSparse } from './preferences.schema';

// ── Types ─────────────────────────────────────────────────────────────────────

interface PreferencesContextValue {
  preferences: UserPreferences;
  /**
   * Apply a preferences update immediately (optimistic) and schedule a
   * debounced PUT to the backend (500 ms).
   */
  updatePreferences: (updater: (prev: UserPreferences) => UserPreferences) => void;
}

// ── Context ───────────────────────────────────────────────────────────────────

const PreferencesContext = createContext<PreferencesContextValue | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface PreferencesProviderProps {
  initialPreferences: UserPreferences;
  children: ReactNode;
}

export function PreferencesProvider({ initialPreferences, children }: PreferencesProviderProps) {
  const [preferences, setPreferences] = useState<UserPreferences>(initialPreferences);
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
      }).catch(() => {
        // Best-effort write — silently dropped per ADR-0061.
      });
    }, 500);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [preferences]);

  const updatePreferences = useCallback((updater: (prev: UserPreferences) => UserPreferences) => {
    setPreferences((prev) => updater(prev));
  }, []);

  return (
    <PreferencesContext.Provider value={{ preferences, updatePreferences }}>
      {children}
    </PreferencesContext.Provider>
  );
}

// ── Hook ──────────────────────────────────────────────────────────────────────

export function usePreferences(): PreferencesContextValue {
  const ctx = useContext(PreferencesContext);
  if (!ctx) throw new Error('usePreferences must be used inside <PreferencesProvider>');
  return ctx;
}
