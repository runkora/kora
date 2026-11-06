/** @tier 3 — triggers-scoped. Why the next page of a delivery list failed, under rows already shown. */

import { Stack, Text } from '@runkora/hades';

interface NextPageErrorProps {
  /** Why the last page failed, if it did. */
  error: string | null;
  /** Rows are on screen. With none, the list shows the error in place of the table. */
  hasRows: boolean;
}

export function NextPageError({ error, hasRows }: NextPageErrorProps) {
  if (error === null || !hasRows) return null;
  return (
    <Stack role="alert">
      <Text size="caption" color="status-error">
        Could not load more deliveries: {error}
      </Text>
    </Stack>
  );
}
