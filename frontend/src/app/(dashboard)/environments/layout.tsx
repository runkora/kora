import { ScopeGuard } from '../_components';

export default function EnvironmentsLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="agents:environments:read">{children}</ScopeGuard>;
}
