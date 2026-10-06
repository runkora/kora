import { ScopeGuard } from '../../_components';

export default function ModelsLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="inference:list">{children}</ScopeGuard>;
}
