import { ScopeGuard } from '../../_components';

export default function AnalyticsLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="platform:analytics:read">{children}</ScopeGuard>;
}
