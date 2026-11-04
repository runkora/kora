import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Grid,
  Heading,
  Icon,
  Separator,
  Stack,
  Text,
} from '@runkora/hades';
import { fmtUsd, fmtTokens, fmtNumCompact } from '@runkora/utils';
import { StatRow } from './stat-row';

// ── Types ─────────────────────────────────────────────────────────────────────

export interface StatsShape {
  spend: { period: number; month: number; quarter: number; year: number };
  requests: { total: number; successful: number; errored: number };
  tokens: { period: number; month: number; quarter: number; year: number };
}

// ── Cards ─────────────────────────────────────────────────────────────────────

function CardDivider() {
  return (
    <Stack asChild>
      <figure className="my-3 m-0">
        <Separator />
      </figure>
    </Stack>
  );
}

function CostCard({ spend }: { spend: StatsShape['spend'] }) {
  return (
    <Card column>
      <CardHeader compact icon={<Icon name="dollar-sign" />}>
        <CardTitle>Costs</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtUsd(spend.period)}</Heading>
        <Text size="caption" color="muted">
          Current billing period
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="This month" value={fmtUsd(spend.month)} />
          <Separator />
          <StatRow label="This quarter" value={fmtUsd(spend.quarter)} />
          <Separator />
          <StatRow label="This year" value={fmtUsd(spend.year)} />
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
      <CardHeader compact icon={<Icon name="activity" />}>
        <CardTitle>Requests</CardTitle>
      </CardHeader>
      <CardContent>
        <Heading variant="metric">{fmtNumCompact(requests.total)}</Heading>
        <Text size="caption" color="status-healthy">
          {successRate}% success rate
        </Text>
        <CardDivider />
        <Stack gap="none">
          <StatRow label="Successful" value={fmtNumCompact(requests.successful)} />
          <Separator />
          <StatRow label="Failed" value={fmtNumCompact(requests.errored)} />
        </Stack>
      </CardContent>
    </Card>
  );
}

function TokensCard({ tokens }: { tokens: StatsShape['tokens'] }) {
  return (
    <Card column>
      <CardHeader compact icon={<Icon name="zap" />}>
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
