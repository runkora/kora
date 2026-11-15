import { Stack } from '@runkora/hades';
import { PermissionsProvider } from '@runkora/app/(dashboard)/_components/permissions-provider';

/** The run page fills the viewport. The session supplies the viewer's name and scopes. */
export default function RunRouteLayout({ children }: { children: React.ReactNode }) {
  return (
    <PermissionsProvider>
      <Stack height="screen" overflow="hidden" style={{ background: 'var(--color-background)' }}>
        {children}
      </Stack>
    </PermissionsProvider>
  );
}
