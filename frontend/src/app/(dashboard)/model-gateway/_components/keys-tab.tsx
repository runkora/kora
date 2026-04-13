'use client';

import { useCallback, useState } from 'react';
import { useRouter } from 'next/navigation';
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
import Link from 'next/link';
import { KeyRound, ScrollText, Trash2 } from 'lucide-react';
import { revokeToken } from '../_actions/tokens';
import { CreateTokenModal } from './create-token-modal';
import { TokenCreatedModal } from './token-created-modal';
import { RevokeTokenModal } from './revoke-token-modal';
import type { CreatePATResponse, PAT } from '@orbit/shared/api';

// ── View model ────────────────────────────────────────────────────────────────
//
// Extends the backend PAT shape with analytics fields that will be populated
// once the backend supports per-key spend attribution. Until then they are
// undefined and the UI renders sensible fallbacks.

interface PATViewModel extends PAT {
  // TODO: replace with real usage data once backend supports PAT spend attribution
  spend?: number;
  spendLimit?: number;
  requests?: number;
  lastUsed?: string;
}

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

function fmtDate(iso: string | null | undefined): string {
  if (!iso) return 'Never expires';
  return new Date(iso).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  });
}

function isExpired(expiresAt: string | null | undefined): boolean {
  if (!expiresAt) return false;
  return new Date(expiresAt) < new Date();
}

// ── Key Card ──────────────────────────────────────────────────────────────────

interface KeyCardProps {
  pat: PATViewModel;
  onRevoke: () => void;
  revoking: boolean;
}

