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
} from '@runkora/hades';
import type { DataTableColumn } from '@runkora/hades';
import { useHasScope, useTenantLocked } from '../../../_components/permissions-provider';
import { isDeclared, managedInGit } from '@runkora/lib/declarative';
import { fmtUsd, fmtWindowSuffix } from '@runkora/utils';
import { updateRoleBudgetAction } from '../../../policy/_actions/roles';
import type { RoleEntry } from '../../../policy/_types/api';
import { BudgetEditModal } from './budget-edit-modal';
import { ManagedInGitBadge, PermissionHint } from '@runkora/components';

interface BudgetRolesTabProps {
  roles: RoleEntry[];
}

export function BudgetRolesTab({ roles }: BudgetRolesTabProps) {
  const router = useRouter();
  const canWrite = useHasScope('platform:budgets:write');
  // Role budgets of a declared role, or of any role in a locked tenant, change in the configuration repository.
  const locked = useTenantLocked();
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
            {r.reserved && (
              <Badge variant="secondary" size="sm">
                not budgetable
              </Badge>
            )}
            {isDeclared(r) && <ManagedInGitBadge />}
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
          // Reserved roles cannot carry budgets: the bundle compiler skips them.
          if (r.reserved || managedInGit(isDeclared(r), locked)) return null;
          return (
            <PermissionHint allowed={canWrite} action="change role budgets">
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite}
                onClick={(e) => {
                  e.stopPropagation();
                  setEditing(r);
                }}
                aria-label={`Edit budget for ${r.role}`}
              >
                <Icon name="edit" size="2xs" />
              </Button>
            </PermissionHint>
          );
        },
      },
    ],
    [canWrite, locked],
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
            const result = await updateRoleBudgetAction(editing.role, budget);
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
