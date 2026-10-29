'use client';

/** @tier 3 — triggers-scoped. The two fields every kind's first step renders: the
 * trigger name and the prompt. Each kind supplies only its own hint, placeholder
 * and catalog, so the field markup, ids and constraints exist once and a surface
 * cannot render them differently by accident. The trigger detail modal uses the
 * name field too; its Prompt section renders `TemplateField` directly, because
 * the rail section is already titled "Prompt" and a second label would repeat it. */

import { FormField, Input, TemplateField } from '@runkora/hades';
import type { TemplateCatalogEntry } from '@runkora/hades';

interface TriggerNameFieldProps {
  value: string;
  onChange: (name: string) => void;
  /** Where this name shows up, in the kind's own vocabulary. */
  hint: string;
  placeholder: string;
  disabled?: boolean;
  /** Marks the field as failing validation. */
  invalid?: boolean;
}

export function TriggerNameField({
  value,
  onChange,
  hint,
  placeholder,
  disabled,
  invalid,
}: TriggerNameFieldProps) {
  return (
    <FormField label="Trigger name" id="trigger-name" hint={hint} required>
      <Input
        id="trigger-name"
        mono
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        maxLength={128}
        filled={invalid ? 'danger' : undefined}
      />
    </FormField>
  );
}

interface PromptFieldProps {
  value: string;
  onChange: (prompt: string) => void;
  /** How this kind fills the prompt's fields at fire time. */
  hint: string;
  catalog: TemplateCatalogEntry[];
  /** Where the catalog's fields come from, shown above the token list. */
  catalogLabel?: string;
  placeholder: string;
  disabled?: boolean;
}

export function PromptField({
  value,
  onChange,
  hint,
  catalog,
  catalogLabel,
  placeholder,
  disabled,
}: PromptFieldProps) {
  return (
    <FormField label="Prompt" id="trigger-prompt" hint={hint} required>
      <TemplateField
        id="trigger-prompt"
        code
        aria-label="Prompt"
        value={value}
        onChange={onChange}
        catalog={catalog}
        catalogLabel={catalogLabel}
        placeholder={placeholder}
        disabled={disabled}
      />
    </FormField>
  );
}
