'use client';

/**
 * @tier 3 · triggers-scoped. Limits who can fire a webhook or chat command
 * trigger. Anyone, or specific senders: the roles the provider reports, which
 * admit everyone holding a ticked one, plus listed sender ids, which admit those
 * senders as well. Used by the wizard and the modal. Renders nothing for
 * providers that identify no sender, and no role cards for providers that report
 * none. Every role's text comes from the provider's catalog.
 */

import { Fragment, useState, type ReactNode } from 'react';
import {
  Card,
  Checkbox,
  Grid,
  Icon,
  RadioCard,
  RadioGroup,
  SectionLabel,
  Separator,
  Stack,
  Text,
  Textarea,
} from '@runkora/hades';
import type { SenderRole } from '@runkora/shared/api';
import { splitIds } from './trigger-wizard/wizard-state';

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
  /** Leaves out the Senders title, for a screen that already has one; the choice is labelled instead. */
  hideHeading?: boolean;
  /** Rendered under the summary, e.g. the modal's save and discard buttons. */
  actions?: ReactNode;
}

/** How many listed ids the summary names before collapsing the rest into a count. */
const SUMMARY_IDS = 3;

const MODES = { anyone: 'anyone', specific: 'specific' } as const;

/** "a", "a and b", "a, b and c". */
function List({
  items,
  render,
  open = false,
}: {
  items: string[];
  render: (item: string) => ReactNode;
  /** Another item follows the list, so it joins with commas throughout. */
  open?: boolean;
}) {
  return (
    <>
      {items.map((item, i) => (
        <Fragment key={item}>
          {i > 0 && (i === items.length - 1 && !open ? ' and ' : ', ')}
          {render(item)}
        </Fragment>
      ))}
    </>
  );
}

/** One sentence on who the choices admit, so the combined effect is stated rather than inferred. */
function Summary({ roles, ids }: { roles: SenderRole[]; ids: string[] }) {
  const shown = ids.slice(0, SUMMARY_IDS);
  const more = ids.length - shown.length;
  return (
    <Card subtle border="none">
      <Stack direction="horizontal" gap="snug" align="start" padding="cozy">
        <Icon name="user" size="xs" color="muted" />
        <Text size="supporting" color="muted">
          {roles.length === 0 && ids.length === 0 ? (
            'Nothing chosen yet, so anyone can start a run.'
          ) : (
            <>
              Can start a run:{' '}
              {roles.length > 0 && (
                <>
                  every{' '}
                  <List
                    items={roles.map((r) => r.label)}
                    render={(label) => (
                      <Text as="span" size="supporting" weight="heavy" color="foreground">
                        {label}
                      </Text>
                    )}
                  />
                  {ids.length > 0 && ', also '}
                </>
              )}
              {ids.length > 0 && (
                <>
                  <List
                    items={shown}
                    open={more > 0}
                    render={(id) => (
                      <Text as="span" mono size="supporting" color="foreground">
                        {id}
                      </Text>
                    )}
                  />
                  {more > 0 && (
                    <>
                      {' and '}
                      <Text as="span" size="supporting" weight="heavy" color="foreground">
                        {more} more
                      </Text>
                    </>
                  )}
                </>
              )}
            </>
          )}
        </Text>
      </Stack>
    </Card>
  );
}

export function SenderRestrictionEditor({
  identifiesSenders,
  ...props
}: SenderRestrictionEditorProps) {
  // The mode lives in the inner component, so a provider that identifies no sender drops it.
  if (!identifiesSenders) return null;
  return <SenderChoice {...props} />;
}

function SenderChoice({
  roles,
  value,
  onChange,
  idPlaceholder = 'Separate sender ids with commas',
  disabled,
  hideHeading = false,
  actions,
}: Omit<SenderRestrictionEditorProps, 'identifiesSenders'>) {
  // A restriction with entries is always "specific"; the choice itself holds the mode while
  // nothing has been entered yet.
  const [pickedSpecific, setPickedSpecific] = useState(false);
  const ids = splitIds(value.ids);
  const specific = pickedSpecific || ids.length > 0 || value.roles.length > 0;
  const hasRoles = roles.length > 0;
  const idsLabel = hasRoles ? 'Additional senders' : 'Allowed senders';

  const chooseMode = (mode: string) => {
    if (mode === MODES.specific) {
      setPickedSpecific(true);
      return;
    }
    // Anyone means no restriction at all, so the entries go with the choice.
    setPickedSpecific(false);
    onChange({ ids: '', roles: [] });
  };
  const toggle = (role: string, on: boolean) =>
    onChange({
      ...value,
      roles: on ? [...value.roles, role] : value.roles.filter((r) => r !== role),
    });

  return (
    <Stack gap="comfortable">
      {!hideHeading && (
        <Stack gap="tight">
          <SectionLabel>Senders</SectionLabel>
          <Text size="caption" color="subtle">
            Limit which people and bots can start a run.
          </Text>
        </Stack>
      )}
      {hideHeading && (
        <Text as="span" size="supporting" weight="heavy">
          Who can start a run
        </Text>
      )}
      <RadioGroup
        value={specific ? MODES.specific : MODES.anyone}
        onValueChange={chooseMode}
        aria-label="Who can start a run"
      >
        <Grid cols={2} gap="cozy">
          <RadioCard
            value={MODES.anyone}
            selected={!specific}
            onSelect={chooseMode}
            title="Anyone"
            description="Every sender, no limits"
            disabled={disabled}
            transparent
          />
          <RadioCard
            value={MODES.specific}
            selected={specific}
            onSelect={chooseMode}
            title="Specific senders"
            description={hasRoles ? 'Chosen roles and senders only' : 'Listed senders only'}
            disabled={disabled}
            transparent
          />
        </Grid>
      </RadioGroup>

      {specific && (
        <Stack direction="horizontal" gap="cozy">
          <Separator orientation="vertical" />
          <Stack grow minWidth="none" gap="comfortable">
            {hasRoles && (
              <Stack gap="snug">
                <Stack gap="tight">
                  <Text as="span" size="supporting" weight="heavy">
                    Roles
                  </Text>
                  <Text size="caption" color="subtle">
                    Anyone with a ticked role can start a run.
                  </Text>
                </Stack>
                <Stack disabled={disabled}>
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
                </Stack>
              </Stack>
            )}
            <Stack gap="snug">
              <Stack gap="tight">
                <Text as="span" size="supporting" weight="heavy">
                  {idsLabel}
                </Text>
                <Text size="caption" color="subtle">
                  {hasRoles
                    ? 'Also allow these senders. Use for accounts with no role. Separate with commas.'
                    : "The platform's id for each sender, separated by commas."}
                </Text>
              </Stack>
              <Textarea
                aria-label={idsLabel}
                mono
                rows={2}
                resize="vertical"
                value={value.ids}
                disabled={disabled}
                onChange={(e) => onChange({ ...value, ids: e.target.value })}
                placeholder={idPlaceholder}
              />
            </Stack>
          </Stack>
        </Stack>
      )}

      {specific && <Summary roles={roles.filter((r) => value.roles.includes(r.value))} ids={ids} />}
      {actions}
    </Stack>
  );
}
