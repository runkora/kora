'use client';

import { Card, CardContent, CardHeader, CardTitle, Heading, Stack, Text } from '@orbit/terra';

interface StatCardProps {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  sub?: string;
}

export function StatCard({ icon, label, value, sub }: StatCardProps) {
  return (
    <Card column>
      <CardHeader compact icon={icon}>
        <CardTitle>{label}</CardTitle>
      </CardHeader>
      <CardContent>
        <Stack gap="none">
          <Heading variant="metric">{value}</Heading>
          {sub && (
            <Text size="caption" color="muted">
              {sub}
            </Text>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
