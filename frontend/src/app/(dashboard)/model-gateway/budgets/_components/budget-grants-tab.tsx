/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button, Callout, DataTable, Icon, PageSectionHeader, Stack, Text } from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { PrincipalBadge } from '@runkora/components';
import { useHasScope } from '../../../_components/permissions-provider';
import { fmtDate, fmtUsd, fmtWindowSuffix } from '@runkora/utils';
import { updateGrantBudgetAction } from '../../../policy/_actions/grants';
import type { Grant } from '../../../policy/_types/api';
import type { PrincipalIdentity } from '../../_types';
import { BudgetEditModal } from './budget-edit-modal';
import { principalDisplay } from './principal-display';

interface BudgetGrantsTabProps {
  grants: Grant[];
  identities: Record<string, PrincipalIdentity>;
}

export function BudgetGrantsTab({ grants, identities }: BudgetGrantsTabProps) {
  const router = useRouter();
  const canWrite = useHasScope('platform:budgets:write');
  const [editing, setEditing] = useState<Grant | null>(null);

  const columns = useMemo<DataTableColumn<Grant>[]>(
    () => [
      {
        id: 'principal',
        header: 'Principal',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '11rem',
        locked: true,
        sortable: true,
        sortAccessor: (g) =>
          principalDisplay(g.principal_id, g.principal_type, identities[g.principal_id]).primary,
        cell: (g) => {
          const display = principalDisplay(
            g.principal_id,
            g.principal_type,
            identities[g.principal_id],
          );
          return (
            <Stack gap="tight">
              <Text size="caption" weight="emphasis">
                {display.primary}
              </Text>
              {display.secondary && (
                <Text size="caption" color="subtle" mono>
                  {display.secondary}
                </Text>
              )}
            </Stack>
          );
        },
      },
      {
        id: 'type',
        header: 'Type',
        gridWidth: '6rem',
        minWidth: '5.5rem',
        sortable: true,
        sortAccessor: (g) => g.principal_type,
        cell: (g) => <PrincipalBadge type={g.principal_type} />,
      },
      {
        id: 'budget',
        header: 'Budget',
        gridWidth: '9rem',
        minWidth: '7rem',
        sortable: true,
        sortAccessor: (g) => g.budget?.limit_usd ?? 0,
        cell: (g) =>
          g.budget ? (
            <Text size="caption" mono>
              {[
                g.budget.limit_usd
                  ? `${fmtUsd(g.budget.limit_usd)}${fmtWindowSuffix(g.budget.reset_cadence)}`
                  : null,
                g.budget.per_run_limit_usd ? `${fmtUsd(g.budget.per_run_limit_usd)}/run` : null,
              ]
                .filter(Boolean)
                .join(' · ')}
            </Text>
          ) : (
            <Text size="caption" color="subtle">
              —
            </Text>
          ),
      },
      {
        id: 'scopes',
        header: 'Scopes',
        gridWidth: '6rem',
        minWidth: '4.5rem',
        cell: (g) => (
          <Text size="caption" color="subtle">
            {(g.scopes ?? []).length}
          </Text>
        ),
      },
      {
        id: 'created',
        header: 'Created',
        gridWidth: '7.5rem',
        minWidth: '6.5rem',
        sortable: true,
        sortAccessor: (g) => g.created_at,
        cell: (g) => (
          <Text size="caption" color="subtle">
            {fmtDate(g.created_at)}
          </Text>
        ),
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '4rem',
        cell: (g) => {
          if (!canWrite) return null;
          return (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={(e) => {
                e.stopPropagation();
                setEditing(g);
              }}
              aria-label={`Edit budget for ${g.principal_id}`}
            >
              <Icon name="edit" size="2xs" />
            </Button>
          );
        },
      },
    ],
    [canWrite, identities],
  );

  return (
    <Stack gap="comfortable">
      <PageSectionHeader
        title="Grant Budgets"
        description="Window and per-run limits carried by individual grants: humans, agents, and services."
      />

      <Callout>
        <Text size="caption" color="muted">
          Editing a budget here keeps the grant (and its spend attribution) intact. Grant scopes are
          immutable. Manage grants on the Policy → Grants page.
        </Text>
      </Callout>

      <DataTable
        data={grants}
        columns={columns}
        getRowKey={(g) => g.id}
        noun="grants"
        searchPlaceholder="Search principals…"
        searchFn={(g, q) => {
          const display = principalDisplay(
            g.principal_id,
            g.principal_type,
            identities[g.principal_id],
          );
          return (
            g.principal_id.toLowerCase().includes(q) ||
            display.primary.toLowerCase().includes(q) ||
            g.principal_type.toLowerCase().includes(q)
          );
        }}
        defaultSortId="budget"
        defaultSortDir="desc"
        emptyMessage="No grants configured. Create grants on the Policy → Grants page."
        filteredEmptyMessage="No grants match your search."
      />

      {editing && (
        <BudgetEditModal
          key={editing.id}
          open
          kind="grant"
          subject={
            principalDisplay(
              editing.principal_id,
              editing.principal_type,
              identities[editing.principal_id],
            ).primary
          }
          initialBudget={editing.budget}
          onClose={() => setEditing(null)}
          onSave={async (budget) => {
            const result = await updateGrantBudgetAction(editing.id, budget);
            if (result.ok) {
              setEditing(null);
              router.refresh();
              return { ok: true };
            }
            return { ok: false, message: result.message };
          }}
        />
      )}
    </Stack>
  );
}
