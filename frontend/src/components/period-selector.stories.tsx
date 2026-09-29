import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@orbit/terra';
import { Example, Option, OptionTable } from '../packages/terra/src/docs/story-ui';
import { PreferencesProvider } from '../app/(dashboard)/_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../app/(dashboard)/_preferences/preferences.schema';
import type { AnalyticsDays } from '../app/(dashboard)/_preferences/period';
import { PeriodSelector } from './period-selector';

/**
 * PeriodSelector
 *
 * Buttons for choosing a 7, 30 or 90 day window on analytics pages. The choice is also saved as
 * the user's default period.
 */
const meta: Meta<typeof PeriodSelector> = {
  title: 'App/Molecules/PeriodSelector',
  component: PeriodSelector,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof PeriodSelector>;

/** The selector reads preferences, so each sample gets a provider seeded with the defaults. */
function Demo({ loading }: { loading?: boolean }) {
  const [days, setDays] = useState<AnalyticsDays>(30);
  return (
    <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
      <PeriodSelector
        prefKey="analytics.defaultPeriod"
        value={days}
        onChange={setDays}
        loading={loading}
      />
    </PreferencesProvider>
  );
}

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Analytics window">
        <Demo />
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option name="default" use="The active period is filled. Pick another to switch.">
          <Demo />
        </Option>
        <Option name="loading" use="Disables every button while new data loads.">
          <Demo loading />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
