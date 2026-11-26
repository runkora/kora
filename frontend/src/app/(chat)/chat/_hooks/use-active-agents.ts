'use client';

import { useEffect, useState } from 'react';
import type { AgentTypeRow } from '@runkora/app/(dashboard)/agents/_types';
import { fetchAgentTypes } from '@runkora/app/(dashboard)/agents/_services/agents-api';

/** The active agents the viewer can see, read once on mount. Null until they load. */
export function useActiveAgents(): { list: AgentTypeRow[]; error: string | null } | null {
  const [agents, setAgents] = useState<{ list: AgentTypeRow[]; error: string | null } | null>(null);
  useEffect(() => {
    let live = true;
    void fetchAgentTypes().then(({ data, error }) => {
      if (live) setAgents({ list: data.filter((a) => a.state === 'active'), error });
    });
    return () => {
      live = false;
    };
  }, []);
  return agents;
}
