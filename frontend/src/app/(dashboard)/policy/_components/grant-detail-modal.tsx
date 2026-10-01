/**
 * @tier 3 — policy-scoped. Everything about one grant, reached by clicking its
 * row. The table clamps scopes to two rows and truncates the principal ID, so
 * this is where the whole grant is readable and the ID can be copied.
 * @promote-to-tier-2 if: another feature needs to show a grant.
 */
'use client';

import { Modal, ModalBody, ModalHeader, Separator, Stack, Text } from '@orbit/terra';
import { CopyButton, ScopeChip } from '@orbit/components';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import type { BudgetAssignment, Grant } from '../_types';
import type { GrantIdentity } from '../_utils/grant-identity';
import { PrincipalBadge } from './principal-badge';

function budgetLines(budget: BudgetAssignment | null | undefined): string[] {
  if (!budget) return [];
  const lines: string[] = [];
  if (budget.limit_usd) {
    lines.push(`${fmtUsd(budget.limit_usd)}${fmtWindowSuffix(budget.reset_cadence)}`);
  }
  if (budget.per_run_limit_usd) lines.push(`${fmtUsd(budget.per_run_limit_usd)} per run`);
  return lines;
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Stack padding="snug" gap="tight">
      <Stack paddingInline="snug">
        <Text size="caption" color="muted">
          {label}
        </Text>
      </Stack>
      <Stack paddingInline="snug" gap="tight">
        {children}
      </Stack>
    </Stack>
  );
}

interface GrantDetailModalProps {
  grant: Grant | null;
  identity: GrantIdentity | null;
  onClose: () => void;
}

export function GrantDetailModal({ grant, identity, onClose }: GrantDetailModalProps) {
  const budget = grant ? budgetLines(grant.budget) : [];
  return (
    <Modal open={!!grant} onClose={onClose} size="lg">
      {grant && identity && (
        <>
          <ModalHeader
            title={identity.primary}
            titleAccessory={<PrincipalBadge type={grant.principal_type} />}
            description={
              identity.email ?? (identity.ownerName ? `Owned by ${identity.ownerName}` : undefined)
            }
          />
          <Separator />

          <ModalBody flush gapless constrained>
            <Section label="Principal ID">
              <Stack direction="horizontal" align="start" gap="snug" minWidth="none">
                <Stack grow minWidth="none">
                  <Text size="caption" color="subtle" mono style={{ overflowWrap: 'anywhere' }}>
                    {grant.principal_id}
                  </Text>
                </Stack>
                <CopyButton value={grant.principal_id} label="Copy principal ID" />
              </Stack>
            </Section>

            <Separator />

            <Section label="Budget">
              {budget.length > 0 ? (
                budget.map((line) => (
                  <Text key={line} size="supporting">
                    {line}
                  </Text>
                ))
              ) : (
                <Text size="supporting" color="subtle">
                  No budget.
                </Text>
              )}
            </Section>

            <Separator />

            <Section label="Created">
              <Text size="supporting">{fmtDate(grant.created_at)}</Text>
            </Section>

            <Separator />

            <Section label={`Scopes (${grant.scopes.length})`}>
              {grant.scopes.length > 0 ? (
                <Stack direction="horizontal" gap="tight" wrap>
                  {grant.scopes.map((scope) => (
                    <ScopeChip key={scope} scope={scope} />
                  ))}
                </Stack>
              ) : (
                <Text size="supporting" color="subtle">
                  No scopes.
                </Text>
              )}
            </Section>
          </ModalBody>
        </>
      )}
    </Modal>
  );
}
