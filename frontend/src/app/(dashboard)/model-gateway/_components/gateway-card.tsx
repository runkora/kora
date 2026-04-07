import { Card, CardContent, CardDescription, CardHeader, CardTitle, Stack } from '@orbit/terra';

// ── GatewayCard ───────────────────────────────────────────────────────────────
//
// Shared card shell used across all model-gateway tabs. Provides a consistent
// header (title + optional description left, optional icon box top-right) and
// a content area populated by the consumer.

export interface GatewayCardProps {
  title: string;
  description?: string;
  icon?: React.ReactNode;
  children: React.ReactNode;
}

export function GatewayCard({ title, description, icon, children }: GatewayCardProps) {
  return (
    <Card column>
      <CardHeader compact>
        <Stack direction="horizontal" align="start" justify="between" gap="comfortable">
          <Stack gap="none" style={{ marginTop: '-6px' }}>
            <CardTitle>{title}</CardTitle>
            {description && <CardDescription>{description}</CardDescription>}
          </Stack>
          {icon && (
            <span className="-mt-3 -mr-3 flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-elevated border border-border-subtle text-muted-foreground">
              {icon}
            </span>
          )}
        </Stack>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}
