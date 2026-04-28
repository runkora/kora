/**
 * @tier 3 — model-gateway scoped. Renders a horizontal legend strip for
 * Recharts-based usage charts.
 * @promote-to-tier-2 if: a second feature area outside model-gateway needs
 * chart legends with status dots.
 */
import { Stack, StatusDot, Text } from '@orbit/terra';

type StatusDotStatus = 'healthy' | 'degraded' | 'error' | 'idle' | 'pending' | 'muted' | 'accent';

export interface ChartLegendItem {
  color: StatusDotStatus;
  label: string;
}

export interface ChartLegendProps {
  items: ChartLegendItem[];
}

export function ChartLegend({ items }: ChartLegendProps) {
  return (
    <Stack direction="horizontal" gap="comfortable" justify="center">
      {items.map((item) => (
        <Stack key={item.label} direction="horizontal" align="center" gap="tight">
          <StatusDot status={item.color} size="sm" aria-hidden />
          <Text size="caption" color="subtle">
            {item.label}
          </Text>
        </Stack>
      ))}
    </Stack>
  );
}
