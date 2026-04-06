import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Badge,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import { STATS } from '../../_data/mock';

// ── StatLine ──────────────────────────────────────────────────────────────────

function StatLine({
  label,
  value,
  mono = false,
}: {
  label: string;
  value: React.ReactNode;
  mono?: boolean;
}) {
  return (
    <Stack asChild direction="horizontal" align="center" justify="between" gap="comfortable">
      <section className="py-2">
        <Text size="supporting" color="muted">
          {label}
        </Text>
        <Text size="supporting" weight="emphasis" mono={mono}>
          {value}
        </Text>
      </section>
    </Stack>
  );
}

// ── UsageOverviewCard ─────────────────────────────────────────────────────────

const TIER_CONFIG: Record<
  string,
  { label: string; variant: 'default' | 'secondary' | 'outline' | 'dim' }
> = {
  Free: { label: 'Free', variant: 'outline' },
  Pro: { label: 'Pro', variant: 'default' },
  Team: { label: 'Team', variant: 'secondary' },
  Enterprise: { label: 'Enterprise', variant: 'dim' },
};

const MOCK_SETUP = {
  tier: 'Pro' as const,
  spendLimit: 500,
  spent: STATS.spend.period,
  resetDate: new Date('2025-07-01').toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  }),
};

export function UsageOverviewCard() {
  const { tier, spendLimit, spent, resetDate } = MOCK_SETUP;
  const tierCfg = TIER_CONFIG[tier] ?? { label: tier, variant: 'outline' as const };
  const pct = Math.min(100, (spent / spendLimit) * 100);
  const remaining = spendLimit - spent;

  return (
    <Card column>
      <CardHeader>
        <Stack direction="horizontal" align="start" justify="between" gap="tight">
          <Stack gap="none">
            <CardTitle>Usage &amp; Limits</CardTitle>
            <CardDescription>Current spend period and account tier</CardDescription>
          </Stack>
          <Badge variant={tierCfg.variant}>{tierCfg.label}</Badge>
        </Stack>
      </CardHeader>
      <CardContent>
        <Stack gap="none">
          <StatLine label="Tier" value={tier} />
          <Separator />
          <StatLine label="Spend limit" value={`$${spendLimit.toLocaleString()}.00`} mono />
          <Separator />
          <StatLine label="Spent this period" value={`$${spent.toFixed(2)}`} mono />
          <Separator />
          <StatLine label="Remaining" value={`$${remaining.toFixed(2)}`} mono />
          <Separator />
          <StatLine label="Period resets on" value={resetDate} />
        </Stack>

        {/* Progress bar */}
        <Stack asChild gap="tight">
          <figure className="mt-4 m-0">
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="caption" color="muted">
                {pct.toFixed(1)}% of limit used
              </Text>
              <Text size="caption" color="muted">
                ${remaining.toFixed(2)} remaining
              </Text>
            </Stack>
            <span className="block h-2 w-full overflow-hidden rounded-full bg-surface-elevated">
              <span
                className={[
                  'block h-full rounded-full transition-all',
                  pct >= 90
                    ? 'bg-status-error'
                    : pct >= 70
                      ? 'bg-status-degraded'
                      : 'bg-status-healthy',
                ].join(' ')}
                style={{ width: `${pct}%` }}
                role="progressbar"
                aria-valuenow={pct}
                aria-valuemin={0}
                aria-valuemax={100}
              />
            </span>
          </figure>
        </Stack>
      </CardContent>
    </Card>
  );
}
