/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useCallback, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  Card,
  CardContent,
  CardHeader,
  Icon,
  Input,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  NativeSelect,
  Progress,
  Stack,
  Text,
} from '@orbit/terra';
import { usePermissions } from '../../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import { fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import {
  upsertTenantBudgetAction,
  deleteTenantBudgetAction,
} from '../../../policy/_actions/tenant-budget';
import type { TenantCapStatus } from '../../_types';

interface TenantCapCardProps {
  cap: TenantCapStatus | null;
}

export function TenantCapCard({ cap }: TenantCapCardProps) {
  const router = useRouter();
  const { scopes: sessionScopes } = usePermissions();
  const canWrite = hasScope(sessionScopes, 'platform:budgets:write');
  const [editing, setEditing] = useState(false);

  const pct = cap && cap.limit_usd > 0 ? (cap.spend_usd / cap.limit_usd) * 100 : 0;
  const variant = cap?.reached ? 'error' : pct >= 80 ? 'degraded' : 'healthy';

  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="center" justify="between">
          <Stack direction="horizontal" align="center" gap="snug">
            <Icon name="dollar-sign" size="2xs" />
            <Text size="caption" weight="emphasis">
              Tenant Cap
            </Text>
          </Stack>
          {canWrite && (
            <Button variant="ghost" size="sm" onClick={() => setEditing(true)}>
              {cap ? 'Edit' : 'Set cap'}
            </Button>
          )}
        </Stack>
      </CardHeader>
      <CardContent>
        {cap ? (
          <Stack gap="snug">
            {cap.reached && (
              <Alert variant="error">
                Tenant cap reached — agent and service inference is blocked until the next period or
                a raised cap. Humans continue under their individual budgets.
              </Alert>
            )}
            <Stack direction="horizontal" align="center" justify="between">
              <Text size="supporting" color="muted">
                All principals&apos; spend, {cap.reset_cadence} window
              </Text>
              <Text size="supporting" color="foreground" mono>
                {fmtUsd(cap.spend_usd)} / {fmtUsd(cap.limit_usd)}
                {fmtWindowSuffix(cap.reset_cadence)}
              </Text>
            </Stack>
            <Progress
              value={cap.spend_usd}
              max={cap.limit_usd}
              label="Tenant cap utilisation"
              variant={variant}
              showValue
            />
            <Text size="caption" color="subtle">
              Blocks agents and services at the limit; humans accumulate spend but are never blocked
              by the cap.
            </Text>
          </Stack>
        ) : (
          <Text size="caption" color="muted">
            No tenant cap configured. All spend in the tenant rolls up against the cap; agents and
            services are blocked when it is reached.
          </Text>
        )}
      </CardContent>

      {editing && (
        <TenantCapModal
          cap={cap}
          onClose={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            router.refresh();
          }}
        />
      )}
    </Card>
  );
}

function TenantCapModal({
  cap,
  onClose,
  onSaved,
}: {
  cap: TenantCapStatus | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [limitUSD, setLimitUSD] = useState(cap ? String(cap.limit_usd) : '');
  const [cadence, setCadence] = useState(cap?.reset_cadence ?? 'monthly');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  const parsed = parseFloat(limitUSD);
  const valid = !Number.isNaN(parsed) && parsed > 0;

  const save = useCallback(async () => {
    setSubmitting(true);
    setSubmitError(null);
    const result = await upsertTenantBudgetAction(parsed, cadence);
    if (result.ok) {
      onSaved();
    } else {
      setSubmitError(result.message);
    }
    setSubmitting(false);
  }, [parsed, cadence, onSaved]);

  const remove = useCallback(async () => {
    setSubmitting(true);
    setSubmitError(null);
    const result = await deleteTenantBudgetAction();
    if (result.ok) {
      onSaved();
    } else {
      setSubmitError(result.message);
    }
    setSubmitting(false);
  }, [onSaved]);

  return (
    <Modal open onClose={onClose} size="md">
      <ModalHeader title={cap ? 'Edit Tenant Cap' : 'Set Tenant Cap'} />
      <ModalBody flushY>
        <Stack gap="comfortable">
          <Stack gap="tight">
            <Text size="caption" weight="emphasis">
              Limit
            </Text>
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption" color="muted">
                $
              </Text>
              <Input
                type="number"
                placeholder="e.g. 2000"
                value={limitUSD}
                onChange={(e) => setLimitUSD(e.target.value)}
              />
              <Stack minWidth="form-field-sm">
                <NativeSelect
                  value={cadence}
                  onChange={(e) => setCadence(e.target.value)}
                  aria-label="Reset window"
                >
                  <option value="monthly">per month</option>
                  <option value="weekly">per week</option>
                </NativeSelect>
              </Stack>
            </Stack>
          </Stack>
          {submitError && (
            <Text color="status-error" size="caption">
              {submitError}
            </Text>
          )}
        </Stack>
      </ModalBody>
      <ModalFooter>
        {cap && (
          <Button variant="outline" size="sm" onClick={() => void remove()} disabled={submitting}>
            Remove Cap
          </Button>
        )}
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button size="sm" onClick={() => void save()} disabled={submitting || !valid}>
          {submitting ? 'Saving...' : 'Save Cap'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
