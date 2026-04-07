import type { Meta, StoryObj } from '@storybook/react';
import { Activity } from 'lucide-react';
import { Badge } from '../badge/badge';
import { Button } from '../button/button';
import { Text } from '../typography/typography';
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './card';

const meta: Meta<typeof Card> = {
  title: 'Terra/Atoms/Card',
  component: Card,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Card>;

export const Default: Story = {
  render: () => (
    <Card className="w-80">
      <CardHeader>
        <CardTitle>Agent: apollo-3</CardTitle>
        <CardDescription>Last active 2 minutes ago</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Processing 14 pending tasks across 3 connected tools.
        </Text>
      </CardContent>
    </Card>
  ),
};

export const WithFooter: Story = {
  render: () => (
    <Card className="w-80">
      <CardHeader>
        <CardTitle>Credential broker</CardTitle>
        <CardDescription>AWS us-east-1 · expires in 12 min</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Scoped read-only access to S3 and DynamoDB.
        </Text>
      </CardContent>
      <CardFooter className="justify-between">
        <Button variant="ghost" size="sm">
          Revoke
        </Button>
        <Button variant="secondary" size="sm">
          Renew
        </Button>
      </CardFooter>
    </Card>
  ),
};

export const WithBadge: Story = {
  render: () => (
    <Card className="w-80">
      <CardHeader>
        <div className="flex items-start justify-between gap-2">
          <CardTitle>Policy engine</CardTitle>
          <Badge variant="healthy" dot>
            Healthy
          </Badge>
        </div>
        <CardDescription>OPA v0.63 · 847 rules loaded</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Evaluated 2,341 decisions in the last hour. Zero violations.
        </Text>
      </CardContent>
    </Card>
  ),
};

export const WithIcon: Story = {
  render: () => (
    <Card column className="w-72">
      <CardHeader compact icon={<Activity className="size-4" />}>
        <CardTitle>Total Requests</CardTitle>
        <CardDescription>Last billing period</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          1.2M requests · 98.4% success rate
        </Text>
      </CardContent>
    </Card>
  ),
};

export const ElevatedSurface: Story = {
  render: () => (
    <Card className="w-80 bg-surface-elevated">
      <CardHeader>
        <CardTitle>Elevated card</CardTitle>
        <CardDescription>Uses bg-surface-elevated for nested depth</CardDescription>
      </CardHeader>
      <CardContent>
        <Text size="supporting" color="muted">
          Place this inside another surface to express a second level of depth.
        </Text>
      </CardContent>
    </Card>
  ),
};
