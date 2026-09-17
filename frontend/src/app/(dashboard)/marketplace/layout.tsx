import { Stack } from '@orbit/terra';

export default function MarketplaceLayout({ children }: { children: React.ReactNode }) {
  return (
    <Stack asChild padding="comfortable">
      <main>{children}</main>
    </Stack>
  );
}
