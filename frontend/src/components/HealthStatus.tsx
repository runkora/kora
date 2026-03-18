'use client';

/**
 * Displays the Go backend connection status.
 *
 * Fetches from the BFF /api/example/health route — never calls the Go
 * backend directly.
 */
import { useEffect, useState } from 'react';
import type { HealthResponse } from '@/shared/api';
import { Badge, Card, CardContent, Code, Text } from '@orbit/terra';

type State = { status: 'loading' } | { status: 'ok'; data: HealthResponse } | { status: 'error' };

export function HealthStatus() {
  const [state, setState] = useState<State>({ status: 'loading' });

  useEffect(() => {
    const controller = new AbortController();
    fetch('/api/example/health', { signal: controller.signal })
      .then(async (res) => {
        const body = await res.json();
        if (!res.ok || body.error) {
          setState({ status: 'error' });
        } else {
          setState({ status: 'ok', data: body.data as HealthResponse });
        }
      })
      .catch((err: unknown) => {
        if (err instanceof Error && err.name === 'AbortError') return;
        console.error('[HealthStatus] fetch failed:', err);
        setState({ status: 'error' });
      });
    return () => controller.abort();
  }, []);

  return (
    // w-72: layout constraint on the card's container, not the card itself.
    <div className="w-72">
      <Card>
        {/*
         * padded: no CardHeader above this content, so we need the standard
         * top padding restored. See CardContent's `padded` prop.
         */}
        <CardContent padded>
          {/*
           * mb-3: spacing between the section label and the status content.
           * This is layout — it lives on the wrapper, not the Text component.
           */}
          <div className="mb-3">
            <Text size="caption" color="muted" weight="emphasis" transform="uppercase">
              Backend
            </Text>
          </div>

          {state.status === 'ok' && (
            <div className="flex items-center gap-2">
              <Badge variant="healthy" dot>
                {state.data.status}
              </Badge>
              <Text asChild size="supporting" color="muted">
                <span>{state.data.service}</span>
              </Text>
            </div>
          )}

          {state.status === 'error' && (
            <div className="flex flex-col gap-2">
              <Badge variant="error" dot>
                Unreachable
              </Badge>
              {/*
               * gap-2 on the parent div handles vertical rhythm — no mt-* needed
               * directly on the Text element.
               */}
              <Text size="caption" color="muted">
                Is <Code variant="bare">make dev</Code> running?
              </Text>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
