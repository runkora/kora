import { ScopeGuard } from '../_components';

/**
 * Policy section layout — wraps all /policy routes with a scope guard.
 * Users without `platform:grants:read` see a forbidden message instead
 * of the page content (the sidebar also hides the entry, but this
 * handles direct URL navigation).
 */
export default function PolicyLayout({ children }: { children: React.ReactNode }) {
  return (
    <ScopeGuard scope="platform:grants:read">
      <main className="px-6 py-8">{children}</main>
    </ScopeGuard>
  );
}
