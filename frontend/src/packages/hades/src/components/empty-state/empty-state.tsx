/** @tier 1 — Zero kora domain knowledge. Could be extracted to a standalone library. */
import { Card, CardContent } from '../card';
import { Icon } from '../icon';
import type { IconName } from '../icon';
import { Stack } from '../layout';
import { Heading, Text } from '../typography';

export interface EmptyStateProps {
  icon: IconName;
  title: string;
  description: string;
  action?: React.ReactNode;
}

/** Placeholder for a list or page with nothing in it yet: icon, title, description, optional action. */
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
          {action}
        </Stack>
      </CardContent>
    </Card>
  );
}
