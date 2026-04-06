import { Grid, Heading, Section, Stack, Text } from '@orbit/terra';
import { UsageOverviewCard } from './_components/usage-overview-card';
import { ApiKeyCard } from './_components/api-key-card';
import { SetupInstructions } from './_components/setup-instructions';

export default function SetupPage() {
  return (
    <Section>
      <Stack gap="spacious">
        {/* ── Page header ── */}
        <Stack gap="snug">
          <Heading variant="title">Setup</Heading>
          <Text size="supporting" color="muted">
            Manage your API key and configure your local agent to use the Orbit Model Gateway.
          </Text>
        </Stack>

        {/* ── Top row: Usage overview + API key side by side on wide screens ── */}
        <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
          <UsageOverviewCard />
          <ApiKeyCard />
        </Grid>

        {/* ── Setup instructions ── */}
        <SetupInstructions />
      </Stack>
    </Section>
  );
}
