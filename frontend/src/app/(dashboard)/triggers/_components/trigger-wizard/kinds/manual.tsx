'use client';

/** @tier 3 — triggers-scoped. The manual kind: one input declaration that both
 * renders the console form and validates the API call. */

import { useRef } from 'react';
import {
  Button,
  Card,
  CardContent,
  CodeBlock,
  FieldRow,
  FormField,
  Grid,
  Icon,
  Input,
  SectionLabel,
  SegmentedControl,
  Stack,
  Text,
  Textarea,
  ToggleRow,
} from '@orbit/terra';
import { CopyButton } from '@orbit/components';
import { VALID, deriveFieldKey, newField, previewBody, resolvedFields } from '../wizard-state';
import { manualCatalog } from '../field-catalog';
import { PromptFields } from '../prompt-fields';
import type {
  DoneContentProps,
  ReviewRow,
  StepComponentProps,
  TriggerKindDefinition,
  WizardField,
  WizardFieldType,
  WizardState,
} from '../types';

const FIELD_TYPES: { value: WizardFieldType; label: string }[] = [
  { value: 'text', label: 'Text' },
  { value: 'number', label: 'Number' },
  { value: 'choice', label: 'Choice' },
  { value: 'boolean', label: 'Boolean' },
];

const TYPE_HINT: Record<WizardFieldType, string> = {
  text: 'Text',
  number: 'Number',
  choice: 'Choose one',
  boolean: 'Toggle',
};

/** The trigger id is minted on create, so the path is only exact afterwards. */
function firePath(triggerId: string): string {
  return `POST /api/v1/triggers/${triggerId}/fire`;
}

function StepInputs({ state, patch }: StepComponentProps) {
  const promptRef = useRef<HTMLTextAreaElement>(null);
  const setField = (id: string, next: Partial<WizardField>) =>
    patch({ fields: state.fields.map((f) => (f.id === id ? { ...f, ...next } : f)) });

  const preview = resolvedFields(state.fields);

  return (
    <Stack gap="relaxed">
      <FormField
        label="Trigger name"
        id="wizard-name"
        hint="Shown in the triggers list and to anyone who can fire it."
        required
      >
        <Input
          id="wizard-name"
          mono
          value={state.name}
          onChange={(e) => patch({ name: e.target.value })}
          placeholder="run-analysis"
          maxLength={128}
        />
      </FormField>

      <Stack gap="cozy">
        <Stack direction="horizontal" align="center" justify="between">
          <SectionLabel>Input fields</SectionLabel>
          <Button
            size="xs"
            variant="outline"
            onClick={() => patch({ fields: [...state.fields, newField(state.fields.length)] })}
          >
            <Icon name="plus" size="xs" />
            Add field
          </Button>
        </Stack>

        <Stack gap="cozy">
          {state.fields.map((f, i) => (
            <Card key={f.id} inset>
              <CardContent padded compact>
                <Stack gap="cozy">
                  <Stack direction="horizontal" align="center" gap="cozy">
                    <Stack grow minWidth="none">
                      <Input
                        aria-label={`Field ${i + 1} label`}
                        value={f.label}
                        onChange={(e) => setField(f.id, { label: e.target.value })}
                        placeholder="Field label"
                        compact
                      />
                    </Stack>
                    <Text as="span" size="caption" mono color="subtle">
                      {deriveFieldKey(f.label, i)}
                    </Text>
                    <Button
                      size="icon-sm"
                      variant="ghost"
                      aria-label={`Remove field ${i + 1}`}
                      disabled={state.fields.length === 1}
                      onClick={() => patch({ fields: state.fields.filter((x) => x.id !== f.id) })}
                    >
                      <Icon name="close" size="xs" />
                    </Button>
                  </Stack>
                  <Stack direction="horizontal" align="center" gap="cozy" justify="between">
                    <SegmentedControl
                      aria-label={`Field ${i + 1} type`}
                      value={f.type}
                      onChange={(v) => setField(f.id, { type: v as WizardFieldType })}
                      options={FIELD_TYPES}
                    />
                    <Stack minWidth="form-field-sm">
                      <ToggleRow
                        label="Required"
                        checked={f.required}
                        onChange={(next) => setField(f.id, { required: next })}
                      />
                    </Stack>
                  </Stack>
                </Stack>
              </CardContent>
            </Card>
          ))}
        </Stack>
      </Stack>

      <Grid cols={2} gap="comfortable">
        <Stack gap="snug">
          <SectionLabel>Console form</SectionLabel>
          <Card inset>
            <CardContent padded compact>
              <Stack gap="cozy">
                {preview.length === 0 && (
                  <Text size="caption" color="subtle">
                    Fields with a label appear here.
                  </Text>
                )}
                {preview.map((f) => (
                  <Stack key={f.name} gap="tight">
                    <Stack direction="horizontal" align="center" gap="tight">
                      <Text as="span" size="caption" weight="strong" color="muted">
                        {f.label}
                      </Text>
                      {f.required && (
                        <Text as="span" size="caption" color="accent">
                          *
                        </Text>
                      )}
                    </Stack>
                    <Input readOnly disabled compact placeholder={TYPE_HINT[f.type]} value="" />
                  </Stack>
                ))}
                <Button size="sm" fullWidth disabled>
                  Run agent
                </Button>
              </Stack>
            </CardContent>
          </Card>
        </Stack>

        <Stack gap="snug">
          <SectionLabel>Same schema via API</SectionLabel>
          <Stack gap="snug" minWidth="none">
            <Text as="span" size="caption" mono color="accent">
              {firePath('{id}')}
            </Text>
            <CodeBlock text={previewBody(state.fields)} density="compact" language="json" />
            <Text size="caption" color="subtle">
              Callers are authorised by their own grants: the same policy check the console form
              goes through.
            </Text>
          </Stack>
        </Stack>
      </Grid>

      <FormField
        label="Prompt"
        id="wizard-prompt"
        hint="What the run starts with. Reference the inputs with {{ var.<key> }} placeholders."
        required
      >
        <Textarea
          id="wizard-prompt"
          ref={promptRef}
          value={state.promptTemplate}
          onChange={(e) => patch({ promptTemplate: e.target.value })}
          placeholder="Answer {{ var.question }} using the {{ var.dataset }} dataset."
          rows={4}
        />
      </FormField>

      <PromptFields
        entries={manualCatalog(state.fields)}
        value={state.promptTemplate}
        onChange={(next) => patch({ promptTemplate: next })}
        textareaRef={promptRef}
      />
    </Stack>
  );
}

