/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import {
  CopyField,
  Badge,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Progress,
  Separator,
  Stack,
  Text,
} from '@runkora/hades';
import { StatusBadge } from '@orbit/components';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import type { BudgetCategoryStatus, BudgetPrincipalRow } from '../../_types';
import { principalDisplay } from './principal-display';
import { principalStatus } from './budget-principal-format';

// ── SectionLabel ────────────────────────────────────────────────────────────

function SectionLabel({ children }: { children: string }) {
  return (
    <Stack paddingTop="none" paddingInline="snug" paddingBottom="tight">
      <Text size="caption" color="muted">
        {children}
      </Text>
    </Stack>
  );
}

// ── StatRow ──────────────────────────────────────────────────────────────────

function StatRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <Stack
      direction="horizontal"
      align="center"
      justify="between"
      paddingBlock="tight"
      paddingInline="cozy"
    >
      <Text size="caption" color="muted">
        {label}
      </Text>
      {typeof value === 'string' ? (
        <Text size="supporting" mono>
          {value}
        </Text>
      ) : (
        value
      )}
    </Stack>
  );
}

// ── SourceRow — one budget source (role or grant), full untruncated id ──────

function SourceRow({ cat }: { cat: BudgetCategoryStatus }) {
  return (
    <Stack gap="none">
      <Stack paddingInline="cozy" paddingTop="tight" paddingBottom="none">
        <Text size="caption" color="muted">
          {cat.source === 'grant' ? 'Grant ID' : 'Role'}
        </Text>
      </Stack>
      {/* Indented one step past the label above, as its child. */}
      <Stack gap="none" style={{ paddingInlineStart: '1rem' }}>
        <CopyField value={cat.id} mono wrap />
        <Stack
          direction="horizontal"
          wrap
          align="center"
          gap="tight"
          paddingInline="cozy"
          // 2px + CopyField's own 8px bottom padding matches the 10px gap above the id.
          style={{ paddingBlockStart: '2px', paddingBlockEnd: '4px' }}
        >
          {cat.limit_usd > 0 && (
            <Text size="caption" color="muted" mono>
              {fmtUsd(cat.spend_usd)} / {fmtUsd(cat.limit_usd)}
              {fmtWindowSuffix(cat.reset_cadence)}
            </Text>
          )}
          {cat.per_run_limit_usd ? (
            <Badge variant="outline" size="sm">
              {fmtUsd(cat.per_run_limit_usd)}/run
            </Badge>
          ) : null}
          {cat.pooled && (
            <Badge variant="outline" size="sm">
              pool
            </Badge>
          )}
        </Stack>
      </Stack>
    </Stack>
  );
}

// ── BudgetPrincipalDetailModal ────────────────────────────────────────────────

interface BudgetPrincipalDetailModalProps {
  principal: BudgetPrincipalRow | null;
  onClose: () => void;
}

export function BudgetPrincipalDetailModal({
  principal,
  onClose,
}: BudgetPrincipalDetailModalProps) {
  const display = principal
    ? principalDisplay(principal.principal_id, principal.principal_type, principal)
    : null;
  const status = principal ? principalStatus(principal) : null;

  return (
    <Modal open={!!principal} onClose={onClose} size="lg">
      {principal && display && status && (
        <>
          <ModalHeader title={display.primary} description={display.secondary ?? undefined} />

          {/* Status strip — lives between header and body intentionally */}
          <Stack
            direction="horizontal"
            align="center"
            gap="snug"
            paddingBlock="snug"
            paddingInline="comfortable"
          >
            <StatusBadge status={status.variant} label={status.label} />
            {principal.principal_type && (
              <Badge variant="secondary" size="sm">
                {principal.principal_type.toLowerCase()}
              </Badge>
            )}
          </Stack>
          <Separator />

          <ModalBody flush gapless constrained>
            {/* Identity */}
            <Stack padding="snug">
              <SectionLabel>Identity</SectionLabel>
              <CopyField label="Principal ID" value={principal.principal_id} mono wrap />
              {principal.email && <CopyField label="Email" value={principal.email} wrap />}
              {principal.display_name && (
                <CopyField label="Display Name" value={principal.display_name} wrap />
              )}
            </Stack>

            <Separator />

            {/* Budget */}
            <Stack padding="snug" gap="none">
              <SectionLabel>Budget</SectionLabel>
              {principal.has_limit ? (
                <>
                  <StatRow
                    label="Effective Limit"
                    value={`${fmtUsd(principal.effective_limit_usd)}${fmtWindowSuffix(principal.effective_reset_cadence ?? 'monthly')}`}
                  />
                  <StatRow label="Effective Spend" value={fmtUsd(principal.effective_spend_usd)} />
                  <StatRow
                    label="Utilisation"
                    value={
                      <Stack style={{ minWidth: '140px' }}>
                        <Progress
                          value={principal.effective_spend_usd}
                          max={principal.effective_limit_usd}
                          label={`Budget utilisation for ${principal.principal_id}`}
                          variant={status.variant === 'idle' ? 'healthy' : status.variant}
                          showValue
                        />
                      </Stack>
                    }
                  />
                </>
              ) : (
                <StatRow label="Effective Limit" value="No budget" />
              )}
              <StatRow label="Spend (month-to-date)" value={fmtUsd(principal.current_spend_usd)} />
              <StatRow
                label="Last Active"
                value={principal.last_active_at ? fmtDate(principal.last_active_at) : '—'}
              />
            </Stack>

            <Separator />

            {/* Budget Sources */}
            <Stack padding="snug" gap="none">
              <SectionLabel>Budget Sources</SectionLabel>
              {principal.categories.length > 0 ? (
                principal.categories.map((cat) => (
                  <SourceRow key={`${cat.source}:${cat.id}`} cat={cat} />
                ))
              ) : (
                <Stack paddingBlock="tight" paddingInline="cozy">
                  <Text size="supporting" color="subtle">
                    No budget sources.
                  </Text>
                </Stack>
              )}
            </Stack>
          </ModalBody>

          <ModalFooter justify="start">
            <Text size="caption" color="subtle">
              Click any ID row to copy to clipboard.
            </Text>
          </ModalFooter>
        </>
      )}
    </Modal>
  );
}
