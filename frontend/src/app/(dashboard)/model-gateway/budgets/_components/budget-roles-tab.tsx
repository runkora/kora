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
} from '@orbit/hades';
import type { DataTableColumn } from '@orbit/hades';
import { usePermissions } from '../../../_components/permissions-provider';
import { hasScope } from '@orbit/lib/permissions';
import { fmtUsd, fmtWindowSuffix } from '@orbit/utils';
import { upsertRoleAction } from '../../../policy/_actions/roles';
import type { RoleEntry } from '../../../policy/_types/api';
import { BudgetEditModal } from './budget-edit-modal';

// Reserved roles cannot carry budgets — the bundle compiler skips them.
const RESERVED_ROLES = new Set(['platform-admin']);

interface BudgetRolesTabProps {
  roles: RoleEntry[];
}

export function BudgetRolesTab({ roles }: BudgetRolesTabProps) {
  const router = useRouter();
  const { scopes: sessionScopes } = usePermissions();
  const canWrite = hasScope(sessionScopes, 'platform:budgets:write');
  const [editing, setEditing] = useState<RoleEntry | null>(null);

  const columns = useMemo<DataTableColumn<RoleEntry>[]>(
    () => [
      {
        id: 'role',
        header: 'Role',
        gridWidth: 'minmax(0, 2fr)',
        minWidth: '12rem',
        locked: true,
        sortable: true,
        sortAccessor: (r) => r.role,
        cell: (r) => (
          <Stack direction="horizontal" align="center" gap="tight">
            <Text size="caption" mono weight="emphasis">
              {r.role}
            </Text>
            {RESERVED_ROLES.has(r.role) && (
              <Badge variant="secondary" size="sm">
                not budgetable
              </Badge>
            )}
          </Stack>
        ),
      },
      {
        id: 'budget',
        header: 'Budget',
        gridWidth: '10rem',
        minWidth: '7rem',
        sortable: true,
        sortAccessor: (r) => r.budget?.limit_usd ?? 0,
        cell: (r) =>
          r.budget ? (
            <Stack direction="horizontal" align="center" gap="tight">
              <Text size="caption" mono>
                {fmtUsd(r.budget.limit_usd ?? 0)}
                {fmtWindowSuffix(r.budget.reset_cadence)}
              </Text>
              {r.budget.pooled && (
                <Badge variant="elevated" size="sm">
                  pool
                </Badge>
              )}
            </Stack>
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
        cell: (r) => (
          <Text size="caption" color="subtle">
            {(r.scopes ?? []).length}
          </Text>
        ),
      },
      {
        id: 'actions',
        header: '',
        pinEnd: true,
        locked: true,
        gridWidth: '4rem',
        cell: (r) => {
          if (!canWrite || RESERVED_ROLES.has(r.role)) return null;
          return (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={(e) => {
                e.stopPropagation();
                setEditing(r);
              }}
              aria-label={`Edit budget for ${r.role}`}
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
        title="Role Budgets"
        description="Weekly or monthly limits carried by OIDC roles: per-member allowances or shared pools."
      />

      <Callout>
        <Text size="caption" color="muted">
          A role budget applies to all principals the IDP asserts the role for at login. Editing
          here preserves the role&apos;s scope mappings. Manage scopes on the Policy → Roles page.
        </Text>
      </Callout>

      <DataTable
        data={roles}
        columns={columns}
        getRowKey={(r) => r.id}
        noun="roles"
        searchPlaceholder="Search roles…"
        searchFn={(r, q) => r.role.toLowerCase().includes(q)}
        defaultSortId="budget"
        defaultSortDir="desc"
        emptyMessage="No roles configured. Create roles on the Policy → Roles page."
        filteredEmptyMessage="No roles match your search."
      />

      {editing && (
        <BudgetEditModal
          key={editing.id}
          open
          kind="role"
          subject={editing.role}
          initialBudget={editing.budget}
          onClose={() => setEditing(null)}
          onSave={async (budget) => {
            // Budget-only edit: pass the existing scopes back unchanged.
            const result = await upsertRoleAction({
              role: editing.role,
              scopes: editing.scopes ?? [],
              budget,
            });
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