function KeyCard({ pat, onRevoke, revoking }: KeyCardProps) {
  const expired = isExpired(pat.expires_at);
  const statusBadgeVariant = expired ? 'error' : 'healthy';

  const hasSpend = pat.spend !== undefined && pat.spendLimit !== undefined && pat.spendLimit > 0;
  const spendPercent = hasSpend ? ((pat.spend ?? 0) / (pat.spendLimit ?? 1)) * 100 : 0;
  const spendVariant = hasSpend ? getSpendVariant(pat.spend ?? 0, pat.spendLimit ?? 0) : 'healthy';

  return (
    <Stack asChild>
      <article className={revoking ? 'pointer-events-none opacity-50' : undefined}>
        <Card>
          <CardHeader compact>
            <Stack direction="horizontal" align="center" justify="between">
              <Stack direction="horizontal" align="center" gap="snug">
                <Heading variant="label">{pat.name}</Heading>
                <Badge variant={statusBadgeVariant} size="sm" dot>
                  {expired ? 'Expired' : 'Active'}
                </Badge>
              </Stack>
              <Stack direction="horizontal" align="center" gap="tight">
                <Link
                  href={`/model-gateway/logs?key=${pat.id}`}
                  aria-label={`View logs for ${pat.name}`}
                  className="inline-flex items-center justify-center rounded p-1 text-muted transition-colors hover:bg-surface-overlay hover:text-foreground focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <ScrollText className="h-3.5 w-3.5" />
                </Link>
                <button
                  type="button"
                  aria-label={`Revoke key ${pat.name}`}
                  disabled={revoking}
                  onClick={onRevoke}
                  className="inline-flex items-center justify-center rounded p-1 text-muted transition-colors hover:bg-surface-overlay hover:text-status-error focus-visible:outline-2 focus-visible:outline-accent disabled:pointer-events-none disabled:opacity-40"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </Stack>
            </Stack>
          </CardHeader>

          <CardContent>
            <Stack gap="snug">
              <Grid cols={{ base: 2, md: 4 }} gap="snug">
                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Created
                  </Text>
                  <Text size="supporting" color="foreground" mono>
                    {fmtDate(pat.created_at)}
                  </Text>
                </Stack>

                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Expires
                  </Text>
                  <Text size="supporting" color="foreground" mono>
                    {fmtDate(pat.expires_at)}
                  </Text>
                </Stack>

                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Last used
                  </Text>
                  <Text size="supporting" color="foreground" mono>
                    {/* TODO: replace with real data once backend supports last-used tracking */}
                    {pat.lastUsed ?? '—'}
                  </Text>
                </Stack>

                <Stack gap="none">
                  <Text size="caption" color="subtle" weight="emphasis">
                    Requests
                  </Text>
                  <Text size="supporting" color="foreground" mono tabular>
                    {/* TODO: replace with real data once backend supports PAT spend attribution */}
                    {pat.requests !== undefined ? fmtNum(pat.requests) : '—'}
                  </Text>
                </Stack>
              </Grid>

              {hasSpend && (
                <Stack gap="snug">
                  <Stack direction="horizontal" justify="between" align="center">
                    <Text size="caption" color="subtle" weight="emphasis">
                      Spend / Limit
                    </Text>
                    <Text size="supporting" color="foreground" mono tabular>
                      ${(pat.spend ?? 0).toFixed(2)} / ${(pat.spendLimit ?? 0).toFixed(2)}
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

// ── Keys Tab ──────────────────────────────────────────────────────────────────

interface KeysTabProps {
  initialTokens: PAT[];
  initialError: string | null;
}

export function KeysTab({ initialTokens, initialError }: KeysTabProps) {
  const router = useRouter();

  const [createOpen, setCreateOpen] = useState(false);
  const [createdPAT, setCreatedPAT] = useState<CreatePATResponse | null>(null);
  const [revokeCandidate, setRevokeCandidate] = useState<PATViewModel | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [revokeError, setRevokeError] = useState<string | null>(null);

  const handleCreated = useCallback(
    (pat: CreatePATResponse) => {
      setCreateOpen(false);
      setCreatedPAT(pat);
      router.refresh();
    },
    [router],
  );

  const handleRevokeConfirm = useCallback(
    async (id: string) => {
      setRevokeError(null);
      setRevokingId(id);
      const result = await revokeToken(id);
      setRevokingId(null);
      if (result.ok) {
        setRevokeCandidate(null);
        router.refresh();
      } else {
        setRevokeError(result.message);
      }
    },
    [router],
  );

  return (
    <>
      <Stack gap="comfortable">
        {/* Header */}
        <Stack direction="horizontal" align="start" justify="between">
          <Stack gap="none">
            <Heading variant="label">API keys</Heading>
            <Text size="supporting" color="muted">
              Manage your API keys and monitor their usage
            </Text>
          </Stack>
          <Button variant="default" size="sm" onClick={() => setCreateOpen(true)}>
            Create key
          </Button>
        </Stack>

        {/* Body */}
        {initialError ? (
          <Text size="supporting" color="status-error">
            {initialError}
          </Text>
        ) : initialTokens.length === 0 ? (
          <Card>
            <CardContent padded>
              <Stack align="center" justify="center" gap="comfortable">
                {/* eslint-disable-next-line local/no-raw-div -- fixed-size icon circle */}
                <div className="flex size-12 items-center justify-center rounded-full bg-surface-elevated">
                  <KeyRound className="size-5 text-subtle" />
                </div>
                <Stack align="center" gap="tight">
                  <Heading variant="label">No API keys yet</Heading>
                  <Text size="supporting" color="muted" align="center">
                    Create a key to start making requests to the inference gateway.
                  </Text>
                </Stack>
                <Button variant="default" size="sm" onClick={() => setCreateOpen(true)}>
                  Create your first key
                </Button>
              </Stack>
            </CardContent>
          </Card>
        ) : (
          <Stack gap="comfortable">
            {initialTokens.map((pat) => (
              <KeyCard
                key={pat.id}
                pat={pat}
                onRevoke={() => setRevokeCandidate(pat)}
                revoking={revokingId === pat.id}
              />
            ))}
          </Stack>
        )}
      </Stack>

      {/* Modals */}
      <CreateTokenModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={handleCreated}
      />
      <TokenCreatedModal pat={createdPAT} onClose={() => setCreatedPAT(null)} />
      <RevokeTokenModal
        pat={revokeCandidate}
        revoking={revokingId === revokeCandidate?.id}
        error={revokeError}
        onConfirm={(id) => void handleRevokeConfirm(id)}
        onClose={() => {
          setRevokeCandidate(null);
          setRevokeError(null);
        }}
      />
    </>
  );
}
