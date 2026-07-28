'use client';

/** @tier 3 — triggers-scoped. The strip of insertable delivery fields shown
 * under a prompt textarea. Clicking one writes its placeholder at the cursor,
 * so the tokens a person can use are visible rather than remembered. */

import { Badge, Button, Stack, Text } from '@orbit/terra';
import { insertAtCursor } from './field-catalog';
import type { FieldCatalogEntry } from './types';

export interface PromptFieldsProps {
  entries: FieldCatalogEntry[];
  /** Field families whose names come from the data, so cannot be listed. */
  patterns?: string[];
  /** Current prompt text. */
  value: string;
  onChange: (next: string) => void;
  /** The textarea being written into, so insertion lands at the cursor. */
  textareaRef: React.RefObject<HTMLTextAreaElement | null>;
}

export function PromptFields({
  entries,
  patterns = [],
  value,
  onChange,
  textareaRef,
}: PromptFieldsProps) {
  if (entries.length === 0 && patterns.length === 0) return null;

  const insert = (token: string) => {
    const el = textareaRef.current;
    const cursor = el && document.activeElement === el ? el.selectionStart : null;
    const next = insertAtCursor(value, cursor, token);
    onChange(next);

    // Put the caret after what was just inserted, so a second click continues
    // where the first left off rather than jumping back.
    const caret = (cursor ?? value.length) + `{{ ${token} }}`.length;
    requestAnimationFrame(() => {
      if (!el) return;
      el.focus();
      el.setSelectionRange(caret, caret);
    });
  };

  return (
    <Stack gap="snug">
      <Text size="caption" color="subtle">
        Click to insert. Values are filled from the delivery when the run starts.
      </Text>
      <Stack direction="horizontal" gap="tight" wrap>
        {entries.map((entry) => (
          <Button
            key={entry.token}
            size="xs"
            shape="pill"
            variant="outline-muted"
            title={entry.description}
            aria-label={`Insert ${entry.token}: ${entry.description}`}
            onClick={() => insert(entry.token)}
          >
            <Text as="span" size="micro" mono>
              {entry.token}
            </Text>
          </Button>
        ))}
        {patterns.map((pattern) => (
          <Badge key={pattern} variant="outline" mono size="sm">
            {pattern}
          </Badge>
        ))}
      </Stack>
      {patterns.length > 0 && (
        <Text size="micro" color="subtle">
          Label and annotation names come from your Prometheus rules, so they cannot be listed.
          Write them out in full.
        </Text>
      )}
    </Stack>
  );
}
