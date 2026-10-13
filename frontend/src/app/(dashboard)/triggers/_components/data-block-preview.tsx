'use client';

/**
 * @tier 3 — triggers-scoped. The shape of a webhook trigger's first message:
 * the prompt as written, then the keys its placeholders name in the untrusted
 * data block.
 */

import { CodeBlock, SectionLabel, Stack } from '@clear-route/hades';
import { dataBlockPreview } from './trigger-wizard/field-catalog';

interface DataBlockPreviewProps {
  template: string;
}

export function DataBlockPreview({ template }: DataBlockPreviewProps) {
  if (!template.trim()) return null;
  return (
    <Stack gap="tight">
      <SectionLabel>First message</SectionLabel>
      <CodeBlock text={dataBlockPreview(template)} wrap density="compact" />
    </Stack>
  );
}
