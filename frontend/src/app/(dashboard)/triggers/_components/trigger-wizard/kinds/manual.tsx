'use client';

/** @tier 3 — triggers-scoped. The manual kind: one input declaration that both
 * renders the console form and validates the API call. */

import {
  CopyButton,
  Button,
  Card,
  CardContent,
  CodeBlock,
  FieldRow,
  Grid,
  Icon,
  Input,
  SectionLabel,
  SegmentedControl,
  Stack,
  Text,
  ToggleRow,
} from '@runkora/hades';
import {
  VALID,
  deriveFieldKey,
  newField,
  previewBody,
  resolvedFields,
  validateNamedPromptStep,
} from '../wizard-state';
import { PromptField, TriggerNameField } from '../shared-fields';
import { manualCatalog, toTemplateCatalog } from '../field-catalog';
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
  return `POST /api/v1/trigger-fires/${triggerId}`;
}

function StepInputs({ state, patch }: StepComponentProps) {
  const setField = (id: string, next: Partial<WizardField>) =>
    patch({ fields: state.fields.map((f) => (f.id === id ? { ...f, ...next } : f)) });

  const preview = resolvedFields(state.fields);

  return (
    <Stack gap="relaxed">
      <TriggerNameField
        value={state.name}
        onChange={(name) => patch({ name })}
        hint="Shown in the triggers list and to anyone who can fire it."
        placeholder="run-analysis"
      />

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
                  <Stack direction="horizontal" align="center" gap="cozy" justify="between" wrap>
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

      <Grid cols={{ base: 1, lg: 2 }} gap="comfortable">
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

      <PromptField
        value={state.promptTemplate}
        onChange={(next) => patch({ promptTemplate: next })}
        hint="What the run starts with. The declared inputs fill the fields."
        catalog={toTemplateCatalog(manualCatalog(state.fields))}
        catalogLabel="Your declared inputs; add fields above to extend this list."
        placeholder="Answer {{ var.question }} using the {{ var.dataset }} dataset."
      />
    </Stack>
  );
}

function validateInputs(state: WizardState) {
  return validateNamedPromptStep(state, (s) => {
    if (!s.fields.some((f) => f.label.trim()))
      return { valid: false, hint: 'Add at least one field with a label.' };
    return VALID;
  });
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
  apiKind: 'manual',
  icon: 'play',
  title: 'Manual',
  description: 'A form in the console, and the same schema as a REST endpoint.',
  example: 'POST /api/v1/trigger-fires/{id}',
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
