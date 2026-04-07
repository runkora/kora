'use client';

import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  Grid,
  Heading,
  Progress,
  Stack,
  Text,
} from '@orbit/terra';
import { Edit2, Trash2 } from 'lucide-react';

// ── Types & Mock Data ─────────────────────────────────────────────────────────

interface ApiKey {
  id: string;
  name: string;
  created: string;
  lastUsed: string;
  status: 'active' | 'expired';
  spend: number;
  limit: number;
  requests: number;
  rateLimit: string;
}

const MOCK_KEYS: ApiKey[] = [
  {
    id: 'key_01HZ9K3V',
    name: 'Production Agent Fleet',
    created: '2026-03-12',
    lastUsed: '2 min ago',
    status: 'active',
    spend: 412.3,
    limit: 500,
    requests: 182400,
    rateLimit: '1000 RPM',
  },
  {
    id: 'key_02JA8M4W',
    name: 'Staging Environment',
    created: '2026-03-18',
    lastUsed: '14 min ago',
    status: 'active',
    spend: 48.9,
    limit: 100,
    requests: 24100,
    rateLimit: '200 RPM',
  },
  {
    id: 'key_03KB9N5X',
    name: 'Dev / Local Testing',
    created: '2026-04-01',
    lastUsed: '3 hours ago',
    status: 'active',
    spend: 12.4,
    limit: 50,
    requests: 8900,
    rateLimit: '50 RPM',
  },
  {
    id: 'key_04LC0P6Y',
    name: 'CI Pipeline Runner',
    created: '2026-02-28',
    lastUsed: '6 hours ago',
    status: 'active',
    spend: 89.2,
    limit: 200,
    requests: 41200,
    rateLimit: '500 RPM',
  },
  {
    id: 'key_05MD1Q7Z',
    name: 'Deprecated v1 Key',
    created: '2025-12-15',
    lastUsed: '32 days ago',
    status: 'expired',
    spend: 0,
    limit: 0,
    requests: 0,
    rateLimit: '—',
  },
];

// ── Helpers ───────────────────────────────────────────────────────────────────

function getSpendVariant(spend: number, limit: number): 'healthy' | 'degraded' | 'error' {
  if (limit === 0) return 'healthy';
  const percent = (spend / limit) * 100;
  if (percent > 80) return 'error';
  if (percent > 60) return 'degraded';
  return 'healthy';
}

function fmtNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

// ── Key Card Component ────────────────────────────────────────────────────────

interface KeyCardProps {
  key_: ApiKey;
}

function KeyCard({ key_ }: KeyCardProps) {
  const spendPercent = key_.limit > 0 ? (key_.spend / key_.limit) * 100 : 0;
  const spendVariant = getSpendVariant(key_.spend, key_.limit);
  const statusBadgeVariant = key_.status === 'active' ? 'healthy' : 'error';

  return (
    <Stack asChild>
      <article className="relative">
        <Card>
          {/* Header */}
          <CardHeader compact>
            <Stack direction="horizontal" align="center" justify="between">
              <Stack direction="horizontal" align="center" gap="snug">
                <Heading variant="label">{key_.name}</Heading>
                <Badge variant={statusBadgeVariant} size="sm" dot>
                  {key_.status === 'active' ? 'Active' : 'Expired'}
                </Badge>
              </Stack>
              <Stack direction="horizontal" gap="tight">
                <button
                  type="button"
                  aria-label="Edit key"
                  className="inline-flex items-center justify-center rounded p-1 transition-colors hover:bg-surface-overlay focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <Edit2 className="h-3.5 w-3.5" />
                </button>
                <button
                  type="button"
                  aria-label="Delete key"
                  className="inline-flex items-center justify-center rounded p-1 transition-colors hover:bg-surface-overlay focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </Stack>
            </Stack>
          </CardHeader>

          <CardContent>
            <Stack gap="snug">
              {/* Metadata Grid */}
              <Grid cols={{ base: 2, md: 4 }} gap="snug">
                {/* Created */}
                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Created
                  </Text>
                  <Text size="supporting" color="foreground" mono>
                    {key_.created}
                  </Text>
                </Stack>

                {/* Last Used */}
                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Last used
                  </Text>
                  <Text size="supporting" color="foreground" mono>
                    {key_.lastUsed}
                  </Text>
                </Stack>

                {/* Requests */}
                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Requests
                  </Text>
                  <Text size="supporting" color="foreground" mono tabular>
                    {fmtNum(key_.requests)}
                  </Text>
                </Stack>

                {/* Rate Limit */}
                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Rate limit
                  </Text>
                  <Text size="supporting" color="foreground" mono>
                    {key_.rateLimit}
                  </Text>
                </Stack>
              </Grid>

              {/* Spend / Limit with Progress Bar */}
              {key_.limit > 0 && (
                <Stack gap="snug">
                  <Stack direction="horizontal" justify="between" align="center">
                    <Text size="caption" color="subtle" weight="emphasis">
                      Spend / Limit
                    </Text>
                    <Text size="supporting" color="foreground" mono tabular>
                      ${key_.spend.toFixed(2)} / ${key_.limit.toFixed(2)}
                    </Text>
                  </Stack>
                  <Progress
                    value={spendPercent}
                    max={100}
                    variant={spendVariant}
                    size="md"
                    label="Spend usage"
                  />
                </Stack>
              )}
            </Stack>
          </CardContent>
        </Card>
      </article>
    </Stack>
  );
}

// ── Keys Tab Component ────────────────────────────────────────────────────────

export function KeysTab() {
  return (
    <Stack gap="comfortable">
      {/* Header */}
      <Stack direction="horizontal" align="start" justify="between">
        <Stack gap="none">
          <Heading variant="label">API keys</Heading>
          <Text size="supporting" color="muted">
            Manage your API keys and monitor their usage
          </Text>
        </Stack>
        <Button variant="default" size="sm">
          Create key
        </Button>
      </Stack>

      {/* Key Cards */}
      <Stack gap="comfortable">
        {MOCK_KEYS.map((key_) => (
          <KeyCard key={key_.id} key_={key_} />
        ))}
      </Stack>
    </Stack>
  );
}
