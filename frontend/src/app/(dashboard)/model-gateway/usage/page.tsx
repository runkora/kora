import { Heading, Stack, Text } from '@orbit/terra';
import { STATS } from '../_data/mock';
import { StatCards, UsageTabs } from './_components';

export default function UsagePage() {
  return (
    <div className="px-6 py-8 max-w-screen-xl mx-auto">
      <Stack gap="spacious">
        {/* ── Page header ── */}
        <div>
          <Heading variant="title">Usage</Heading>
          <div className="mt-1">
            <Text size="supporting" color="muted">
              Monitor your Model Gateway consumption, costs, and request health.
            </Text>
          </div>
        </div>

        {/* ── Quick stats ── */}
        <StatCards stats={STATS} />

        {/* ── Time-series tables ── */}
        <UsageTabs />
      </Stack>
    </div>
  );
}
