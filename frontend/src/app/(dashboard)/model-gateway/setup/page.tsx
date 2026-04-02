import { Heading, Text } from '@orbit/terra';
import { UsageOverviewCard } from './_components/usage-overview-card';
import { ApiKeyCard } from './_components/api-key-card';
import { SetupInstructions } from './_components/setup-instructions';

export default function SetupPage() {
  return (
    <div className="px-6 py-8 max-w-screen-xl mx-auto">
      <div className="flex flex-col gap-8">
        {/* ── Page header ── */}
        <div>
          <Heading variant="title">Setup</Heading>
          <div className="mt-1">
            <Text size="supporting" color="muted">
              Manage your API key and configure your local agent to use the Orbit Model Gateway.
            </Text>
          </div>
        </div>

        {/* ── Top row: Usage overview + API key side by side on wide screens ── */}
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <UsageOverviewCard />
          <ApiKeyCard />
        </div>

        {/* ── Setup instructions ── */}
        <SetupInstructions />
      </div>
    </div>
  );
}
