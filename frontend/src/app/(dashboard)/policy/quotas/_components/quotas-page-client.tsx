'use client';

import { useState } from 'react';
import { Callout, Stack, Tabs, TabsContent, TabsList, TabsTrigger, Text } from '@orbit/terra';
import { usePermissions } from '../../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import type { QuotaGroup } from '../../_services/quota-server';
import { QuotaGroupsTab } from './quota-groups-tab';
import { QuotaPrincipalsTab } from './quota-principals-tab';

interface QuotasPageClientProps {
  initialGroups: QuotaGroup[];
  initialError: string | null;
}

export function QuotasPageClient({ initialGroups, initialError }: QuotasPageClientProps) {
  const { scopes } = usePermissions();
  const canWrite = hasScope(scopes, 'platform:quotas:write');
  const [groups, setGroups] = useState<QuotaGroup[]>(initialGroups);

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
          <TabsTrigger value="groups">Groups</TabsTrigger>
        </TabsList>

        <TabsContent value="principals">
          <QuotaPrincipalsTab groups={groups} canWrite={canWrite} />
        </TabsContent>

        <TabsContent value="groups">
          <QuotaGroupsTab initialGroups={groups} onGroupsChange={setGroups} canWrite={canWrite} />
        </TabsContent>
      </Tabs>
    </Stack>
  );
}
