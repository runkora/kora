/** @tier 3 — triggers-scoped. The "Load older" control under a paged delivery list, with why the last page failed. */

import { Button, Icon, Stack, Text } from '@runkora/hades';

interface LoadOlderProps {
  loadingMore: boolean;
  /** Why the last page failed, if it did. */
  error: string | null;
  onLoadOlder?: () => void;
}

export function LoadOlder({ loadingMore, error, onLoadOlder }: LoadOlderProps) {
  return (
    <Stack direction="horizontal" align="center" gap="cozy">
      <Button size="xs" variant="secondary" disabled={loadingMore} onClick={onLoadOlder}>
        {loadingMore ? (
          <>
            <Icon name="loader" size="2xs" />
            Loading…
          </>
        ) : (
          'Load older'
        )}
      </Button>
      {error !== null && (
        <Text size="caption" color="status-error">
          Could not load older deliveries: {error}
        </Text>
      )}
    </Stack>
  );
}
