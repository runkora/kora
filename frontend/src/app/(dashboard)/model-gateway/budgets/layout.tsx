import { ScopeGuard } from '../../_components';

export default function BudgetsLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="platform:budgets:read">{children}</ScopeGuard>;
}
