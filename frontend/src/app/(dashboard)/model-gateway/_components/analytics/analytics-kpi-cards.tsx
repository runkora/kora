'use client';

import { DollarSign, Zap, Activity, Timer } from 'lucide-react';
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Grid,
  Heading,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import type { AnalyticsKPIs } from '../../_types';

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmtUsd(n: number) {
  return `$${n.toFixed(4)}`;
}

function fmtTokens(n: number) {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
  return n.toLocaleString();
}

function fmtReqs(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

function fmtPct(r: number) {
  return `${(r * 100).toFixed(1)}%`;
}

function fmtMs(ms: number) {
  return `${Math.round(ms)}ms`;
}

// ── Sub-components ────────────────────────────────────────────────────────────

interface StatRowProps {
  label: string;
  value: string;
}

function StatRow({ label, value }: StatRowProps) {
  return (
    <Stack asChild direction="horizontal" align="center" justify="between">
      <section className="py-1.5">
        <Text size="supporting" color="muted">
          {label}
        </Text>
        <Text size="supporting" weight="emphasis" mono tabular>
          {value}
        </Text>
      </section>
    </Stack>
  );
}

function CardDivider() {
  return (
    <Stack asChild>
      <figure className="my-3 m-0">
        <Separator span="full" />
      </figure>
    </Stack>
  );
}

// ── Cards ─────────────────────────────────────────────────────────────────────

function CostCard({ kpis }: { kpis: AnalyticsKPIs }) {
  return (
    <Card column>
      <CardHeader compact icon={<DollarSign className="size-4" aria-hidden />}>
        <CardTitle>Cost</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtUsd(kpis.estimated_cost_usd)}</Heading>
        <Text size="caption" color="muted">
          Selected period
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="Cache hit rate" value={fmtPct(kpis.cache_hit_rate)} />
          <Separator />
          <StatRow label="Error rate" value={fmtPct(kpis.error_rate)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

function RequestsCard({ kpis }: { kpis: AnalyticsKPIs }) {
  const successRate = kpis.total_requests > 0 ? 1 - kpis.error_rate : 0;
  const failed = Math.round(kpis.total_requests * kpis.error_rate);
  const successful = kpis.total_requests - failed;

  return (
    <Card column>
      <CardHeader compact icon={<Activity className="size-4" aria-hidden />}>
        <CardTitle>Requests</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtReqs(kpis.total_requests)}</Heading>
        <Text size="caption" color="status-healthy">
          {fmtPct(successRate)} success rate
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="Successful" value={fmtReqs(successful)} />
          <Separator />
          <StatRow label="Failed" value={fmtReqs(failed)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

function TokensCard({ kpis }: { kpis: AnalyticsKPIs }) {
  return (
    <Card column>
      <CardHeader compact icon={<Zap className="size-4" aria-hidden />}>
        <CardTitle>Tokens</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtTokens(kpis.total_tokens)}</Heading>
        <Text size="caption" color="muted">
          Total consumed
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="Input" value={fmtTokens(kpis.prompt_tokens)} />
          <Separator />
          <StatRow label="Output" value={fmtTokens(kpis.completion_tokens)} />
          <Separator />
          <StatRow label="Cache read" value={fmtTokens(kpis.cache_read_tokens)} />
          <Separator />
          <StatRow label="Cache write" value={fmtTokens(kpis.cache_creation_tokens)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

function LatencyCard({ kpis }: { kpis: AnalyticsKPIs }) {
  return (
    <Card column>
      <CardHeader compact icon={<Timer className="size-4" aria-hidden />}>
        <CardTitle>Latency</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtMs(kpis.avg_latency_ms)}</Heading>
        <Text size="caption" color="muted">
          Average
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="p95" value={fmtMs(kpis.p95_latency_ms)} />
          <Separator />
          <StatRow label="p99" value={fmtMs(kpis.p99_latency_ms)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

// ── Export ────────────────────────────────────────────────────────────────────

export function AnalyticsKPICards({ kpis }: { kpis: AnalyticsKPIs }) {
  return (
    <Grid cols={{ base: 1, md: 2, lg: 4 }} gap="comfortable">
      <CostCard kpis={kpis} />
      <RequestsCard kpis={kpis} />
      <TokensCard kpis={kpis} />
      <LatencyCard kpis={kpis} />
    </Grid>
  );
}
