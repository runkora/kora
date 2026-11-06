/** @tier 3 — triggers-scoped. Why the next page of a delivery list failed, under rows already shown. */

import { Text } from '@runkora/hades';

interface NextPageErrorProps {
  /** Why the last page failed, if it did. */
  error: string | null;
  /** Rows on screen. With none, the list shows the error in place of the table. */
  rows: number;
}

export function NextPageError({ error, rows }: NextPageErrorProps) {
  if (error === null || rows === 0) return null;
  return (
    <Text size="caption" color="status-error">
      Could not load more deliveries: {error}
    </Text>
  );
}
