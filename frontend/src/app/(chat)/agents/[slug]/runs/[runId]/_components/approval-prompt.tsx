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
          border: '1px solid var(--chat-bds)',
          borderRadius: 8,
          padding: '8px 12px',
          fontSize: 12,
          color: 'var(--chat-subtle)',
          gap: 8,
        }}
      >
        <Text
          as="span"
          style={{ color: resolution === 'approved' ? 'var(--chat-healthy)' : 'var(--chat-error)' }}
        >
          {resolution === 'approved' ? '✓' : '✗'}
        </Text>
        {resolution === 'approved' ? 'Approved' : 'Rejected'}: {title}
      </Stack>
    );
  }

  return (
    <Stack
      style={{
        margin: '10px 0',
        border: '1px solid var(--chat-acc-bdr)',
        borderRadius: 8,
        background: 'var(--chat-acc-dim)',
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
          borderBottom: '1px solid var(--chat-acc-bdr)',
          fontSize: 12.5,
          fontWeight: 500,
        }}
      >
        <Text as="span" style={{ color: 'var(--chat-acc)' }}>
          ◈
        </Text>
        {title}
      </Stack>

      {/* Body */}
      <Stack style={{ padding: '9px 12px', fontSize: 12, color: 'var(--chat-muted)' }}>
        <Text style={{ marginBottom: 8 }}>{description}</Text>
        <Code
          style={{
            display: 'block',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 11.5,
            color: 'var(--chat-fg)',
            background: 'var(--chat-deep)',
            border: '1px solid var(--chat-bds)',
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
          borderTop: '1px solid var(--chat-acc-bdr)',
        }}
      >
        {/* Approve */}
        <Button
          type="button"
          onClick={() => setResolution('approved')}
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'var(--chat-acc)',
            border: '1px solid var(--chat-acc)',
            color: 'oklch(9% 0.005 248)',
          }}
        >
          Approve
        </Button>

        {/* Edit (stub — no action in mock) */}
        <Button
          type="button"
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'transparent',
            border: '1px solid var(--chat-bds)',
            color: 'var(--chat-muted)',
          }}
        >
          Edit
        </Button>

        {/* Reject */}
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
            border: '1px solid oklch(38% 0.15 25)',
            color: 'var(--chat-error)',
          }}
        >
          Reject
        </Button>
      </Stack>
    </Stack>
  );
}
