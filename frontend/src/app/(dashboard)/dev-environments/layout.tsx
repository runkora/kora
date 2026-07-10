import { Stack } from '@orbit/terra';

export default function DevEnvironmentsLayout({ children }: { children: React.ReactNode }) {
  return (
    <Stack asChild padding="comfortable">
      <main>{children}</main>
    </Stack>
  );
}
