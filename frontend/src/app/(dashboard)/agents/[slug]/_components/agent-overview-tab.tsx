'use client';

/** @tier 3 · agents/[slug]-scoped. The Overview tab. */

import { Card, CardContent, CardHeader, Grid, Stack, Text } from '@clear-route/hades';
import { fmtTokens, fmtUsd } from '@orbit/utils';
import { StatTile } from '../../_components/agent-stat-tiles';
import { Breakdown } from '../../_components/agents-spend-card';
import { DEFAULT_PERIOD, PERIOD_DAYS, PERIOD_TEXT, changeText } from '../../_lib/spend';
import type { AgentOverview } from '../../_lib/overview';
import { formatDuration } from '../../_types';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import type { WindowStatus } from '../../_hooks/use-period-cache';
import { AgentInstructionsCard } from './agent-instructions-card';
import { GrantScopesCard, MetadataCard, RecentRunsTable } from './agent-overview-cards';
import { AgentSpendCard, shortModel } from './agent-spend-card';
import { DurationHistogram } from './duration-histogram';

interface AgentOverviewTabProps {
  /** Absent until a period has loaded, or when the caller may read neither part. */
  overview: AgentOverview | undefined;
  /** The selected period's load state; the figures on screen dim while it loads. */
  status: WindowStatus;
  /** Opens the Configuration tab at the Behaviour step. */
  onEditInstructions: () => void;
}

function CouldNotLoad({ title }: { title: string }) {
  return (
    <Card>
      <CardHeader compact title={title} divider />
      <CardContent padded>
        <Text size="supporting" color="muted">
          Could not load
        </Text>
      </CardContent>
    </Card>
  );
}

const perDay = (total: number, period: AgentOverview['period']) => {
  const n = total / PERIOD_DAYS[period];
  return n < 10 ? n.toFixed(1) : String(Math.round(n));
};

/** The Spend tile names the two largest models and folds the rest together. */
function modelSegments(models: { name: string; cost: number }[]) {
  const top = models.slice(0, 2).map((m) => ({
    label: shortModel(m.name),
    value: m.cost,
    text: fmtUsd(m.cost),
  }));
  const other = models.slice(2).reduce((n, m) => n + m.cost, 0);
  return other > 0 ? [...top, { label: 'other', value: other, text: fmtUsd(other) }] : top;
}

export function AgentOverviewTab({ overview, status, onEditInstructions }: AgentOverviewTabProps) {
  const { agent } = useAgentDetailContext();
  if (!agent) return null;

  const usage = overview?.usage;
  const runs = overview?.runs;
  const period = overview?.period ?? DEFAULT_PERIOD;
  const { text: periodText, previous: previousText } = PERIOD_TEXT[period];
  // Without figures the period status says whether the load failed; with them each part does.
  const runsFailed = overview ? overview.failed.runs : status === 'failed';
  const usageFailed = overview ? overview.failed.usage : status === 'failed';
  const loading = status === 'loading';

  return (
    <Stack
      gap="comfortable"
      aria-busy={loading}
      style={{ opacity: loading ? 0.6 : 1, transition: 'opacity 150ms' }}
    >
      {(usage || runs || runsFailed || usageFailed) && (
        <Grid cols={{ base: 2, md: 3, xl: 5 }} gap="comfortable">
          {runsFailed && <CouldNotLoad title="Run stats" />}
          {usageFailed && <CouldNotLoad title="Tokens and spend" />}
          {runs && (
            <>
              <StatTile
                label="Total runs"
                icon="play"
                value={String(runs.total)}
                segments={[
                  { label: 'triggered', value: runs.triggered },
                  { label: 'manual', value: runs.manual },
                ]}
                period={periodText}
                trailing={runs.total > 0 ? `${perDay(runs.total, period)} a day` : 'No runs yet'}
              />
              <StatTile
                label="Completed runs"
                icon="circle-check"
                value={String(runs.completed)}
                segments={[
                  { label: 'completed', value: runs.completed },
                  { label: 'failed', value: runs.failed },
                ]}
                period={periodText}
                trailing={
                  runs.successRate === null ? 'No runs yet' : `${runs.successRate}% success`
                }
              />
              <StatTile
                label="Median duration"
                icon="clock"
                value={runs.finished > 0 ? formatDuration(runs.durations.median_ms) : '–'}
                period={periodText}
                trailing={runs.finished > 0 ? `${runs.finished} finished` : 'No runs yet'}
              >
                <DurationHistogram durations={runs.durations} finished={runs.finished} />
              </StatTile>
            </>
          )}
          {usage && (
            <>
              <StatTile
                label="Tokens"
                icon="zap"
                value={fmtTokens(usage.tokens.total)}
                segments={[
                  {
                    label: 'cached',
                    value: usage.tokens.cached,
                    text: fmtTokens(usage.tokens.cached),
                  },
                  {
                    label: 'input',
                    value: usage.tokens.input,
                    text: fmtTokens(usage.tokens.input),
                  },
                  {
                    label: 'output',
                    value: usage.tokens.output,
                    text: fmtTokens(usage.tokens.output),
                  },
                ]}
                period={periodText}
                trailing={
                  usage.cachedShare === null ? 'No usage yet' : `${usage.cachedShare}% cached`
                }
              />
              <StatTile
                label="Spend"
                icon="dollar-sign"
                value={fmtUsd(usage.spend)}
                segments={modelSegments(usage.models)}
                period={periodText}
                trailing={usage.change === null ? 'No usage yet' : changeText(usage.change)}
              />
            </>
          )}
        </Grid>
      )}

      {/* Columns stretch to full width; align="start" would size them to their content on mobile. */}
      <Stack direction={{ base: 'vertical', xl: 'horizontal' }} gap="comfortable">
        <Stack gap="comfortable" grow minWidth="none">
          <RecentRunsTable />
          <AgentInstructionsCard prompt={agent.systemPrompt} onEdit={onEditInstructions} />
        </Stack>

        <Stack
          gap="comfortable"
          minWidth={{ base: 'none', xl: 'detail-panel' }}
          maxWidth={{ base: 'full', xl: 'detail-panel' }}
          shrink={false}
        >
          {usage && (
            <>
              <AgentSpendCard
                models={usage.models}
                trend={usage.trend}
                total={usage.spend}
                previousTotal={usage.previousSpend}
                change={usage.change}
                periodText={periodText}
                previousText={previousText}
                budget={overview?.budget ?? null}
              />
              <Card>
                <CardHeader
                  compact
                  title="Token usage"
                  description={`${periodText.charAt(0).toUpperCase()}${periodText.slice(1)}`}
                  divider
                />
                <CardContent padded>
                  <Breakdown
                    rows={[
                      { name: 'Cached', cost: usage.tokens.cached },
                      { name: 'Input', cost: usage.tokens.input },
                      { name: 'Output', cost: usage.tokens.output },
                    ]}
                    total={usage.tokens.total}
                    format={fmtTokens}
                  />
                </CardContent>
              </Card>
            </>
          )}
          <GrantScopesCard agent={agent} />
          <MetadataCard agent={agent} />
        </Stack>
      </Stack>
    </Stack>
  );
}
