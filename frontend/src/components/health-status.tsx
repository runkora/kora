'use client';

/**
 * Displays the Go backend connection status.
 *
 * Fetches from the BFF /api/example/health route — never calls the Go
 * backend directly.
 */
import { useEffect, useState } from 'react';
import type { HealthResponse } from '@orbit/shared/api';
import { Badge, Card, CardContent, Code, Skeleton, Stack, Text } from '@orbit/terra';

type State = { status: 'loading' } | { status: 'ok'; data: HealthResponse } | { status: 'error' };

export function HealthStatus() {
  const [state, setState] = useState<State>({ status: 'loading' });

  useEffect(() => {
    const controller = new AbortController();
    fetch('/api/identity/health', { signal: controller.signal })
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
    <Stack maxWidth="card">
      <Card>
        {/*
         * padded: no CardHeader above this content, so we need the standard
         * top padding restored. See CardContent's `padded` prop.
         */}
        <CardContent padded>
          {/*
           * Stack gap="snug" replaces the mb-3 wrapper div — vertical rhythm
           * between the section label and the status row is handled by gap.
           */}
          <Stack gap="snug">
            <Text size="caption" color="muted" weight="emphasis" transform="uppercase">
              Backend
            </Text>

            {state.status === 'loading' && <Skeleton shape="line" width="half" size="sm" />}

            {state.status === 'ok' && (
              <Stack direction="horizontal" align="center" gap="snug">
                <Badge variant="healthy" dot>
                  {state.data.status}
                </Badge>
                <Text asChild size="supporting" color="muted">
                  <span>{state.data.service}</span>
                </Text>
              </Stack>
            )}

            {state.status === 'error' && (
              <Stack gap="snug">
                <Badge variant="error" dot>
                  Unreachable
                </Badge>
                <Text size="caption" color="muted">
                  Is <Code variant="bare">make dev</Code> running?
                </Text>
              </Stack>
            )}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