function validateInputs(state: WizardState) {
  if (!state.name.trim()) return { valid: false, hint: 'Give the trigger a name.' };
  if (!state.fields.some((f) => f.label.trim()))
    return { valid: false, hint: 'Add at least one field with a label.' };
  if (!state.promptTemplate.trim())
    return { valid: false, hint: 'Write the prompt the run starts with.' };
  return VALID;
}

function DoneContent({ triggerId }: DoneContentProps) {
  const path = firePath(triggerId);
  return (
    <Stack gap="snug" minWidth="prose" maxWidth="prose">
      <FieldRow actions={<CopyButton value={path} label="Copy fire endpoint" />}>
        <Text as="span" size="caption" mono color="accent" truncate>
          {path}
        </Text>
      </FieldRow>
      <Text size="caption" color="subtle" align="center">
        Run it from the trigger&apos;s page, or POST the same input schema to this endpoint.
      </Text>
    </Stack>
  );
}

export const manualKind: TriggerKindDefinition = {
  id: 'manual',
  icon: 'play',
  title: 'Manual',
  description: 'A form in the console, and the same schema as a REST endpoint.',
  example: 'POST /api/v1/triggers/{id}/fire',
  steps: [
    {
      id: 'inputs',
      railTitle: 'Inputs',
      railSub: 'Form and API contract',
      description:
        'Declare what an operator is asked for when they run this agent. The same declaration renders the console form and validates the API call.',
      Component: StepInputs,
      validate: validateInputs,
    },
  ],
  summarize: (state) => {
    const fields = resolvedFields(state.fields);
    const rows: ReviewRow[] = [
      { key: 'Inputs', value: `${fields.length} ${fields.length === 1 ? 'field' : 'fields'}` },
    ];
    if (fields.length > 0) {
      rows.push({ key: 'Keys', value: fields.map((f) => f.name).join(', '), mono: true });
    }
    return rows;
  },
  draftPatch: (state) => ({ manualInputs: resolvedFields(state.fields) }),
  fieldCatalog: (state) => manualCatalog(state.fields),
  doneContent: DoneContent,
};
