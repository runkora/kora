import { ScopeGuard } from '../../_components';

export default function PolicyOverviewLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="platform:grants:read">{children}</ScopeGuard>;
}
