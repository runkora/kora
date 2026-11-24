'use client';

/**
 * @tier 3 · triggers-scoped. The attribution form of a generic source: links,
 * the actor, the object and the verb map. Used by the wizard and the detail modal.
 */

import { useId } from 'react';
import type { ReactNode } from 'react';
import {
  Button,
  FormField,
  Grid,
  Icon,
  Input,
  NativeSelect,
  SectionLabel,
  Stack,
  Text,
} from '@runkora/hades';
import {
  ACTOR_KINDS,
  MAX_ATTRIBUTION_LINKS,
  templateInvalid,
  textTooLong,
  type ActorKind,
  type AttributionDraft,
} from '../_lib/generic-source';

interface AttributionFieldsProps {
  value: AttributionDraft;
  onChange: (next: AttributionDraft) => void;
  disabled?: boolean;
}

/** Marks an input invalid, in its border and for assistive technology. */
const invalid = (bad: boolean) => ({ hasError: bad, 'aria-invalid': bad || undefined });

/** One row of two inputs and a remove button, for a link or a verb. */
function PairRow({
  first,
  second,
  onRemove,
  removeLabel,
  disabled,
}: {
  first: ReactNode;
  second: ReactNode;
  onRemove: () => void;
  removeLabel: string;
  disabled: boolean;
}) {
  return (
    <Stack direction="horizontal" gap="snug" align="center">
      <Stack grow minWidth="none">
        <Grid cols={2} gap="snug">
          {first}
          {second}
        </Grid>
      </Stack>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={removeLabel}
        disabled={disabled}
        onClick={onRemove}
      >
        <Icon name="trash" size="2xs" />
      </Button>
    </Stack>
  );
}

function AddButton({
  label,
  disabled,
  onClick,
}: {
  label: string;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <Stack direction="horizontal">
      <Button variant="ghost" size="xs" disabled={disabled} onClick={onClick}>
        <Icon name="plus" size="2xs" />
        {label}
      </Button>
    </Stack>
  );
}

