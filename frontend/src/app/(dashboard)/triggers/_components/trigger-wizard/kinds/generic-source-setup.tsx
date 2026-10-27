'use client';

/**
 * @tier 3 · triggers-scoped. A new generic webhook source's settings on the
 * source step: how deliveries are verified, where the secret comes from, and
 * where the event type sits. The source is created with the trigger.
 */

import {
  FormField,
  Grid,
  Input,
  RadioCard,
  RadioGroup,
  SectionLabel,
  SegmentedControl,
  Stack,
  Text,
} from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import {
  GENERIC_EVENT_TYPE,
  takesPastedSecret,
  VERIFICATION_METHODS,
  type EventTypeLocation,
  type GenericSourceDraft,
  type SecretChoice,
  type SignatureEncoding,
  type VerificationMethod,
} from '../../../_lib/generic-source';

interface GenericSourceSetupProps {
  draft: GenericSourceDraft;
  onChange: (next: GenericSourceDraft) => void;
  /** The pasted secret, held apart from the draft so it never reaches review rows. */
  secret: string;
  onSecretChange: (secret: string) => void;
}

const EVENT_LOCATIONS: { value: EventTypeLocation; label: string }[] = [
  { value: 'none', label: 'None' },
  { value: 'header', label: 'Header' },
  { value: 'path', label: 'Payload field' },
];

export function GenericSourceSetup({
  draft,
  onChange,
  secret,
  onSecretChange,
}: GenericSourceSetupProps) {
  const { productName } = useBrand();
  const set = (next: Partial<GenericSourceDraft>) => onChange({ ...draft, ...next });

  return (
    <Stack gap="cozy">
      <Stack gap="snug">
        <SectionLabel>Verification</SectionLabel>
        <RadioGroup
          value={draft.method}
          onValueChange={(v) => set({ method: v as VerificationMethod })}
          aria-label="Verification"
        >
          <Grid cols={3} gap="cozy">
            {VERIFICATION_METHODS.map((m) => (
              <RadioCard
                key={m.value}
                value={m.value}
                selected={draft.method === m.value}
                onSelect={(v) => set({ method: v as VerificationMethod })}
                title={m.title}
                description={m.description}
              />
            ))}
          </Grid>
        </RadioGroup>
      </Stack>

      {draft.method === 'hmac_sha256' && (
        <Grid cols={3} gap="cozy">
          <FormField label="Signature header" id="wizard-generic-header" required>
            <Input
              id="wizard-generic-header"
              mono
              value={draft.header}
              onChange={(e) => set({ header: e.target.value })}
              placeholder="X-Signature"
              maxLength={128}
            />
          </FormField>
          <FormField label="Prefix" id="wizard-generic-prefix" hint="Stripped before decoding.">
            <Input
              id="wizard-generic-prefix"
              mono
              value={draft.prefix}
              onChange={(e) => set({ prefix: e.target.value })}
              placeholder="sha256="
              maxLength={32}
            />
          </FormField>
          <FormField label="Encoding" id="wizard-generic-encoding">
            <SegmentedControl
              id="wizard-generic-encoding"
              aria-label="Encoding"
              value={draft.encoding}
              onChange={(v) => set({ encoding: v as SignatureEncoding })}
              options={[
                { value: 'hex', label: 'Hex' },
                { value: 'base64', label: 'Base64' },
              ]}
            />
          </FormField>
        </Grid>
      )}

      {takesPastedSecret(draft.method) && (
        <Stack gap="snug">
          <SectionLabel>Signing secret</SectionLabel>
          <SegmentedControl
            aria-label="Signing secret"
            value={draft.secretChoice}
            onChange={(v) => set({ secretChoice: v as SecretChoice })}
            options={[
              { value: 'generate', label: `${productName} generates it` },
              { value: 'paste', label: "Paste the sender's secret" },
            ]}
          />
          {draft.secretChoice === 'paste' ? (
            <FormField
              label="Secret"
              id="wizard-generic-secret"
              required
              hint={
                draft.method === 'standard_webhooks'
                  ? 'Base64, with or without the whsec_ prefix. It is stored and never shown back.'
                  : 'It is stored and never shown back.'
              }
            >
              <Input
                id="wizard-generic-secret"
                type="password"
                autoComplete="off"
                mono
                value={secret}
                onChange={(e) => onSecretChange(e.target.value)}
                placeholder="The secret your sender issued"
                maxLength={256}
              />
            </FormField>
          ) : (
            <Text size="caption" color="subtle">
              Shown once, after the trigger is created. Give it to your sender then.
            </Text>
          )}
        </Stack>
      )}
      {draft.method === 'bearer' && (
        <Text size="caption" color="subtle">
          {productName} generates the token and shows it once, after the trigger is created.
        </Text>
      )}

      <Stack gap="snug">
        <Stack gap="tight">
          <SectionLabel>Event type location</SectionLabel>
          <Text size="caption" color="subtle">
            Where the sender puts the event type. Without one, every delivery has the event type{' '}
            {GENERIC_EVENT_TYPE}.
          </Text>
        </Stack>
        <SegmentedControl
          aria-label="Event type location"
          value={draft.eventLocation}
          onChange={(v) => set({ eventLocation: v as EventTypeLocation, eventKey: '' })}
          options={EVENT_LOCATIONS}
        />
        {draft.eventLocation !== 'none' && (
          <Input
            mono
            aria-label={draft.eventLocation === 'header' ? 'Event type header' : 'Event type field'}
            value={draft.eventKey}
            onChange={(e) => set({ eventKey: e.target.value })}
            placeholder={draft.eventLocation === 'header' ? 'X-Event-Key' : 'type'}
            maxLength={128}
          />
        )}
      </Stack>
    </Stack>
  );
}
