import { Card, CardContent, Heading, Icon, Stack, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';

interface EmptyStateProps {
  icon: IconName;
  title: string;
  description: string;
  action?: React.ReactNode;
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <Card column>
      <CardContent>
        <Stack gap="snug" align="center" asChild>
          <section className="py-8">
            <Icon name={icon} size="md" container containerVariant="surface-subtle" circle />
            <Heading variant="section">{title}</Heading>
            <Text size="caption" color="muted" align="center">
              {description}
            </Text>
            {action && action}
          </section>
        </Stack>
      </CardContent>
    </Card>
  );
}
