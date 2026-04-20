import { Card, CardContent, Heading, Stack, Text } from '@orbit/terra';

interface EmptyStateProps {
  icon: React.ReactNode;
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
            {/* eslint-disable-next-line local/no-raw-div */}
            <div className="flex size-10 items-center justify-center rounded-full bg-surface-elevated border border-border-subtle text-subtle">
              {icon}
            </div>
            <Heading variant="section">{title}</Heading>
            <Text size="caption" color="muted" align="center">
              {description}
            </Text>
            {action && (
              <>
                {/* eslint-disable-next-line local/no-raw-div */}
                <div className="mt-2">{action}</div>
              </>
            )}
          </section>
        </Stack>
      </CardContent>
    </Card>
  );
}
