/** @tier 3 — sign-in-callback-scoped. Promote to src/components/ if a second feature needs this. */
'use client';

import { Button, Card, CardContent, CardHeader, Heading, Stack, Text } from '@runkora/hades';

export interface SwitchDeniedProps {
  /** Returns to the page the switch started from, still in the current tenant. */
  onContinue: () => void;
}

/** Tells the person a tenant switch was refused and that they are still signed in. */
export function SwitchDenied({ onContinue }: SwitchDeniedProps) {
  return (
    <Card>
      <CardHeader>
        <Heading variant="section">Switch failed</Heading>
      </CardHeader>
      <CardContent>
        <Stack gap="comfortable">
          <Text size="supporting" color="muted">
            The switch to that tenant was refused. Your access may have changed.
          </Text>
          <Button fullWidth onClick={onContinue}>
            Continue
          </Button>
        </Stack>
      </CardContent>
    </Card>
  );
}
