'use client';

/**
 * @tier 3 — triggers-scoped. Limits who can fire a webhook trigger: named
 * logins, or anyone whose relationship to the repository is ticked. Used by
 * the wizard and the drawer. Renders nothing for providers that identify no
 * sender.
 */

import { Checkbox, FormField, Input, SectionLabel, Stack, Text } from '@orbit/terra';

export interface SenderRestrictionDraft {
  /** Raw while typed; split on commas and whitespace when saved. */
  logins: string;
  associations: string[];
}

interface SenderRestrictionEditorProps {
  /** The association values the provider reports; empty hides the control. */
  available: string[];
  value: SenderRestrictionDraft;
  onChange: (next: SenderRestrictionDraft) => void;
  disabled?: boolean;
  /** Distinguishes the login input's id when two editors share a page. */
  idPrefix: string;
}

export function SenderRestrictionEditor({
  available,
  value,
  onChange,
  disabled,
  idPrefix,
}: SenderRestrictionEditorProps) {
  if (available.length === 0) return null;
  const toggle = (association: string, on: boolean) =>
    onChange({
      ...value,
      associations: on
        ? [...value.associations, association]
        : value.associations.filter((a) => a !== association),
    });

  return (
    <Stack gap="snug">
      <Stack gap="tight">
        <SectionLabel>Senders</SectionLabel>
        <Text size="caption" color="subtle">
          Leave empty to accept events from anyone. Otherwise a sender must be listed or have a
          ticked relationship to the repository.
        </Text>
      </Stack>
      <FormField label="Logins" id={`${idPrefix}-sender-logins`}>
        <Input
          id={`${idPrefix}-sender-logins`}
          mono
          value={value.logins}
          disabled={disabled}
          onChange={(e) => onChange({ ...value, logins: e.target.value })}
          placeholder="octocat, hubot"
        />
      </FormField>
      <Stack direction="horizontal" gap="relaxed" wrap>
        {available.map((association) => (
          <Checkbox
            key={association}
            checked={value.associations.includes(association)}
            disabled={disabled}
            onChange={(on) => toggle(association, on)}
          >
            {association.toLowerCase()}
          </Checkbox>
        ))}
      </Stack>
    </Stack>
  );
}
