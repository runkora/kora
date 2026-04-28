/**
 * @tier 2 — App-level shared. Cross-feature; used by model-gateway, policy, and all future
 * dashboard sections that need a zero-state placeholder.
 * @promote-to-tier-1 if: domain knowledge is removed and a second non-dashboard Orbit surface needs it.
 */
import { Card, CardContent, Heading, Icon, Stack, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';

export interface EmptyStateProps {
  icon: IconName;
  title: string;
  description: string;
  action?: React.ReactNode;
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <Card column>
      <CardContent>
        <Stack gap="snug" align="center" paddingBlock="comfortable">
          <Icon name={icon} size="md" container containerVariant="surface-subtle" circle />
          <Heading variant="section">{title}</Heading>
          <Text size="caption" color="muted" align="center">
            {description}
          </Text>
          {action && action}
        </Stack>
      </CardContent>
    </Card>
  );
}
