'use client';

import { useState } from 'react';
import { Callout, Stack, Tabs, TabsContent, TabsList, TabsTrigger, Text } from '@orbit/terra';
import { usePermissions } from '../../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import type { QuotaRole } from '../../_services/quota-server';
import { QuotaRolesTab } from './quota-roles-tab';
import { QuotaPrincipalsTab } from './quota-principals-tab';

interface QuotasPageClientProps {
  initialRoles: QuotaRole[];
  initialError: string | null;
}

export function QuotasPageClient({ initialRoles, initialError }: QuotasPageClientProps) {
  const { scopes } = usePermissions();
  const canWrite = hasScope(scopes, 'platform:quotas:write');
  const [roles, setRoles] = useState<QuotaRole[]>(initialRoles);

  return (
    <Stack gap="comfortable">
      {initialError && (
        <Callout variant="error">
          <Text size="supporting" color="muted">
            {initialError}
          </Text>
        </Callout>
      )}

      <Tabs defaultValue="principals">
        <TabsList>
          <TabsTrigger value="principals">Principals</TabsTrigger>
          <TabsTrigger value="roles">Roles</TabsTrigger>
        </TabsList>

        <TabsContent value="principals">
          <QuotaPrincipalsTab roles={roles} canWrite={canWrite} />
        </TabsContent>

        <TabsContent value="roles">
          <QuotaRolesTab initialRoles={roles} onRolesChange={setRoles} canWrite={canWrite} />
        </TabsContent>
      </Tabs>
    </Stack>
  );
}
