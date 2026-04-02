import { Sparkles, Zap, DollarSign } from 'lucide-react';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
  Heading,
  Text,
} from '@orbit/terra';
import { MODELS } from '../../_data/mock';
import { ProviderIcon } from './provider-icon';
import { AvailabilityDot } from './availability-dot';

// ── curated list ──────────────────────────────────────────────────────────────

interface TopModel {
  id: string;
  badge: string;
  badgeIcon: React.ElementType;
  reason: string;
}

const TOP_PICKS: TopModel[] = [
  {
    id: 'claude-3-5-sonnet-20241022',
    badge: 'Best overall',
    badgeIcon: Sparkles,
    reason: 'Highest quality output across coding, analysis and long-context tasks.',
  },
  {
    id: 'gpt-4o-mini',
    badge: 'Best value',
    badgeIcon: DollarSign,
    reason: 'Lowest cost-per-token for everyday tasks with strong accuracy.',
  },
  {
    id: 'gemini-1.5-flash-002',
    badge: 'Fastest',
    badgeIcon: Zap,
    reason: 'Sub-400 ms median latency — ideal for real-time agent interactions.',
  },
];

// ── TopModelCard ──────────────────────────────────────────────────────────────

function TopModelCard({ pick }: { pick: TopModel }) {
  const model = MODELS.find((m) => m.id === pick.id);
  if (!model) return null;
  const Icon = pick.badgeIcon;

  return (
    <Card column>
      <CardHeader compact>
        <div className="flex items-start justify-between gap-2">
          <div className="flex items-center gap-2">
            <ProviderIcon provider={model.provider} size="md" />
            <div className="min-w-0">
              <CardTitle>{model.name}</CardTitle>
              <Text size="caption" color="muted">
                {model.provider}
              </Text>
            </div>
          </div>
          <div className="shrink-0">
            <Badge variant="secondary" size="sm">
              <span className="flex items-center gap-1">
                <Icon className="size-3" />
                {pick.badge}
              </span>
            </Badge>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col gap-3">
          <Text size="supporting" color="muted">
            {pick.reason}
          </Text>

          <div className="grid grid-cols-3 gap-2 rounded-lg bg-surface-elevated px-3 py-2.5">
            <div className="flex flex-col gap-0.5">
              <Text size="caption" color="muted">
                Latency
              </Text>
              <Text size="supporting" mono tabular weight="emphasis">
                {model.latencyMs === 0 ? '—' : `${model.latencyMs.toLocaleString()} ms`}
              </Text>
            </div>
            <div className="flex flex-col gap-0.5">
              <Text size="caption" color="muted">
                Context
              </Text>
              <Text size="supporting" mono tabular weight="emphasis">
                {model.contextK >= 1000 ? `${model.contextK / 1000}M` : `${model.contextK}k`}
              </Text>
            </div>
            <div className="flex flex-col gap-0.5">
              <Text size="caption" color="muted">
                Prompt / 1M
              </Text>
              <Text size="supporting" mono tabular weight="emphasis">
                ${model.promptPer1M.toFixed(2)}
              </Text>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <AvailabilityDot status={model.status} />
            <Text size="caption" color="muted">
              {model.status === 'available' ? 'Available' : 'Unavailable'}
            </Text>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

// ── TopModels ─────────────────────────────────────────────────────────────────

export function TopModels() {
  return (
    <div className="flex flex-col gap-3">
      <div>
        <Heading variant="section">Top picks</Heading>
        <div className="mt-0.5">
          <Text size="supporting" color="muted">
            Curated recommendations from the Orbit team
          </Text>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        {TOP_PICKS.map((pick) => (
          <TopModelCard key={pick.id} pick={pick} />
        ))}
      </div>
    </div>
  );
}
