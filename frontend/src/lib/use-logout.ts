'use client';

import { useCallback, useState } from 'react';

export interface UseLogoutReturn {
  isLoggingOut: boolean;
  logout: () => Promise<void>;
}

export function useLogout(): UseLogoutReturn {
  const [isLoggingOut, setIsLoggingOut] = useState(false);

  const logout = useCallback(async () => {
    if (isLoggingOut) return;
    setIsLoggingOut(true);
    try {
      await fetch('/api/auth/logout', { method: 'POST' });
    } finally {
      window.location.href = '/login';
    }
  }, [isLoggingOut]);

  return { isLoggingOut, logout };
}
