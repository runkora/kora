'use client';

/** @tier 3 — agents/[slug]-scoped. The floating save bar for the Configuration
 * tab: appears on dirty state, one Save is one re-provision. Rebuilt from the
 * settings tab's bar on semantic tokens so it works in both themes
 * (docs/adr/2026-07-17-agent-detail-configuration.md §4). */

import { Button, Card, CardContent, Stack, StatusDot, Text } from '@orbit/terra';

export function ConfigurationSaveBar({
  visible,
  saving,
  saveSuccess,
  saveError,
  onSave,
  onDiscard,
}: {
  visible: boolean;
  saving: boolean;
  saveSuccess: boolean;
  saveError: string | null;
  onSave: () => void;
  onDiscard: () => void;
}) {
  if (!visible && !saveSuccess) return null;

  return (
    <Stack
      position="absolute"
      style={{
        position: 'fixed',
        bottom: '20px',
        left: '50%',
        transform: 'translateX(-50%)',
        zIndex: 40,
      }}
    >
      <Card border="default" subtle>
        <CardContent padded compact>
          <Stack direction="horizontal" align="center" gap="comfortable">
            <Stack direction="horizontal" align="center" gap="snug">
              <StatusDot
                size="sm"
                status={saveSuccess ? 'healthy' : 'degraded'}
                pulse={!saveSuccess}
              />
              <Text as="span" size="caption" color={saveSuccess ? 'foreground' : 'muted'}>
                {saveSuccess ? 'Changes saved' : 'Unsaved changes'}
              </Text>
            </Stack>
            {saveError && (
              <Text as="span" size="caption" color="status-error">
                {saveError}
              </Text>
            )}
            {!saveSuccess && (
              <Stack direction="horizontal" align="center" gap="snug">
                <Button variant="secondary" size="sm" onClick={onDiscard} disabled={saving}>
                  Discard
                </Button>
                <Button size="sm" onClick={onSave} disabled={saving}>
                  {saving ? 'Saving…' : 'Save changes'}
                </Button>
              </Stack>
            )}
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}
