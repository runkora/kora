import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Badge,
  Separator,
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
    <div className="flex items-center justify-between gap-4 py-2">
      <Text size="supporting" color="muted">
        {label}
      </Text>
      <Text size="supporting" weight="emphasis" mono={mono}>
        {value}
      </Text>
    </div>
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
        <div className="flex items-start justify-between gap-2">
          <div>
            <CardTitle>Usage &amp; Limits</CardTitle>
            <CardDescription>Current spend period and account tier</CardDescription>
          </div>
          <Badge variant={tierCfg.variant}>{tierCfg.label}</Badge>
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col gap-0">
          <StatLine label="Tier" value={tier} />
          <Separator />
          <StatLine label="Spend limit" value={`$${spendLimit.toLocaleString()}.00`} mono />
          <Separator />
          <StatLine label="Spent this period" value={`$${spent.toFixed(2)}`} mono />
          <Separator />
          <StatLine label="Remaining" value={`$${remaining.toFixed(2)}`} mono />
          <Separator />
          <StatLine label="Period resets on" value={resetDate} />
        </div>

        {/* Progress bar */}
        <div className="mt-4 flex flex-col gap-1.5">
          <div className="flex items-center justify-between">
            <Text size="caption" color="muted">
              {pct.toFixed(1)}% of limit used
            </Text>
            <Text size="caption" color="muted">
              ${remaining.toFixed(2)} remaining
            </Text>
          </div>
          <div className="h-2 w-full overflow-hidden rounded-full bg-surface-elevated">
            <div
              className={[
                'h-full rounded-full transition-all',
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
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
