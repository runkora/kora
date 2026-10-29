'use client';

/**
 * @tier 3 — triggers-scoped. What the shared trigger sections need to be told
 * about where they are rendered.
 *
 * The New trigger wizard and a stored trigger's Configuration tab show the same
 * sections. They differ in one way that matters: some properties are chosen once
 * and then fixed (the source a trigger listens on, the agent it starts). A
 * section takes a `mode`, and a field that cannot change after creation renders
 * through `SectionField` so the edit surface shows it greyed out and labelled,
 * instead of leaving it out or letting it look editable.
 */

import type { ReactNode } from 'react';
import { Badge, SectionLabel, Stack, Text } from '@runkora/hades';

/** `create` is the wizard; `edit` is a stored trigger's Configuration tab. */
export type SectionMode = 'create' | 'edit';

/** When a field may be changed. */
export type FieldMutability = 'always' | 'create-only';

/** Whether a field with this rule is locked in this mode. */
export function isLocked(mode: SectionMode, mutability: FieldMutability): boolean {
  return mode === 'edit' && mutability === 'create-only';
}

interface SectionFieldProps {
  label: string;
  mutability?: FieldMutability;
  mode: SectionMode;
  /** One line under the label saying what the field is for. */
  help?: string;
  children: ReactNode;
}

/**
 * A labelled field of a shared section. A create-only field in `edit` mode gets
 * an `Read-only` badge and a note, and its content is expected to render
 * disabled (the caller reads `isLocked` for that).
 */
export function SectionField({
  label,
  mutability = 'always',
  mode,
  help,
  children,
}: SectionFieldProps) {
  const locked = isLocked(mode, mutability);
  return (
    <Stack gap="snug">
      <Stack gap="tight">
        <Stack direction="horizontal" gap="snug" align="center">
          <SectionLabel>{label}</SectionLabel>
          {locked && (
            <Badge variant="dim" size="sm" shape="pill">
              Read-only
            </Badge>
          )}
        </Stack>
        {help !== undefined && (
          <Text size="caption" color="subtle">
            {help}
          </Text>
        )}
      </Stack>
      {children}
    </Stack>
  );
}
