import { DollarSign, Zap, Activity } from 'lucide-react';
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

// ── Types ─────────────────────────────────────────────────────────────────────

export interface StatsShape {
  spend: { period: number; month: number; quarter: number; year: number };
  requests: { total: number; successful: number; errored: number };
  tokens: { period: number; month: number; quarter: number; year: number };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function fmt(usd: number) {
  return `$${usd.toFixed(2)}`;
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

/**
 * Separator omits className — wrap in a figure to apply vertical margin.
 */
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

function CostCard({ spend }: { spend: StatsShape['spend'] }) {
  return (
    <Card column>
      <CardHeader compact icon={<DollarSign className="size-4" aria-hidden />}>
        <CardTitle>Costs</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmt(spend.period)}</Heading>
        <Text size="caption" color="muted">
          Current billing period
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="This month" value={fmt(spend.month)} />
          <Separator />
          <StatRow label="This quarter" value={fmt(spend.quarter)} />
          <Separator />
          <StatRow label="This year" value={fmt(spend.year)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

function RequestsCard({ requests }: { requests: StatsShape['requests'] }) {
  const successRate =
    requests.total > 0 ? ((requests.successful / requests.total) * 100).toFixed(1) : '0.0';

  return (
    <Card column>
      <CardHeader compact icon={<Activity className="size-4" aria-hidden />}>
        <CardTitle>Requests</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtReqs(requests.total)}</Heading>
        <Text size="caption" color="status-healthy">
          {successRate}% success rate
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="Successful" value={fmtReqs(requests.successful)} />
          <Separator />
          <StatRow label="Failed" value={fmtReqs(requests.errored)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

function TokensCard({ tokens }: { tokens: StatsShape['tokens'] }) {
  return (
    <Card column>
      <CardHeader compact icon={<Zap className="size-4" aria-hidden />}>
        <CardTitle>Tokens</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtTokens(tokens.period)}</Heading>
        <Text size="caption" color="muted">
          Current billing period
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="This month" value={fmtTokens(tokens.month)} />
          <Separator />
          <StatRow label="This quarter" value={fmtTokens(tokens.quarter)} />
          <Separator />
          <StatRow label="This year" value={fmtTokens(tokens.year)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

// ── Export ────────────────────────────────────────────────────────────────────

export interface StatCardsProps {
  stats: StatsShape;
}

export function StatCards({ stats }: StatCardsProps) {
  return (
    <Grid cols={{ base: 1, lg: 3 }} gap="comfortable">
      <CostCard spend={stats.spend} />
      <RequestsCard requests={stats.requests} />
      <TokensCard tokens={stats.tokens} />
    </Grid>
  );
}