export function AttributionFields({ value, onChange, disabled = false }: AttributionFieldsProps) {
  const id = useId();
  const set = (next: Partial<AttributionDraft>) => onChange({ ...value, ...next });
  const setLink = (i: number, next: Partial<AttributionDraft['links'][number]>) =>
    set({ links: value.links.map((l, j) => (j === i ? { ...l, ...next } : l)) });
  const setVerb = (i: number, next: Partial<AttributionDraft['verbs'][number]>) =>
    set({ verbs: value.verbs.map((v, j) => (j === i ? { ...v, ...next } : v)) });

  return (
    <Stack gap="comfortable">
      <Text size="caption" color="subtle">
        Mix fixed text with payload paths in {'{{ }}'}, such as {'{{ data.issue.id }}'}. A path the
        payload lacks renders empty.
      </Text>

      <Stack gap="snug">
        <SectionLabel>Links</SectionLabel>
        {value.links.map((link, i) => {
          // Marks the half of a half-written row that is missing, as the filter rows do.
          const hasLabel = link.label.trim() !== '';
          const hasUrl = link.url.trim() !== '';
          return (
            <PairRow
              key={i}
              first={
                <Input
                  mono
                  aria-label={`Link ${i + 1} label`}
                  value={link.label}
                  disabled={disabled}
                  {...invalid((!hasLabel && hasUrl) || templateInvalid(link.label, false))}
                  onChange={(e) => setLink(i, { label: e.target.value })}
                  placeholder="#{{ data.number }} {{ data.title }}"
                />
              }
              second={
                <Input
                  mono
                  aria-label={`Link ${i + 1} URL`}
                  value={link.url}
                  disabled={disabled}
                  {...invalid((!hasUrl && hasLabel) || templateInvalid(link.url, true))}
                  onChange={(e) => setLink(i, { url: e.target.value })}
                  placeholder="https://tracker.example.com/issues/{{ data.id }}"
                />
              }
              removeLabel={`Remove link ${i + 1}`}
              disabled={disabled}
              onRemove={() => set({ links: value.links.filter((_, j) => j !== i) })}
            />
          );
        })}
        <AddButton
          label="Add link"
          disabled={disabled || value.links.length >= MAX_ATTRIBUTION_LINKS}
          onClick={() => set({ links: [...value.links, { label: '', url: '' }] })}
        />
      </Stack>

      <Stack gap="snug">
        <SectionLabel>Event summary</SectionLabel>
        <Text size="caption" color="subtle">
          Who acted and what they acted on. Set both, or neither.
        </Text>
        <Grid cols={3} gap="cozy">
          <FormField label="Actor name" id={`${id}-actor-name`}>
            <Input
              mono
              value={value.actor.name}
              disabled={disabled}
              onChange={(e) => set({ actor: { ...value.actor, name: e.target.value } })}
              placeholder="{{ data.user.name }}"
            />
          </FormField>
          <FormField label="Actor kind" id={`${id}-actor-kind`}>
            <NativeSelect
              value={value.actor.kind}
              disabled={disabled}
              onChange={(e) =>
                set({ actor: { ...value.actor, kind: e.target.value as ActorKind | '' } })
              }
            >
              <option value="">Choose</option>
              {ACTOR_KINDS.map((k) => (
                <option key={k.value} value={k.value}>
                  {k.label}
                </option>
              ))}
            </NativeSelect>
          </FormField>
          <FormField label="Actor URL" id={`${id}-actor-url`}>
            <Input
              mono
              value={value.actor.url}
              disabled={disabled}
              onChange={(e) => set({ actor: { ...value.actor, url: e.target.value } })}
              placeholder="Optional"
            />
          </FormField>
        </Grid>
        <Grid cols={2} gap="cozy">
          <FormField label="Object label" id={`${id}-object-label`}>
            <Input
              mono
              value={value.object.label}
              disabled={disabled}
              onChange={(e) => set({ object: { ...value.object, label: e.target.value } })}
              placeholder="#{{ data.number }} {{ data.title }}"
            />
          </FormField>
          <FormField label="Object URL" id={`${id}-object-url`}>
            <Input
              mono
              value={value.object.url}
              disabled={disabled}
              onChange={(e) => set({ object: { ...value.object, url: e.target.value } })}
              placeholder="Optional"
            />
          </FormField>
        </Grid>
      </Stack>

      <Stack gap="snug">
        <SectionLabel>Verbs</SectionLabel>
        <Text size="caption" color="subtle">
          The verb for each event type. An event type with none uses its own name.
        </Text>
        {value.verbs.map((verb, i) => {
          const eventType = verb.eventType.trim();
          const hasVerb = verb.verb.trim() !== '';
          // An event type mapped by an earlier row is the duplicate.
          const repeated =
            eventType !== '' &&
            value.verbs.slice(0, i).some((v) => v.eventType.trim() === eventType);
          return (
            <PairRow
              key={i}
              first={
                <Input
                  mono
                  aria-label={`Verb ${i + 1} event type`}
                  value={verb.eventType}
                  disabled={disabled}
                  {...invalid((eventType === '' && hasVerb) || repeated || textTooLong(eventType))}
                  onChange={(e) => setVerb(i, { eventType: e.target.value })}
                  placeholder="issue.comment.created"
                />
              }
              second={
                <Input
                  aria-label={`Verb ${i + 1}`}
                  value={verb.verb}
                  disabled={disabled}
                  {...invalid((!hasVerb && eventType !== '') || textTooLong(verb.verb))}
                  onChange={(e) => setVerb(i, { verb: e.target.value })}
                  placeholder="commented on"
                />
              }
              removeLabel={`Remove verb ${i + 1}`}
              disabled={disabled}
              onRemove={() => set({ verbs: value.verbs.filter((_, j) => j !== i) })}
            />
          );
        })}
        <AddButton
          label="Add verb"
          disabled={disabled}
          onClick={() => set({ verbs: [...value.verbs, { eventType: '', verb: '' }] })}
        />
      </Stack>
    </Stack>
  );
}
