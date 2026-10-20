import { ScopeGuard } from '../../_components';

export default function PolicyRolesLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="platform:roles:read">{children}</ScopeGuard>;
}
