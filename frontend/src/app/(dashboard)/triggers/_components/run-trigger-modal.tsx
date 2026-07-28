'use client';

/** @tier 3 — triggers-scoped. The console form a manual trigger publishes:
 * the declared inputs, rendered for real, firing with their values. */

import { useState } from 'react';
import {
  Alert,
  Button,
  FormField,
  Input,
  Modal,
  ModalAccessibleTitle,
  NativeSelect,
  Stack,
  Text,
  ToggleRow,
} from '@orbit/terra';
import type { InputField } from '@orbit/shared/api';
import type { TriggerRow } from '../_types';

interface RunTriggerModalProps {
  trigger: TriggerRow;
  onClose: () => void;
  /** Fires the trigger with the collected variables; resolves with an error to show, or null. */
  onFire: (variables: Record<string, string>) => Promise<string | null>;
}

/** Initial form values: declared defaults, else empty. */
function initialValues(fields: InputField[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const f of fields) {
    out[f.name] = f.default ?? (f.type === 'boolean' ? 'false' : '');
  }
  return out;
}

/**
 * RunTriggerModal renders the input schema a manual trigger declares as a real
 * form. The same declaration validates the API call, so what this form submits
 * is exactly what `POST /fire` accepts; a required field left empty here is
 * caught before the request rather than bounced by the server.
 */
export function RunTriggerModal({ trigger, onClose, onFire }: RunTriggerModalProps) {
  const fields = trigger.manualInputs ?? [];
  const [values, setValues] = useState<Record<string, string>>(() => initialValues(fields));
  const [attempted, setAttempted] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const missing = (f: InputField): boolean =>
    f.required === true && (values[f.name] ?? '').trim() === '';
  const invalid = fields.some(missing);

  const set = (name: string, v: string) => setValues((prev) => ({ ...prev, [name]: v }));

  const submit = async () => {
    setAttempted(true);
    if (invalid || pending) return;
    setPending(true);
    setError(null);
    // Only send what is set: an optional field left empty stays absent rather
    // than arriving as an empty string the prompt then renders literally.
    const variables: Record<string, string> = {};
    for (const f of fields) {
      const v = (values[f.name] ?? '').trim();
      if (v !== '') variables[f.name] = v;
    }
    const failure = await onFire(variables);
    setPending(false);
    if (failure !== null) {
      setError(failure);
      return;
    }
    onClose();
  };

  return (
    <Modal open onClose={onClose} size="sm">
      <ModalAccessibleTitle>Run {trigger.name}</ModalAccessibleTitle>
      <Stack gap="comfortable" padding="relaxed">
        <Stack gap="tight">
          <Text size="supporting" weight="heavy">
            Run {trigger.name}
          </Text>
          <Text size="caption" color="muted">
            The run starts as the trigger&apos;s own identity, checked against its grants.
          </Text>
        </Stack>

        {error !== null && <Alert variant="error">{error}</Alert>}

        {fields.map((f) => {
          const label = f.label ?? f.name;
          const fieldError = attempted && missing(f) ? `${label} is required` : undefined;
          if (f.type === 'boolean') {
            return (
              <ToggleRow
                key={f.name}
                checked={values[f.name] === 'true'}
                onChange={(next) => set(f.name, next ? 'true' : 'false')}
                label={label}
                description={f.description}
              />
            );
          }
          return (
            <FormField
              key={f.name}
              id={`run-${f.name}`}
              label={label}
              hint={f.description}
              required={f.required === true}
              error={fieldError}
            >
              {f.type === 'choice' ? (
                <NativeSelect
                  id={`run-${f.name}`}
                  value={values[f.name] ?? ''}
                  onChange={(e) => set(f.name, e.target.value)}
                >
                  <option value="">Choose…</option>
                  {(f.choices ?? []).map((c) => (
                    <option key={c} value={c}>
                      {c}
                    </option>
                  ))}
                </NativeSelect>
              ) : (
                <Input
                  id={`run-${f.name}`}
                  type={f.type === 'number' ? 'number' : 'text'}
                  value={values[f.name] ?? ''}
                  onChange={(e) => set(f.name, e.target.value)}
                />
              )}
            </FormField>
          );
        })}

        <Stack direction="horizontal" justify="end" gap="snug">
          <Button variant="secondary" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={() => void submit()} disabled={pending}>
            {pending ? 'Queueing…' : 'Run agent'}
          </Button>
        </Stack>
      </Stack>
    </Modal>
  );
}
