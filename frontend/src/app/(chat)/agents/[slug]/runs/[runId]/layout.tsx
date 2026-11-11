import { Stack } from '@runkora/hades';
import { PermissionsProvider } from '@runkora/app/(dashboard)/_components/permissions-provider';

/** The run page fills the viewport. The session supplies the viewer's name and scopes. */
export default function RunLayout({ children }: { children: React.ReactNode }) {
  return (
    <PermissionsProvider>
      <Stack
        style={{ height: '100dvh', overflow: 'hidden', background: 'var(--color-background)' }}
      >
        {children}
      </Stack>
    </PermissionsProvider>
  );
}
