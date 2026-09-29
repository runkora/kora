import type { Meta, StoryObj } from '@storybook/react';
import { Stack, Text } from '@orbit/terra';
import { Example, Option, OptionTable } from '../packages/terra/src/docs/story-ui';
import { PreferencesProvider } from '../app/(dashboard)/_preferences/preferences-context';
import { PREFERENCE_DEFAULTS } from '../app/(dashboard)/_preferences/preferences.schema';
import { SmartTable, type DataTableColumn } from './smart-table';
import { StatusBadge, type OrbitStatus } from './status-badge';

/**
 * SmartTable
 *
 * A data table that remembers which columns each user hides. Use it for tables inside the
 * dashboard, and use the plain DataTable everywhere else.
 */
const meta: Meta<typeof SmartTable> = {
  title: 'App/Molecules/SmartTable',
  component: SmartTable,
  tags: ['autodocs'],
  parameters: { layout: 'padded' },
};
export default meta;
type Story = StoryObj<typeof SmartTable>;

interface Agent {
  name: string;
  status: OrbitStatus;
  provider: string;
  runs: number;
}

const AGENTS: Agent[] = [
  { name: 'apollo-3', status: 'healthy', provider: 'Anthropic', runs: 412 },
  { name: 'hermes-1', status: 'idle', provider: 'Bedrock', runs: 87 },
  { name: 'daedalus-4', status: 'error', provider: 'Anthropic', runs: 63 },
  { name: 'minerva-2', status: 'pending', provider: 'Vertex AI', runs: 254 },
];

function columns(hideProvider = false): DataTableColumn<Agent>[] {
  return [
    {
      id: 'name',
      header: 'Agent',
      sortable: true,
      sortAccessor: (a) => a.name,
      cell: (a) => (
        <Text size="supporting" weight="emphasis">
          {a.name}
        </Text>
      ),
    },
    {
      id: 'status',
      header: 'Status',
      gridWidth: '140px',
      cell: (a) => <StatusBadge status={a.status} />,
    },
    {
      id: 'provider',
      header: 'Provider',
      defaultVisible: !hideProvider,
      cell: (a) => <Text size="supporting">{a.provider}</Text>,
    },
    {
      id: 'runs',
      header: 'Runs',
      gridWidth: '90px',
      sortable: true,
      sortAccessor: (a) => a.runs,
      cell: (a) => (
        <Text size="supporting" mono>
          {a.runs}
        </Text>
      ),
    },
  ];
}

/** SmartTable reads preferences, so each sample gets its own provider seeded with the defaults. */
function Demo({ tableId, hideProvider }: { tableId: string; hideProvider?: boolean }) {
  return (
    <PreferencesProvider initialPreferences={PREFERENCE_DEFAULTS}>
      <SmartTable
        tableId={tableId}
        data={AGENTS}
        columns={columns(hideProvider)}
        getRowKey={(a) => a.name}
        defaultSortId="name"
      />
    </PreferencesProvider>
  );
}

export const Overview: Story = {
  render: () => (
    <Stack maxWidth="prose">
      <Example title="Agents">
        <Demo tableId="story-overview" />
      </Example>
    </Stack>
  ),
};

export const Options: Story = {
  render: () => (
    <Stack maxWidth="wide">
      <OptionTable title="Options">
        <Option
          name="tableId"
          use="Names the table so its hidden columns are saved for it alone. Toggle a column in the column menu."
        >
          <Demo tableId="story-options-default" />
        </Option>
        <Option
          name="defaultVisible: false"
          use="A column that starts hidden. Only a change from this default is saved."
        >
          <Demo tableId="story-options-hidden" hideProvider />
        </Option>
      </OptionTable>
    </Stack>
  ),
};
