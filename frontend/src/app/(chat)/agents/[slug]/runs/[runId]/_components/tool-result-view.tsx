'use client';

/**
 * @tier 3 — run-chat-scoped.
 * The full input and result of one tool call, formatted on demand: JSON
 * pretty-printed, diffs as diffs, long output folded with a way into the canvas.
 * Mounted only when a row is expanded, so nothing is formatted until asked for.
 */

import { useState } from 'react';
import { Button, CodeBlock, Stack, Text } from '@orbit/hades';
import type { ToolCall } from '../_types';
import { prettyJson } from '../_hooks/use-run';
import { describeTool } from '../_lib/tool-activity';
import { useRunContext } from '../_context/run-context';

const FOLD_LINES = 40;

interface DiffLine {
  kind: 'add' | 'del' | 'ctx';
  text: string;
}

/** Lines of a unified diff, or null when the text is not one. */
export function parseDiff(text: string): DiffLine[] | null {
  if (!/^(diff --git |--- |@@ )/m.test(text)) return null;
  return text.split('\n').map((line) => ({
    kind:
      line.startsWith('+') && !line.startsWith('+++')
        ? 'add'
        : line.startsWith('-') && !line.startsWith('---')
          ? 'del'
          : 'ctx',
    text: line,
  }));
}

/** An Edit call's change as diff lines. */
/** An edit's change as diff lines; Claude Code spells the fields old_string, OpenCode oldString. */
export function editDiff(input: Record<string, unknown>): DiffLine[] | null {
  const before = input.old_string ?? input.oldString;
  const after = input.new_string ?? input.newString;
  if (typeof before !== 'string' || typeof after !== 'string') return null;
  return [
    ...before.split('\n').map((text) => ({ kind: 'del' as const, text: `-${text}` })),
    ...after.split('\n').map((text) => ({ kind: 'add' as const, text: `+${text}` })),
  ];
}

const DIFF_COLOURS: Record<DiffLine['kind'], string> = {
  add: 'var(--color-status-healthy)',
  del: 'var(--color-status-error)',
  ctx: 'var(--color-muted)',
};

function DiffBlock({ lines }: { lines: DiffLine[] }) {
  return (
    <Stack
      data-diff=""
      style={{
        padding: '8px 10px',
        borderRadius: 6,
        border: '1px solid var(--color-border-subtle)',
        background: 'var(--color-surface)',
        overflowX: 'auto',
      }}
    >
      {lines.map((line, i) => (
        <Text
          key={i}
          as="span"
          mono
          style={{
            fontSize: 11,
            lineHeight: 1.6,
            whiteSpace: 'pre',
            color: DIFF_COLOURS[line.kind],
          }}
        >
          {line.text || ' '}
        </Text>
      ))}
    </Stack>
  );
}

function inputView(tool: ToolCall) {
  if (
    describeTool(tool.name, tool.input).kind === 'command' &&
    typeof tool.input.command === 'string'
  ) {
    return <CodeBlock text={tool.input.command} density="compact" wrap />;
  }
  const diff = editDiff(tool.input);
  if (diff) return <DiffBlock lines={diff} />;
  if (Object.keys(tool.input).length === 0) return null;
  return (
    <CodeBlock text={JSON.stringify(tool.input, null, 2)} density="compact" language="json" wrap />
  );
}

export function ToolResultView({ tool }: { tool: ToolCall }) {
  const { openToolInCanvas } = useRunContext();
  const [unfolded, setUnfolded] = useState(false);
  const json = prettyJson(tool.result);
  const text = json ?? tool.result;
  const diff = json ? null : parseDiff(tool.result);
  const lines = text.split('\n');
  const folded = !unfolded && lines.length > FOLD_LINES;
  const shown = folded ? lines.slice(0, FOLD_LINES).join('\n') : text;

  return (
    <Stack data-tool-result="" style={{ gap: 8, padding: '6px 0 10px 26px', minWidth: 0 }}>
      {inputView(tool)}
      {tool.status === 'running' ? (
        <Text size="supporting" color="muted">
          Waiting for the result…
        </Text>
      ) : !tool.result.trim() ? (
        <Text size="supporting" color="muted">
          No output.
        </Text>
      ) : diff ? (
        <DiffBlock lines={folded ? diff.slice(0, FOLD_LINES) : diff} />
      ) : (
        <CodeBlock text={shown} density="compact" language={json ? 'json' : undefined} wrap />
      )}
      {(folded || tool.truncated || lines.length > FOLD_LINES) && (
        <Stack direction="horizontal" align="center" style={{ gap: 12, flexWrap: 'wrap' }}>
          {folded && (
            <Button type="button" variant="ghost" size="sm" onClick={() => setUnfolded(true)}>
              Show all {lines.length} lines
            </Button>
          )}
          <Button type="button" variant="ghost" size="sm" onClick={() => openToolInCanvas(tool)}>
            Open in canvas
          </Button>
          {tool.truncated && (
            <Text size="supporting" color="muted">
              Recorded result cut at 64 KB of {Math.round((tool.originalBytes ?? 0) / 1024)} KB.
            </Text>
          )}
        </Stack>
      )}
    </Stack>
  );
}
