import { ScopeGuard } from '../../_components';

export default function CatalogLayout({ children }: { children: React.ReactNode }) {
  return (
    <ScopeGuard scope="platform:catalog:write">
      <main className="px-6 py-8">{children}</main>
    </ScopeGuard>
  );
}
