/** @tier 3 — budgets-page-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import {
  Alert,
  EmptyState,
  Icon,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@runkora/hades';
import type { Grant, RoleEntry } from '../../../policy/_types/api';
import type { BudgetPrincipalsView, PrincipalIdentity } from '../../_types';
import { BudgetPrincipalsTab } from './budget-principals-tab';
import { BudgetRolesTab } from './budget-roles-tab';
import { BudgetGrantsTab } from './budget-grants-tab';

interface BudgetsShellProps {
  principals: BudgetPrincipalsView | null;
  roles: RoleEntry[];
  grants: Grant[];
  identities: Record<string, PrincipalIdentity>;
  initialError: string | null;
  /** Why the roles or grants could not be read; shown in that tab instead of an empty table. */
  rolesError?: string | null;
  grantsError?: string | null;
}

export function BudgetsShell({
  principals,
  roles,
  grants,
  identities,
  initialError,
  rolesError = null,
  grantsError = null,
}: BudgetsShellProps) {
  return (
    <Stack gap="comfortable">
      {initialError && (
        <Alert variant="error">
          <Icon name="triangle-alert" color="status-error" />
          <Text size="caption" color="status-error">
            {initialError}
          </Text>
        </Alert>
      )}

      <Tabs defaultValue="principals">
        <TabsList>
          <TabsTrigger value="principals">Principals</TabsTrigger>
          <TabsTrigger value="roles">Role Budgets</TabsTrigger>
          <TabsTrigger value="grants">Grant Budgets</TabsTrigger>
        </TabsList>

        <TabsContent value="principals">
          <BudgetPrincipalsTab principals={principals} />
        </TabsContent>

        <TabsContent value="roles">
          {rolesError ? (
            <EmptyState icon="lock" title="Role budgets unavailable" description={rolesError} />
          ) : (
            <BudgetRolesTab roles={roles} />
          )}
        </TabsContent>

        <TabsContent value="grants">
          {grantsError ? (
            <EmptyState icon="lock" title="Grant budgets unavailable" description={grantsError} />
          ) : (
            <BudgetGrantsTab grants={grants} identities={identities} />
          )}
        </TabsContent>
      </Tabs>
    </Stack>
  );
}
