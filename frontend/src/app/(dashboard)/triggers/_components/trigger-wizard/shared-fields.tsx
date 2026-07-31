'use client';

/** @tier 3 — triggers-scoped. The two fields every kind's first step renders:
 * the trigger name and the prompt. Each kind supplies only its own hint,
 * placeholder and catalog, so the field markup, ids and constraints exist once
 * and a new kind cannot render them differently by accident. */

import { FormField, Input, TemplateField } from '@orbit/terra';
import type { TemplateCatalogEntry } from '@orbit/terra';

interface TriggerNameFieldProps {
  value: string;
  onChange: (name: string) => void;
  /** Where this name shows up, in the kind's own vocabulary. */
  hint: string;
  placeholder: string;
}

export function TriggerNameField({ value, onChange, hint, placeholder }: TriggerNameFieldProps) {
  return (
    <FormField label="Trigger name" id="wizard-name" hint={hint} required>
      <Input
        id="wizard-name"
        mono
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        maxLength={128}
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
  catalogLabel: string;
  placeholder: string;
}

export function PromptField({
  value,
  onChange,
  hint,
  catalog,
  catalogLabel,
  placeholder,
}: PromptFieldProps) {
  return (
    <FormField label="Prompt" id="wizard-prompt" hint={hint} required>
      <TemplateField
        id="wizard-prompt"
        aria-label="Prompt"
        value={value}
        onChange={onChange}
        catalog={catalog}
        catalogLabel={catalogLabel}
        placeholder={placeholder}
      />
    </FormField>
  );
}
