'use client';

/**
 * @tier 3 · triggers-scoped. Limits who can fire a webhook or chat command
 * trigger: listed sender ids, or anyone with a ticked role the provider
 * reports. Used by the wizard and the drawer. Renders nothing for providers
 * that identify no sender, and no role cards for providers that report none.
 * Every role's text comes from the provider's catalog.
 */

import { useId, type ReactNode } from 'react';
import { Card, Checkbox, FormField, Grid, Input, SectionLabel, Stack, Text } from '@runkora/hades';
import type { SenderRole } from '@runkora/shared/api';

export interface SenderRestrictionDraft {
  /** Raw while typed; split on commas and whitespace when saved. */
  ids: string;
  roles: string[];
}

interface SenderRestrictionEditorProps {
  /** Whether the provider says who caused an event; false renders nothing. */
  identifiesSenders: boolean;
  /** The roles the provider reports; empty hides the role cards. */
  roles: SenderRole[];
  value: SenderRestrictionDraft;
  onChange: (next: SenderRestrictionDraft) => void;
  /** An example of the provider's sender ids. */
  idPlaceholder?: string;
  disabled?: boolean;
  /** Rendered under the role cards, e.g. the drawer's save and discard buttons. */
  actions?: ReactNode;
}

export function SenderRestrictionEditor({
  identifiesSenders,
  roles,
  value,
  onChange,
  idPlaceholder = 'Separate sender ids with commas',
  disabled,
  actions,
}: SenderRestrictionEditorProps) {
  const idsId = useId();
  if (!identifiesSenders) return null;
  const toggle = (role: string, on: boolean) =>
    onChange({
      ...value,
      roles: on ? [...value.roles, role] : value.roles.filter((r) => r !== role),
    });

  return (
    <Stack gap="snug">
      <Stack gap="tight">
        <SectionLabel>Senders</SectionLabel>
        <Text size="caption" color="subtle">
          Leave empty to accept events from anyone. Otherwise a sender must be listed
          {roles.length > 0 && ' or have a ticked role'}.
        </Text>
      </Stack>
      <FormField
        label="Allowed senders"
        id={idsId}
        hint="The platform's id for each sender, separated by commas."
      >
        <Input
          id={idsId}
          mono
          value={value.ids}
          disabled={disabled}
          onChange={(e) => onChange({ ...value, ids: e.target.value })}
          placeholder={idPlaceholder}
        />
      </FormField>
      {roles.length > 0 && (
        <Grid cols={3} gap="cozy">
          {roles.map((role) => {
            const checked = value.roles.includes(role.value);
            return (
              <Card
                key={role.value}
                interactive={!disabled}
                selected={checked}
                selectedVariant="outline"
              >
                <Stack
                  direction="horizontal"
                  align="start"
                  justify="between"
                  gap="cozy"
                  padding="cozy"
                  onClick={() => !disabled && toggle(role.value, !checked)}
                >
                  <Stack gap="none">
                    <Text as="span" weight="strong">
                      {role.label}
                    </Text>
                    {role.description && (
                      <Text as="span" size="caption" color="subtle">
                        {role.description}
                      </Text>
                    )}
                  </Stack>
                  {/* The card's click toggles; stop the checkbox's own click from toggling twice. */}
                  <Stack onClick={(e) => e.stopPropagation()}>
                    <Checkbox
                      size="lg"
                      checked={checked}
                      disabled={disabled}
                      onChange={(on) => toggle(role.value, on)}
                      aria-label={role.label}
                    />
                  </Stack>
                </Stack>
              </Card>
            );
          })}
        </Grid>
      )}
      {actions}
    </Stack>
  );
}
