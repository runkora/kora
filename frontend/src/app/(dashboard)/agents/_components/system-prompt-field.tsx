'use client';

/** @tier 3 — agents-scoped. System instructions editor shared by the agent builder and the
 * agent configuration tab. Promote to src/components/ if a second feature needs this. */

import { useState } from 'react';
import {
  Button,
  FormField,
  Icon,
  Modal,
  ModalFooter,
  ModalHeader,
  Stack,
  Textarea,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@orbit/terra';

// In the expanded editor the cap is high enough that the modal body, not the
// textarea, does the scrolling.
const EXPANDED_MAX_ROWS = 1000;

export interface SystemPromptFieldProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

/**
 * Fixed-height "System instructions" textarea with an expand button in its
 * corner that opens the same value in a large modal editor, for prompts too
 * long to work with inline. Both surfaces edit the value directly, so nothing
 * is lost if the modal is dismissed.
 */
export function SystemPromptField({ value, onChange, placeholder }: SystemPromptFieldProps) {
  const [expanded, setExpanded] = useState(false);

  return (
    <>
      <FormField label="System instructions" required>
        <Textarea
          value={value}
          onChange={(e) => onChange(e.target.value)}
          rows={5}
          placeholder={placeholder}
          action={
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Expand editor"
                  onClick={() => setExpanded(true)}
                >
                  <Icon name="maximize" size="2xs" color="muted" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Expand editor</TooltipContent>
            </Tooltip>
          }
        />
      </FormField>

      <Modal open={expanded} onClose={() => setExpanded(false)} size="builder">
        <ModalHeader
          title="System instructions"
          description="How the agent should behave. Changes apply to the form as you type."
        />
        {/* The body fills the panel and is the single scroller: the textarea
            grows with its content (effectively uncapped) instead of scrolling
            inside itself. */}
        <Stack grow overflowY="auto" padding="comfortable">
          <Textarea
            value={value}
            onChange={(e) => onChange(e.target.value)}
            rows={20}
            autoResize
            maxRows={EXPANDED_MAX_ROWS}
            placeholder={placeholder}
            aria-label="System instructions"
            aria-required
            autoFocus
          />
        </Stack>
        <ModalFooter>
          <Button size="sm" onClick={() => setExpanded(false)}>
            Done
          </Button>
        </ModalFooter>
      </Modal>
    </>
  );
}
