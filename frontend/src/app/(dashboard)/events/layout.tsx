import { ScopeGuard } from '../_components';

export default function EventsLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="events:read">{children}</ScopeGuard>;
}
