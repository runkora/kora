/**
 * Orbit — root landing page.
 *
 * Temporary placeholder until the real dashboard is built.
 * The HealthStatus component fetches via the BFF /api/example/health route
 * so the browser never communicates with the Go backend directly.
 */
import { Page } from '@core/components/page';
import { HealthStatus } from '@core/components/health-status';
import { Badge, Heading, Stack, Text } from '@orbit/terra';

export default function RootPage() {
  return (
    <Page variant="centered">
      <Stack align="center" gap="relaxed" textAlign="center">
        {/* Wordmark */}
        <Stack align="center" gap="snug">
          {/*
           * variant="display": text-5xl hero wordmark. This is the correct
           * Terra variant for splash/landing-page titles — no className="text-5xl"
           * override needed.
           */}
          <Badge variant="outline" size="sm">
            EARLY ACCESS
          </Badge>

          <Heading variant="display">Orbit</Heading>

          {/*
           * maxWidth="prose": constrains the subtitle to a comfortable reading
           * measure (max-w-prose / 65ch) on wide viewports — the correct
           * semantic token for body-width text constraints.
           */}
          <Stack maxWidth="prose">
            <Text size="lead" color="muted">
              Control plane for autonomous AI agents.
            </Text>
          </Stack>
        </Stack>

        {/* Backend connection status */}
        <HealthStatus />
      </Stack>
    </Page>
  );
}
