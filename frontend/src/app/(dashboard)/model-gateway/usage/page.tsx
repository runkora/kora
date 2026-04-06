import { Heading, Section, Stack, Text } from '@orbit/terra';
import { STATS } from '../_data/mock';
import { StatCards, UsageTabs } from './_components';

export default function UsagePage() {
  return (
    <Section>
      <Stack gap="spacious">
        {/* ── Page header ── */}
        <Stack gap="snug">
          <Heading variant="title">Usage</Heading>
          <Text size="supporting" color="muted">
            Monitor your Model Gateway consumption, costs, and request health.
          </Text>
        </Stack>

        {/* ── Quick stats ── */}
        <StatCards stats={STATS} />

        {/* ── Time-series tables ── */}
        <UsageTabs />
      </Stack>
    </Section>
  );
}
