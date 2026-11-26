import { Stack } from '@runkora/hades';
import { PermissionsProvider } from '@runkora/app/(dashboard)/_components/permissions-provider';

/** The chat page and the run page fill the viewport. The session supplies the viewer's name and scopes. */
export default function ChatLayout({ children }: { children: React.ReactNode }) {
  return (
    <PermissionsProvider>
      <Stack height="screen" overflow="hidden" style={{ background: 'var(--color-background)' }}>
        {children}
      </Stack>
    </PermissionsProvider>
  );
}
