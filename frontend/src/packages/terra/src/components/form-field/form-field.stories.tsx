import * as React from 'react';
import type { Meta, StoryObj } from '@storybook/react';
import { Input } from '../input/input';
import { Textarea } from '../textarea/textarea';
import { Button } from '../button/button';
import { FormField } from './form-field';

const meta: Meta<typeof FormField> = {
  title: 'Terra/Molecules/FormField',
  component: FormField,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#060709' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof FormField>;

// ── Stories ───────────────────────────────────────────────────────────────────

/**
 * Minimal usage — label wired to an input via `htmlFor ↔ id` automatically.
 * The `id` prop is passed once to `FormField`; the control receives it as its
 * own `id` attribute and the label's `htmlFor` is set implicitly.
 */
export const Default: Story = {
  render: () => (
    <div className="w-80">
      <FormField label="Agent name" id="agent-name">
        <Input id="agent-name" placeholder="e.g. apollo-3" />
      </FormField>
    </div>
  ),
};

/**
 * Supplementary hint text appears below the control when the field is in a
 * valid state. The hint is suppressed when an error is present so the two
 * never stack.
 */
export const WithHint: Story = {
  render: () => (
    <div className="w-80">
      <FormField
        label="Inference endpoint"
        id="endpoint"
        hint="Must be a fully-qualified HTTPS URL reachable from the control plane."
      >
        <Input id="endpoint" placeholder="https://api.example.com/v1" />
      </FormField>
    </div>
  ),
};

/**
 * When `error` is non-empty the error message replaces the hint and the input
 * receives `aria-invalid` via context. Pass `hasError` to the control so it
 * renders the red border state.
 */
export const WithError: Story = {
  render: () => (
    <div className="w-80">
      <FormField
        label="Model ID"
        id="model-id"
        hint="Fully-qualified model identifier."
        error="Model ID is required."
      >
        <Input id="model-id" hasError placeholder="bedrock/claude-sonnet-4-5" />
      </FormField>
    </div>
  ),
};

/**
 * Required fields render a red `*` next to the label and propagate
 * `aria-required` through context. The asterisk is `aria-hidden` so screen
 * readers rely on the label text and `aria-required` attribute instead.
 */
export const Required: Story = {
  render: () => (
    <div className="w-80">
      <FormField
        label="Principal ID"
        id="principal"
        hint="The agent identity this grant applies to."
        required
      >
        <Input id="principal" placeholder="agent:apollo-3" />
      </FormField>
    </div>
  ),
};

/**
 * All validation states together: default, hint, error, required. Use this
 * layout as a quick visual reference when reviewing token changes.
 */
export const AllStates: Story = {
  render: () => (
    <div className="flex flex-col gap-6 w-80">
      <FormField label="Default" id="state-default">
        <Input id="state-default" placeholder="No modifier" />
      </FormField>

      <FormField
        label="With hint"
        id="state-hint"
        hint="Hint text appears here when the field is valid."
      >
        <Input id="state-hint" placeholder="Value" />
      </FormField>

      <FormField
        label="With error"
        id="state-error"
        hint="This hint is hidden when error is set."
        error="This field contains an invalid value."
      >
        <Input id="state-error" hasError placeholder="Invalid value" />
      </FormField>

      <FormField label="Required" id="state-required" hint="This field is mandatory." required>
        <Input id="state-required" placeholder="Required value" />
      </FormField>

      <FormField
        label="Required with error"
        id="state-required-error"
        error="You must provide a value."
        required
      >
        <Input id="state-required-error" hasError placeholder="" />
      </FormField>
    </div>
  ),
};

/**
 * `FormField` accepts any input primitive as its child — here a `Textarea`.
 * The label, hint, and error wiring are identical regardless of the control
 * type.
 */
export const WithTextarea: Story = {
  render: () => (
    <div className="w-80">
      <FormField
        label="Policy description"
        id="policy-desc"
        hint="Briefly describe what this policy permits or restricts."
      >
        <Textarea id="policy-desc" placeholder="Allow inference requests from…" rows={4} />
      </FormField>
    </div>
  ),
};

/**
 * Inline validation — error state is set on submit if the field is empty.
 * This pattern shows the recommended pattern for lightweight form validation
 * without a form library.
 */
function InlineValidationDemo() {
  const [value, setValue] = React.useState('');
  const [error, setError] = React.useState<string | undefined>();
  const [submitted, setSubmitted] = React.useState(false);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!value.trim()) {
      setError('Token name is required.');
      return;
    }
    setError(undefined);
    setSubmitted(true);
  };

  if (submitted) {
    return (
      <div className="w-80 text-center">
        <p className="text-sm text-status-success">Token &quot;{value}&quot; created.</p>
        <Button
          variant="ghost"
          size="sm"
          className="mt-2"
          onClick={() => {
            setValue('');
            setError(undefined);
            setSubmitted(false);
          }}
        >
          Reset
        </Button>
      </div>
    );
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4 w-80">
      <FormField
        label="Token name"
        id="inline-token-name"
        hint="Use a descriptive name so you can identify this token later."
        error={error}
        required
      >
        <Input
          id="inline-token-name"
          value={value}
          hasError={!!error}
          placeholder="e.g. ci-prod"
          onChange={(e) => {
            setValue(e.target.value);
            if (error) setError(undefined);
          }}
        />
      </FormField>
      <Button type="submit" size="sm">
        Create token
      </Button>
    </form>
  );
}

export const InlineValidation: Story = {
  render: () => <InlineValidationDemo />,
};
