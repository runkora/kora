'use client';

/**
 * @tier 3 — triggers-scoped. Limits who can fire a webhook trigger: named
 * logins, or anyone whose relationship to the repository is ticked. Used by
 * the wizard and the drawer. Renders nothing for providers that identify no
 * sender.
 */

import { useId, type ReactNode } from 'react';
import { Card, Checkbox, FormField, Grid, Input, SectionLabel, Stack, Text } from '@runkora/hades';

export interface SenderRestrictionDraft {
  /** Raw while typed; split on commas and whitespace when saved. */
  logins: string;
  associations: string[];
}

/** Card text for the repository relationships GitHub reports; other values show as-is. */
const ROLE_TEXT: Record<string, { title: string; description: string }> = {
  OWNER: { title: 'Owner', description: 'Owns the repository' },
  MEMBER: { title: 'Member', description: 'Member of the organisation' },
  COLLABORATOR: { title: 'Collaborator', description: 'Invited to the repository' },
};

interface SenderRestrictionEditorProps {
  /** The association values the provider reports; empty hides the control. */
  available: string[];
  value: SenderRestrictionDraft;
  onChange: (next: SenderRestrictionDraft) => void;
  disabled?: boolean;
  /** Rendered under the role cards, e.g. the drawer's save and discard buttons. */
  actions?: ReactNode;
}

export function SenderRestrictionEditor({
  available,
  value,
  onChange,
  disabled,
  actions,
}: SenderRestrictionEditorProps) {
  const loginsId = useId();
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
      <FormField label="Logins" id={loginsId}>
        <Input
          id={loginsId}
          mono
          value={value.logins}
          disabled={disabled}
          onChange={(e) => onChange({ ...value, logins: e.target.value })}
          placeholder="octocat, hubot"
        />
      </FormField>
      <Grid cols={3} gap="cozy">
        {available.map((association) => {
          const checked = value.associations.includes(association);
          const text = ROLE_TEXT[association] ?? {
            title: association.toLowerCase(),
            description: '',
          };
          return (
            <Card
              key={association}
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
                onClick={() => !disabled && toggle(association, !checked)}
              >
                <Stack gap="none">
                  <Text as="span" weight="strong">
                    {text.title}
                  </Text>
                  {text.description && (
                    <Text as="span" size="caption" color="subtle">
                      {text.description}
                    </Text>
                  )}
                </Stack>
                {/* The card's click toggles; stop the checkbox's own click from toggling twice. */}
                <Stack onClick={(e) => e.stopPropagation()}>
                  <Checkbox
                    size="lg"
                    checked={checked}
                    disabled={disabled}
                    onChange={(on) => toggle(association, on)}
                    aria-label={text.title}
                  />
                </Stack>
              </Stack>
            </Card>
          );
        })}
      </Grid>
      {actions}
    </Stack>
  );
}
