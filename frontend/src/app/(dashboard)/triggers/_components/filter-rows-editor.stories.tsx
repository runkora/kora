'use client';

import { useState } from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Stack } from '@runkora/hades';
import { FilterRowsEditor } from './filter-rows-editor';
import type { FilterRow } from './filter-rows-editor';
import type { SchemaCatalogEntry } from './trigger-wizard/field-catalog';

/** The sentence-style filter rows, in the states of the event filters redesign. */
const meta: Meta<typeof FilterRowsEditor> = {
  title: 'Pages/Trigger filter rows',
  component: FilterRowsEditor,
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj<typeof FilterRowsEditor>;

const FIELDS: SchemaCatalogEntry[] = [
  { token: 'action', description: 'What happened to the pull request', group: 'event' },
  { token: 'pull_request.draft', description: 'Whether it is a draft', group: 'pull_request' },
  { token: 'pull_request.base.ref', description: 'Target branch', group: 'pull_request' },
  { token: 'pull_request.head.ref', description: 'Source branch', group: 'pull_request' },
  { token: 'sender.login', description: 'Who triggered the event', group: 'sender' },
];

function Editable({ initial, showIncomplete }: { initial: FilterRow[]; showIncomplete?: boolean }) {
  const [rows, setRows] = useState(initial);
  return (
    <Stack gap="snug" maxWidth="prose">
      <FilterRowsEditor
        rows={rows}
        fields={FIELDS}
        onChange={setRows}
        showIncomplete={showIncomplete ?? false}
      />
    </Stack>
  );
}

export const Default: Story = {
  render: () => (
    <Editable
      initial={[
        { field: 'action', values: 'opened | reopened | ready_for_review' },
        { field: 'pull_request.base.ref', values: 'main' },
      ]}
    />
  ),
};

/** A row that has just been added, before a field is chosen. */
export const NewFilterEmpty: Story = {
  render: () => (
    <Editable
      initial={[
        { field: 'action', values: 'opened' },
        { field: '', values: '' },
      ]}
    />
  ),
};

/** What Save shows for a row that names a field but no value. */
export const IncompleteOnSave: Story = {
  render: () => (
    <Editable
      showIncomplete
      initial={[
        { field: 'action', values: 'opened' },
        { field: 'sender.login', operator: 'contains', values: '' },
      ]}
    />
  ),
};
