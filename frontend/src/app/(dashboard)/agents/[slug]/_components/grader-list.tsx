'use client';

/**
 * @tier 3 · agents/[slug]-scoped. The Grades tab's list: each Grader's pass rate over its last
 * `WINDOW` graded runs against its `minPassRate`, in the API's order, failing first.
 */

import { useState } from 'react';
import {
  DataTable,
  EmptyState,
  Icon,
  SegmentedControl,
  Stack,
  Text,
  type DataTableColumn,
} from '@runkora/hades';
import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@runkora/lib/pagination';
import type { GraderStatus, GraderSummary } from '@runkora/shared/api';
import { GRADER_STATUS_LABEL, WINDOW, graderName, passRate } from '../../_lib/grades';

/** "52% passed · needs 90%"; the minimum only when the Grader sets one. */
function RateText({ g }: { g: GraderSummary }) {
  const rate = passRate(g.passed, g.graded);
  if (rate === null) return null;
  return (
    <Text size="supporting" tabular color={g.status === 'failing' ? 'status-error' : 'foreground'}>
      {rate}% passed
      {g.min_pass_rate !== null && (
        <Text as="span" size="supporting" color="subtle">
          {' '}
          · needs {g.min_pass_rate}%
        </Text>
      )}
    </Text>
  );
}

const STATUS_ICON = {
  failing: { name: 'circle-x', color: 'status-error' },
  passing: { name: 'circle-check', color: 'status-healthy' },
  no_minimum: { name: 'minus', color: 'subtle' },
  not_enough_runs: { name: 'clock', color: 'subtle' },
} as const satisfies Record<GraderStatus, { name: string; color: string }>;

type Filter = 'all' | 'failing' | 'passing' | 'no_minimum';

const FILTER_LABEL: Record<Filter, string> = {
  all: 'All',
  failing: 'Failing',
  passing: 'Passing',
  no_minimum: 'No minimum',
};

const matches = (g: GraderSummary, q: string) =>
  g.name.toLowerCase().includes(q) || g.description.toLowerCase().includes(q);

const COLUMNS: DataTableColumn<GraderSummary>[] = [
  {
    id: 'grader',
    header: 'Grader',
    gridWidth: 'minmax(0, 1fr)',
    cell: (g) => {
      const icon = STATUS_ICON[g.status];
      return (
        <Stack direction="horizontal" align="start" gap="snug" minWidth="none">
          <Icon
            name={icon.name}
            size="sm"
            color={icon.color}
            label={GRADER_STATUS_LABEL[g.status]}
          />
          <Stack gap="tight" minWidth="none">
            <Text
              size="supporting"
              weight={g.status === 'failing' ? 'emphasis' : 'regular'}
              truncate
            >
              {graderName(g)}
            </Text>
            <Text size="caption" color="subtle" mono>
              {g.name}
            </Text>
          </Stack>
        </Stack>
      );
    },
  },
  {
    id: 'rate',
    header: 'Passed',
    gridWidth: '11rem',
    align: 'right',
    cell: (g) =>
      g.status === 'not_enough_runs' ? (
        <Text size="supporting" color="subtle" tabular>
          {GRADER_STATUS_LABEL.not_enough_runs}
        </Text>
      ) : (
        <RateText g={g} />
      ),
  },
  {
    id: 'runs',
    header: 'Graded runs',
    gridWidth: '7rem',
    align: 'right',
    cell: (g) => (
      <Text size="supporting" color="muted" tabular>
        {g.graded}
        {g.too_large > 0 && ` · ${g.too_large} too large`}
      </Text>
    ),
  },
];

/** The agent's Graders in the API's order, failing first. */
export function GradersList({
  graders,
  onOpen,
}: {
  graders: GraderSummary[];
  onOpen: (name: string) => void;
}) {
  const [filter, setFilter] = useState<Filter>('all');
  if (!graders.length)
    return (
      <EmptyState
        icon="file-check"
        title="No Graders"
        description="This agent has no Grader resources"
      />
    );

  const count = (f: Filter) =>
    f === 'all' ? graders.length : graders.filter((g) => g.status === f).length;
  const rows = filter === 'all' ? graders : graders.filter((g) => g.status === filter);

  return (
    <DataTable
      // A new filter starts again from page one.
      key={filter}
      data={rows}
      columns={COLUMNS}
      getRowKey={(g) => g.name}
      columnToggle={false}
      noun="Graders"
      onRowClick={(g) => onOpen(g.name)}
      rowLabel={(g) => `Open ${graderName(g)}`}
      searchFn={matches}
      searchPlaceholder="Search Graders…"
      emptyMessage="No Graders match this filter."
      quickFilters={
        <SegmentedControl
          aria-label="Grader status"
          value={filter}
          onChange={(v) => setFilter(v as Filter)}
          options={(Object.keys(FILTER_LABEL) as Filter[]).map((f) => ({
            value: f,
            label: FILTER_LABEL[f],
            count: count(f),
          }))}
        />
      }
      pagination={{ pageSizeOptions: PAGE_SIZES, defaultPageSize: DEFAULT_PAGE_SIZE }}
      toolbarTitle={
        <Stack gap="tight">
          <Text size="supporting" weight="emphasis" tabular>
            {count('failing')} of {graders.length} Graders failing
          </Text>
          <Text size="caption" color="subtle" tabular>
            Last {WINDOW} graded runs per Grader
          </Text>
        </Stack>
      }
    />
  );
}
