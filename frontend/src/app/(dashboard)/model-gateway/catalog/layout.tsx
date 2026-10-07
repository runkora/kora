import { ScopeGuard } from '../../_components';

export default function CatalogLayout({ children }: { children: React.ReactNode }) {
  return <ScopeGuard scope="platform:catalog:write">{children}</ScopeGuard>;
}
