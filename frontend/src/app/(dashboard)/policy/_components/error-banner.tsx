import { Stack, Text } from '@orbit/terra';
import { TriangleAlert } from 'lucide-react';

interface ErrorBannerProps {
  message: string;
}

export function ErrorBanner({ message }: ErrorBannerProps) {
  return (
    <section className="rounded-lg border border-status-error/30 bg-status-error/5 px-4 py-3">
      <Stack direction="horizontal" align="center" gap="snug">
        <TriangleAlert className="size-4 text-status-error shrink-0" />
        <Text size="caption" color="status-error">
          {message}
        </Text>
      </Stack>
    </section>
  );
}
