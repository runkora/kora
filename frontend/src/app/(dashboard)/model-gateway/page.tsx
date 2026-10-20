'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';

import { hasScopeCovering } from '@runkora/lib/permissions';
import { APP_ROUTES } from '@runkora/lib/routing';
import { usePermissions } from '../_components/permissions-provider';
import { MODEL_GATEWAY_NAV } from '../_utils';

// Sends the caller to the first Model Gateway page they can open. My Usage needs no scope.
export default function ModelGatewayIndex() {
  const router = useRouter();
  const { scopes, loaded } = usePermissions();

  useEffect(() => {
    if (!loaded) return;
    const first = MODEL_GATEWAY_NAV.find(
      (entry) =>
        !entry.wip && (!entry.requiredScope || hasScopeCovering(scopes, entry.requiredScope)),
    );
    router.replace(first?.href ?? APP_ROUTES.modelGateway.myUsage);
  }, [loaded, scopes, router]);

  return null;
}
