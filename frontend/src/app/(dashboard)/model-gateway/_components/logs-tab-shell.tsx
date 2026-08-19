'use client';

// ── LogsTabShell ──────────────────────────────────────────────────────────────
//
// Client boundary for the logs page. Renders one of two surfaces:
//
//   - "My logs"  — the caller's own inference requests (self-scoped). Seeded by
//                  the RSC page so it paints instantly with no loading flash.
//   - "All logs" — every principal's requests across the tenant (admin). Only
//                  offered to principals holding `platform:analytics:read`, and
//                  fetched lazily when the tab first mounts (no RSC seed, since
//                  scopes are only known client-side).
//
// Each tab owns its own LogsProvider so their state — filters, pagination,
// identity map — stays independent. Enforcement is at the API layer: a
// non-admin who reaches GET /v1/usage/all is rejected by the gateway. This
// scope check only decides whether to *show* the admin tab.

import { useState } from 'react';
import { Heading, Stack, Tabs, TabsList, TabsTrigger, TabsContent, Text } from '@orbit/terra';
import { hasScope } from '@orbit/lib/permissions';
import { LogsProvider } from '../_context/logs-context';
import { LogsTab } from './logs-tab';
import { useSession } from '../../_components/permissions-provider';
import type { InferenceUsageRecord } from '../_types';
import type { PAT } from '@orbit/shared/api';

interface LogsTabShellProps {
  initialRecords: InferenceUsageRecord[];
  initialHasNextPage: boolean;
  initialNextCursor: string | null;
  initialPats: PAT[];
  initialError: string | null;
  selectedKeyId?: string;
}

// Per-surface page descriptions. The admin copy references the tab switch, so it
// only appears once the tabbed admin view is available.
const ME_DESCRIPTION = 'Inference requests made with your own identity.';
const ALL_DESCRIPTION =
  'Inference requests across every principal in your tenant. Switch to "My logs" to see only requests made with your own identity.';

export function LogsTabShell({
  initialRecords,
  initialHasNextPage,
  initialNextCursor,
  initialPats,
  initialError,
  selectedKeyId,
}: LogsTabShellProps) {
  const { scopes, loaded } = useSession();
  // Conservative: reveal the admin tab only once the session confirms the
  // scope, so we never fire the tenant-wide fetch (and flash a 403) for a
  // principal who turns out not to hold it.
  const canViewAll = loaded && hasScope(scopes, 'platform:analytics:read');

  // Controlled so the page description below the title tracks the active tab.
  const [activeTab, setActiveTab] = useState('me');

  const myLogs = (
    <LogsProvider
      variant="me"
      initialRecords={initialRecords}
      initialHasNextPage={initialHasNextPage}
      initialNextCursor={initialNextCursor}
      initialPats={initialPats}
      initialError={initialError}
    >
      <LogsTab selectedKeyId={selectedKeyId} />
    </LogsProvider>
  );

  const header = (description: string) => (
    <Stack gap="snug">
      <Heading variant="title">Logs</Heading>
      <Text size="supporting" color="muted">
        {description}
      </Text>
    </Stack>
  );

  // Non-admins (and the pre-session-load window) see just their own logs, no
  // tab chrome — identical to the surface before this feature existed.
  if (!canViewAll) {
    return (
      <Stack gap="comfortable">
        {header(ME_DESCRIPTION)}
        {myLogs}
      </Stack>
    );
  }

  return (
    <Stack gap="comfortable">
      {header(activeTab === 'all' ? ALL_DESCRIPTION : ME_DESCRIPTION)}

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="me">My logs</TabsTrigger>
          <TabsTrigger value="all">Admin logs</TabsTrigger>
        </TabsList>

        <TabsContent value="me">{myLogs}</TabsContent>

        <TabsContent value="all">
          {/* No RSC seed: fetchOnMount drives the first tenant-wide load when
              this tab is first selected (Radix mounts content lazily). */}
          <LogsProvider
            variant="all"
            initialRecords={[]}
            initialHasNextPage={false}
            initialNextCursor={null}
            initialPats={[]}
            initialError={null}
            fetchOnMount
          >
            <LogsTab />
          </LogsProvider>
        </TabsContent>
      </Tabs>
    </Stack>
  );
}
