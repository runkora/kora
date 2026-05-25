import { Stack } from '@orbit/terra';

export default function AgentsLayout({ children }: { children: React.ReactNode }) {
  return (
    <Stack asChild padding="comfortable">
      <main>{children}</main>
    </Stack>
  );
}
