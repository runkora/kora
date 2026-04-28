'use client';

import { useCallback, useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  Grid,
  Heading,
  Icon,
  Progress,
  Stack,
  Text,
  PageSectionHeader,
} from '@orbit/terra';
import Link from 'next/link';
import { revokeToken } from '../_actions/tokens';
import { CreateTokenModal } from './create-token-modal';
import { TokenCreatedModal } from './token-created-modal';
import { EditScopesModal } from './edit-scopes-modal';
import { ScopeChip } from '@orbit/components';
import type { CreatePATResponse, PAT } from '@orbit/shared/api';
import { fmtDate, fmtExpiry, fmtNumCompact, isExpired } from '@orbit/utils';
import type { TokenUsageStat } from '../_services/usage-server';
import { EmptyState } from '@orbit/components';
import { ConfirmDialog } from '@orbit/components';

// ── View model ────────────────────────────────────────────────────────────────

interface PATViewModel extends PAT {
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

// ── Key Card ──────────────────────────────────────────────────────────────────

interface KeyCardProps {
  pat: PATViewModel;
  onRevoke: () => void;
  onEditScopes: () => void;
  revoking: boolean;
}

function KeyCard({ pat, onRevoke, onEditScopes, revoking }: KeyCardProps) {
  const expired = isExpired(pat.expires_at);
  const statusBadgeVariant = expired ? 'error' : 'healthy';

  const hasSpend = pat.spend !== undefined && pat.spendLimit !== undefined && pat.spendLimit > 0;
  const spendPercent = hasSpend ? ((pat.spend ?? 0) / (pat.spendLimit ?? 1)) * 100 : 0;
  const spendVariant = hasSpend ? getSpendVariant(pat.spend ?? 0, pat.spendLimit ?? 0) : 'healthy';

  return (
    <Stack disabled={revoking}>
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
                <Icon name="scroll-text" size="2xs" />
              </Link>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Edit permissions for ${pat.name}`}
                onClick={onEditScopes}
              >
                <Icon name="edit" size="2xs" />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`Revoke key ${pat.name}`}
                disabled={revoking}
                onClick={onRevoke}
              >
                <Icon name="trash" size="2xs" />
              </Button>
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
                  {fmtExpiry(pat.created_at)}
                </Text>
              </Stack>

              <Stack gap="none">
                <Text size="caption" color="subtle" weight="emphasis">
                  Expires
                </Text>
                <Text size="supporting" color="foreground" mono>
                  {fmtExpiry(pat.expires_at)}
                </Text>
              </Stack>

              <Stack gap="none">
                <Text size="caption" color="subtle" weight="emphasis">
                  Last used
                </Text>
                <Text size="supporting" color="foreground" mono>
                  {pat.lastUsed ?? '—'}
                </Text>
              </Stack>

              <Stack gap="none">
                <Text size="caption" color="subtle" weight="emphasis">
                  Requests
                </Text>
                <Text size="supporting" color="foreground" mono tabular>
                  {pat.requests !== undefined ? fmtNumCompact(pat.requests) : '—'}
                </Text>
              </Stack>
            </Grid>

            {/* Scopes */}
            {pat.scopes && pat.scopes.length > 0 && (
              <Stack gap="tight">
                <Text size="caption" color="subtle" weight="emphasis">
                  Permissions
                </Text>
                <Stack direction="horizontal" gap="tight" wrap>
                  {pat.scopes.map((scope) => (
                    <ScopeChip key={scope} scope={scope} />
                  ))}
                </Stack>
              </Stack>
            )}

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
    </Stack>
  );
}

// ── Keys Tab ──────────────────────────────────────────────────────────────────

interface KeysTabProps {
  initialTokens: PAT[];
  initialError: string | null;
  effectiveScopes: string[];
  usageStats: TokenUsageStat[];
}

export function KeysTab({
  initialTokens,
  initialError,
  effectiveScopes,
  usageStats,
}: KeysTabProps) {
  const statsById = useMemo(
    () => Object.fromEntries(usageStats.map((s) => [s.token_id, s])),
    [usageStats],
  );
  const router = useRouter();

  const [createOpen, setCreateOpen] = useState(false);
  const [createdPAT, setCreatedPAT] = useState<CreatePATResponse | null>(null);
  const [revokeCandidate, setRevokeCandidate] = useState<PATViewModel | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [revokeError, setRevokeError] = useState<string | null>(null);
  const [editScopesCandidate, setEditScopesCandidate] = useState<PATViewModel | null>(null);

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

  const handleScopesUpdated = useCallback(() => {
    router.refresh();
  }, [router]);

  return (
    <>
      <Stack gap="comfortable">
        {/* Header */}
        <PageSectionHeader
          title="API keys"
          description="Manage your API keys and monitor their usage"
          action={
            <Button variant="default" size="sm" onClick={() => setCreateOpen(true)}>
              Create key
            </Button>
          }
        />

        {/* Body */}
        {initialError ? (
          <Text size="supporting" color="status-error">
            {initialError}
          </Text>
        ) : initialTokens.length === 0 ? (
          <EmptyState
            icon="key"
            title="No API keys yet"
            description="Create a key to start making requests to the inference gateway."
            action={
              <Button variant="default" size="sm" onClick={() => setCreateOpen(true)}>
                Create your first key
              </Button>
            }
          />
        ) : (
          <Stack gap="comfortable">
            {initialTokens.map((pat) => {
              const stat = statsById[pat.id];
              const viewModel = {
                ...pat,
                requests: stat?.request_count,
                lastUsed: stat ? fmtDate(stat.last_used) : undefined,
              };
              return (
                <KeyCard
                  key={pat.id}
                  pat={viewModel}
                  onRevoke={() => setRevokeCandidate(pat)}
                  onEditScopes={() => setEditScopesCandidate(pat)}
                  revoking={revokingId === pat.id}
                />
              );
            })}
          </Stack>
        )}
      </Stack>

      {/* Modals */}
      <CreateTokenModal
        open={createOpen}
        availableScopes={effectiveScopes}
        onClose={() => setCreateOpen(false)}
        onCreated={handleCreated}
      />
      <TokenCreatedModal pat={createdPAT} onClose={() => setCreatedPAT(null)} />
      <ConfirmDialog
        open={!!revokeCandidate}
        title="Revoke API key"
        description="This key will be permanently revoked and any applications using it will lose access immediately. This cannot be undone."
        confirmLabel="Revoke key"
        confirming={revokingId === revokeCandidate?.id}
        error={revokeError}
        onConfirm={() => revokeCandidate && void handleRevokeConfirm(revokeCandidate.id)}
        onClose={() => {
          setRevokeCandidate(null);
          setRevokeError(null);
        }}
      >
        {revokeCandidate && (
          <Text size="caption" mono weight="emphasis">
            {revokeCandidate.name}
          </Text>
        )}
      </ConfirmDialog>
      <EditScopesModal
        pat={editScopesCandidate}
        availableScopes={effectiveScopes}
        onClose={() => setEditScopesCandidate(null)}
        onUpdated={handleScopesUpdated}
      />
    </>
  );
}
