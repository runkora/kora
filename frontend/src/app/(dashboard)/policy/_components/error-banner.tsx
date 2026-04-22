import { Icon, Stack, Text } from '@orbit/terra';

interface ErrorBannerProps {
  message: string;
}

export function ErrorBanner({ message }: ErrorBannerProps) {
  return (
    <section className="rounded-lg border border-status-error/30 bg-status-error/5 px-4 py-3">
      <Stack direction="horizontal" align="center" gap="snug">
        <Icon name="triangle-alert" color="status-error" />
        <Text size="caption" color="status-error">
          {message}
        </Text>
      </Stack>
    </section>
  );
}
