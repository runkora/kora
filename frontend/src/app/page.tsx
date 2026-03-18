/**
 * Orbit — root landing page.
 *
 * Temporary placeholder until the real dashboard is built.
 * The HealthStatus component fetches via the BFF /api/example/health route
 * so the browser never communicates with the Go backend directly.
 */
import { HealthStatus } from '@/components/HealthStatus';
import { Badge, Heading, Text } from '@orbit/terra';

export default function RootPage() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center">
      <div className="flex flex-col items-center gap-6 text-center">

        {/* Wordmark */}
        <div className="flex flex-col items-center gap-3">
          {/*
           * transform="uppercase": Badge doesn't carry a transform prop — the
           * uppercase treatment here is applied via Text transform semantics on
           * the content. We use a plain uppercase string + size="sm" instead
           * so no className override is needed on Badge itself.
           */}
          <Badge variant="outline" size="sm">EARLY ACCESS</Badge>

          {/*
           * variant="display": text-5xl hero wordmark. This is the correct
           * Terra variant for splash/landing-page titles — no className="text-5xl"
           * override needed.
           */}
          <Heading variant="display">Orbit</Heading>

          {/*
           * max-w-md wrapper: constrains the subtitle width to a readable
           * measure on wide viewports. Layout sizing belongs on a wrapper
           * element, not on the Text component itself.
           */}
          <div className="max-w-md">
            <Text size="lead" color="muted">
              Control plane for autonomous AI agents.
            </Text>
          </div>
        </div>

        {/* Backend connection status */}
        <HealthStatus />
      </div>
    </main>
  );
}
