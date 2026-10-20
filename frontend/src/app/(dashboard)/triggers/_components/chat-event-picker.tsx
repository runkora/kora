'use client';

/**
 * @tier 3 · triggers-scoped. The event choice for a chat command trigger: one
 * card per chat event the API declares, with readable labels. Used by the
 * wizard and the drawer.
 */

import { Grid, RadioCard, RadioGroup, Text } from '@runkora/hades';
import { chatEventDescription, chatEventIcon, chatEventLabel } from '../_lib/chat';

interface ChatEventPickerProps {
  /** The chat events the source's catalog declares. */
  events: string[];
  value: string;
  onChange: (eventType: string) => void;
  disabled?: boolean;
}

export function ChatEventPicker({ events, value, onChange, disabled }: ChatEventPickerProps) {
  if (events.length === 0) {
    return (
      <Text size="caption" color="subtle">
        The events appear once the source or platform is known.
      </Text>
    );
  }
  return (
    <RadioGroup value={value} onValueChange={onChange} aria-label="Chat event">
      <Grid cols={2} gap="cozy">
        {events.map((eventType) => (
          <RadioCard
            key={eventType}
            value={eventType}
            selected={value === eventType}
            onSelect={onChange}
            icon={chatEventIcon(eventType)}
            title={chatEventLabel(eventType)}
            description={chatEventDescription(eventType)}
            disabled={disabled}
          />
        ))}
      </Grid>
    </RadioGroup>
  );
}
