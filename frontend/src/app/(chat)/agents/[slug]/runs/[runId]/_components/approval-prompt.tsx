'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Inline approval card — approve, reject, or edit before the agent executes
 * a potentially impactful command.
 */

import { useState } from 'react';
import { Stack, Text, Button, Code } from '@orbit/terra';

interface ApprovalPromptProps {
  id: string;
  title: string;
  description: string;
  command: string;
}

type Resolution = 'approved' | 'rejected';

export function ApprovalPrompt({ title, description, command }: ApprovalPromptProps) {
  const [resolution, setResolution] = useState<Resolution | null>(null);

  if (resolution) {
    return (
      <Stack
        direction="horizontal"
        align="center"
        style={{
          margin: '10px 0',
          border: '1px solid var(--color-border-subtle)',
          borderRadius: 8,
          padding: '8px 12px',
          gap: 8,
        }}
      >
        <Text
          as="span"
          style={{
            fontSize: 12,
            color:
              resolution === 'approved'
                ? 'var(--color-status-healthy)'
                : 'var(--color-status-error)',
          }}
        >
          {resolution === 'approved' ? '✓' : '✗'}
        </Text>
        <Text as="span" style={{ fontSize: 14, color: 'var(--color-subtle)' }}>
          {resolution === 'approved' ? 'Approved' : 'Rejected'}: {title}
        </Text>
      </Stack>
    );
  }

  return (
    <Stack
      style={{
        margin: '10px 0',
        border: '1px solid color-mix(in oklch, var(--color-accent) 35%, transparent)',
        borderRadius: 8,
        background: 'color-mix(in oklch, var(--color-accent) 12%, transparent)',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <Stack
        direction="horizontal"
        align="center"
        style={{
          gap: 8,
          padding: '8px 12px',
          borderBottom: '1px solid color-mix(in oklch, var(--color-accent) 35%, transparent)',
        }}
      >
        <Text as="span" style={{ fontSize: 12, color: 'var(--color-accent)' }}>
          ◈
        </Text>
        <Text as="span" style={{ fontSize: 14, fontWeight: 500, color: 'var(--color-foreground)' }}>
          {title}
        </Text>
      </Stack>

      {/* Body */}
      <Stack style={{ padding: '9px 12px' }}>
        <Text
          style={{ fontSize: 14, lineHeight: 1.6, color: 'var(--color-muted)', marginBottom: 8 }}
        >
          {description}
        </Text>
        <Code
          style={{
            display: 'block',
            fontFamily: 'var(--font-mono)',
            fontSize: 12,
            color: 'var(--color-foreground)',
            background: 'var(--color-background)',
            border: '1px solid var(--color-border-subtle)',
            borderRadius: 4,
            padding: '6px 10px',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-all',
          }}
        >
          {command}
        </Code>
      </Stack>

      {/* Actions */}
      <Stack
        direction="horizontal"
        style={{
          gap: 7,
          padding: '9px 12px',
          borderTop: '1px solid color-mix(in oklch, var(--color-accent) 35%, transparent)',
        }}
      >
        <Button
          type="button"
          onClick={() => setResolution('approved')}
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'var(--color-accent)',
            border: '1px solid transparent',
            color: 'var(--color-accent-foreground)',
          }}
        >
          Approve
        </Button>
        <Button
          type="button"
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'transparent',
            border: '1px solid var(--color-border-subtle)',
            color: 'var(--color-muted)',
          }}
        >
          Edit
        </Button>
        <Button
          type="button"
          onClick={() => setResolution('rejected')}
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'transparent',
            border: '1px solid var(--color-status-error)',
            color: 'var(--color-status-error)',
          }}
        >
          Reject
        </Button>
      </Stack>
    </Stack>
  );
}
