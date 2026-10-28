'use client';

import { useCallback, useState } from 'react';
import { useRouter } from 'next/navigation';

export interface UseLogoutReturn {
  isLoggingOut: boolean;
  logout: () => Promise<void>;
}

export function useLogout(): UseLogoutReturn {
  const [isLoggingOut, setIsLoggingOut] = useState(false);
  const router = useRouter();

  const logout = useCallback(async () => {
    if (isLoggingOut) return;
    setIsLoggingOut(true);

    try {
      await fetch('/api/auth/logout', { method: 'POST' });
    } finally {
      router.replace('/login');
    }
  }, [isLoggingOut, router]);

  return { isLoggingOut, logout };
}
