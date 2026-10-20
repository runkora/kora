import { ScopeGuard } from '../_components';

export default function TriggersLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="triggers:read">{children}</ScopeGuard>;
}
