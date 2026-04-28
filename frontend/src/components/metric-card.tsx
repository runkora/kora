/**
 * @tier 2 — App-level shared. Cross-feature; unifies StatCard (dashboard),
 * KPICard (model-gateway overview), and KPICard (policy overview) into one canonical
 * metric display component. Used by dashboard home, model-gateway overview, policy overview,
 * and all future feature overview pages.
 * @promote-to-tier-1 if: Orbit heading variants and delta colour semantics are removed
 * and a second non-dashboard surface needs a metric display.
 */
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
  Heading,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import Link from 'next/link';

export interface MetricCardProps {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  /** Delta indicator row. `positive` determines colour; `positiveIsBad` inverts the colour logic. */
  delta?: {
    text: string;
    positive: boolean;
    positiveIsBad?: boolean;
  };
  /** Optional breakdown table rendered below the metric value. */
  breakdown?: Array<{ label: string; value: string | number }>;
  /** When provided, renders a "View all →" link in the card footer. */
  href?: string;
}

export function MetricCard({ icon, label, value, delta, breakdown, href }: MetricCardProps) {
  const deltaColor =
    delta == null
      ? undefined
      : delta.positiveIsBad
        ? delta.positive
          ? 'status-degraded'
          : 'status-healthy'
        : delta.positive
          ? 'status-healthy'
          : 'status-degraded';

  return (
    <Card column>
      <CardHeader compact icon={icon}>
        <CardTitle>{label}</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="snug">
          <Heading variant="metric">{value}</Heading>
          {delta && (
            <Text size="caption" color={deltaColor}>
              {delta.text}
            </Text>
          )}
          {breakdown && breakdown.length > 0 && (
            <>
              <Separator />
              <Stack gap="tight">
                {breakdown.map((item) => (
                  <Stack key={item.label} direction="horizontal" align="center" justify="between">
                    <Text size="caption" color="muted">
                      {item.label}
                    </Text>
                    <Text size="caption" weight="emphasis">
                      {item.value}
                    </Text>
                  </Stack>
                ))}
              </Stack>
            </>
          )}
        </Stack>
      </CardContent>
      {href && (
        <CardFooter compact justify="end">
          <Link href={href} className="text-xs text-subtle hover:text-foreground transition-colors">
            View all →
          </Link>
        </CardFooter>
      )}
    </Card>
  );
}
