/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Callout,
  DataTable,
  Icon,
  PageSectionHeader,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { usePermissions } from '../../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import { fmtDate, fmtUsd } from '@orbit/utils';
import { updateGrantBudgetAction } from '../../../policy/_actions/grants';
import type { Grant } from '../../../policy/_types/api';
import { BudgetEditModal } from './budget-edit-modal';

interface BudgetGrantsTabProps {
  grants: Grant[];
}

export function BudgetGrantsTab({ grants }: BudgetGrantsTabProps) {
  const router = useRouter();
  const { scopes: sessionScopes } = usePermissions();
  const canWrite = hasScope(sessionScopes, 'platform:budgets:write');
  const [editing, setEditing] = useState<Grant | null>(null);

  const columns = useMemo<DataTableColumn<Grant>[]>(
    () => [
      {
        id: 'principal',
        header: 'Principal',
        locked: true,
        sortable: true,
        sortAccessor: (g) => g.principal_id,
        cell: (g) => (
          <Stack direction="horizontal" align="center" gap="tight">
            <Text size="caption" mono weight="emphasis">
              {g.principal_id}
            </Text>
            <Badge variant="secondary" size="sm">
              {g.principal_type.toLowerCase()}
            </Badge>
          </Stack>
        ),
      },
      {
        id: 'budget',
        header: 'Monthly Budget',
        sortable: true,
        sortAccessor: (g) => g.budget?.monthly_limit_usd ?? 0,
        cell: (g) =>
          g.budget ? (
            <Text size="caption" mono>
              {fmtUsd(g.budget.monthly_limit_usd)}/mo
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
        cell: (g) => (
          <Text size="caption" color="subtle">
            {(g.scopes ?? []).length}
          </Text>
        ),
      },
      {
        id: 'created',
        header: 'Created',
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
        locked: true,
        width: '4rem',
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
    [canWrite],
  );

  return (
    <Stack gap="comfortable">
      <PageSectionHeader
        title="Grant Budgets"
        description="Monthly limits carried by individual grants — humans, agents, and services."
      />

      <Callout>
        <Text size="caption" color="muted">
          Editing a budget here keeps the grant (and its spend attribution) intact. Grant scopes are
          immutable — manage grants on the Policy → Grants page.
        </Text>
      </Callout>

      <DataTable
        data={grants}
        columns={columns}
        getRowKey={(g) => g.id}
        noun="grants"
        searchPlaceholder="Search principals…"
        searchFn={(g, q) =>
          g.principal_id.toLowerCase().includes(q) || g.principal_type.toLowerCase().includes(q)
        }
        defaultSortId="budget"
        defaultSortDir="desc"
        emptyMessage="No grants configured. Create grants on the Policy → Grants page."
        filteredEmptyMessage="No grants match your search."
      />

      {editing && (
        <BudgetEditModal
          key={editing.id}
          open
          subject={editing.principal_id}
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
