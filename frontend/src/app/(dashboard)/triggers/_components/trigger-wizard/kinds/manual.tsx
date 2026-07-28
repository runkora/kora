'use client';

/** @tier 3 — triggers-scoped. The manual kind: one input declaration that both
 * renders the console form and validates the API call. */

import { useCallback, useEffect, useRef } from 'react';
import {
  Button,
  Card,
  CardContent,
  CodeBlock,
  FieldRow,
  FormField,
  Icon,
  Input,
  SectionLabel,
  SegmentedControl,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
  Textarea,
  ToggleRow,
} from '@orbit/terra';
import { CopyButton } from '@orbit/components';
import {
  FIELD_IDS,
  VALID,
  deriveFieldKey,
  firstBlocker,
  newField,
  previewBody,
  resolvedFields,
  validateName,
  validatePrompt,
} from '../wizard-state';
import type {
  DoneContentProps,
  ReviewRow,
  StepComponentProps,
  StepValidation,
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

function fieldInputId(field: WizardField): string {
  return `wizard-field-${field.id}`;
}

function StepInputs({ state, patch, fieldError, firstFieldRef }: StepComponentProps) {
  // A field added by the person should be the thing they are typing in next.
  const pendingFocus = useRef<string | null>(null);

  const setField = (id: string, next: Partial<WizardField>) =>
    patch({ fields: state.fields.map((f) => (f.id === id ? { ...f, ...next } : f)) });

  const addField = useCallback(() => {
    const field = newField(state.fields.length);
    pendingFocus.current = fieldInputId(field);
    patch({ fields: [...state.fields, field] });
  }, [state.fields, patch]);

  useEffect(() => {
    if (!pendingFocus.current) return;
    const el = document.getElementById(pendingFocus.current);
    pendingFocus.current = null;
    if (el instanceof HTMLElement) {
      el.scrollIntoView?.({ block: 'nearest' });
      el.focus();
    }
  }, [state.fields.length]);

  const preview = resolvedFields(state.fields);

  return (
    <Stack gap="relaxed">
      <FormField
        label="Trigger name"
        id={FIELD_IDS.name}
        hint="Shown in the triggers list and on every run it starts."
        error={fieldError(FIELD_IDS.name)}
        required
      >
        <Input
          id={FIELD_IDS.name}
          ref={firstFieldRef}
          mono
          value={state.name}
          onChange={(e) => patch({ name: e.target.value })}
          placeholder="run-analysis"
          maxLength={128}
        />
      </FormField>

      <Stack gap="cozy" id={FIELD_IDS.fields}>
        <SectionLabel>Input fields</SectionLabel>

        <Stack gap="cozy">
          {state.fields.map((f, i) => (
            <Card key={f.id} inset>
              <CardContent padded compact>
                <Stack gap="cozy">
                  <Stack direction="horizontal" align="center" gap="cozy">
                    <Stack grow minWidth="none">
                      <Input
                        id={fieldInputId(f)}
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

        <Button variant="ghost" size="sm" fullWidth onClick={addField}>
          <Icon name="plus" size="xs" />
          Add field
        </Button>

        {fieldError(FIELD_IDS.fields) && (
          <Text size="caption" color="status-error">
            {fieldError(FIELD_IDS.fields)}
          </Text>
        )}
      </Stack>

      <FormField
        label="Prompt"
        id={FIELD_IDS.prompt}
        hint="What the run starts with. Reference the inputs with {{ var.<key> }} placeholders."
        error={fieldError(FIELD_IDS.prompt)}
        required
      >
        <Textarea
          id={FIELD_IDS.prompt}
          value={state.promptTemplate}
          onChange={(e) => patch({ promptTemplate: e.target.value })}
          placeholder="Answer {{ var.question }} using the {{ var.dataset }} dataset."
          rows={4}
        />
      </FormField>

      <Card inset>
        <CardContent flush>
          <Tabs defaultValue="form" size="compact">
            <TabsList>
              <TabsTrigger value="form">Console form</TabsTrigger>
              <TabsTrigger value="api">Via API</TabsTrigger>
            </TabsList>
            <TabsContent value="form">
              <Stack gap="cozy" padding="comfortable">
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
            </TabsContent>
            <TabsContent value="api">
              <Stack gap="snug" padding="comfortable" minWidth="none">
                <Text as="span" size="caption" mono color="accent">
                  {firePath('{id}')}
                </Text>
                <CodeBlock text={previewBody(state.fields)} density="compact" language="json" />
              </Stack>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </Stack>
  );
}

function validateFields(state: WizardState): StepValidation {
  if (!state.fields.some((f) => f.label.trim()))
    return { valid: false, blocker: 'Needs one labelled field', fieldId: FIELD_IDS.fields };
  return VALID;
}

function DoneContent({ triggerId }: DoneContentProps) {
  const path = firePath(triggerId);
  return (
    <Stack gap="snug">
      <FieldRow actions={<CopyButton value={path} label="Copy fire endpoint" />}>
        <Text as="span" size="caption" mono color="accent" truncate>
          {path}
        </Text>
      </FieldRow>
      <Text size="caption" color="subtle">
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
      description:
        'The fields someone fills in before the run starts. The same fields build the console form and validate the API call.',
      Component: StepInputs,
      validate: (state) =>
        firstBlocker(
          () => validateName(state),
          () => validateFields(state),
          () => validatePrompt(state),
        ),
    },
  ],
  summarize: (state) => {
    const fields = resolvedFields(state.fields);
    const rows: ReviewRow[] = [
      {
        key: 'Input fields',
        value: `${fields.length} ${fields.length === 1 ? 'field' : 'fields'}`,
        description: 'Asked for in the console and validated on the API call.',
      },
    ];
    if (fields.length > 0) {
      rows.push({
        key: 'Keys',
        value: fields.map((f) => f.name).join(', '),
        mono: true,
        description: 'What callers send in the request body.',
      });
    }
    return rows;
  },
  draftPatch: (state) => ({ manualInputs: resolvedFields(state.fields) }),
  doneContent: DoneContent,
};
