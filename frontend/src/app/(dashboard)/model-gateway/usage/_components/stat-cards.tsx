import { DollarSign, Zap, Activity } from 'lucide-react';
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Grid,
  Heading,
  Separator,
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
    <div className="flex items-center justify-between py-1.5">
      <Text size="supporting" color="muted">
        {label}
      </Text>
      <Text size="supporting" weight="emphasis" mono tabular>
        {value}
      </Text>
    </div>
  );
}

/**
 * Separator omits className — wrap in a div to apply vertical margin.
 */
function CardDivider() {
  return (
    <div className="my-3">
      <Separator span="full" />
    </div>
  );
}

// ── Cards ─────────────────────────────────────────────────────────────────────

function CostCard({ spend }: { spend: StatsShape['spend'] }) {
  return (
    <Card column>
      <CardHeader compact>
        <div className="flex items-center gap-2">
          <div className="flex size-8 items-center justify-center rounded-md bg-surface-elevated border border-border-subtle text-muted">
            <DollarSign className="size-4" aria-hidden />
          </div>
          <CardTitle>Costs</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmt(spend.period)}</Heading>
        <div className="mt-0.5">
          <Text size="caption" color="muted">
            Current billing period
          </Text>
        </div>
        <CardDivider />
        <div className="divide-y divide-border-subtle">
          <StatRow label="This month" value={fmt(spend.month)} />
          <StatRow label="This quarter" value={fmt(spend.quarter)} />
          <StatRow label="This year" value={fmt(spend.year)} />
        </div>
      </CardContent>
    </Card>
  );
}

function RequestsCard({ requests }: { requests: StatsShape['requests'] }) {
  const successRate =
    requests.total > 0
      ? ((requests.successful / requests.total) * 100).toFixed(1)
      : '0.0';

  return (
    <Card column>
      <CardHeader compact>
        <div className="flex items-center gap-2">
          <div className="flex size-8 items-center justify-center rounded-md bg-surface-elevated border border-border-subtle text-muted">
            <Activity className="size-4" aria-hidden />
          </div>
          <CardTitle>Requests</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtReqs(requests.total)}</Heading>
        <div className="mt-0.5">
          <Text size="caption" color="status-healthy">
            {successRate}% success rate
          </Text>
        </div>
        <CardDivider />
        <div className="divide-y divide-border-subtle">
          <StatRow label="Successful" value={fmtReqs(requests.successful)} />
          <StatRow label="Failed" value={fmtReqs(requests.errored)} />
        </div>
      </CardContent>
    </Card>
  );
}

function TokensCard({ tokens }: { tokens: StatsShape['tokens'] }) {
  return (
    <Card column>
      <CardHeader compact>
        <div className="flex items-center gap-2">
          <div className="flex size-8 items-center justify-center rounded-md bg-surface-elevated border border-border-subtle text-muted">
            <Zap className="size-4" aria-hidden />
          </div>
          <CardTitle>Tokens</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtTokens(tokens.period)}</Heading>
        <div className="mt-0.5">
          <Text size="caption" color="muted">
            Current billing period
          </Text>
        </div>
        <CardDivider />
        <div className="divide-y divide-border-subtle">
          <StatRow label="This month" value={fmtTokens(tokens.month)} />
          <StatRow label="This quarter" value={fmtTokens(tokens.quarter)} />
          <StatRow label="This year" value={fmtTokens(tokens.year)} />
        </div>
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
