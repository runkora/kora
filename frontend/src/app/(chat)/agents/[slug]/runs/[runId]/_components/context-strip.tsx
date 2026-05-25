'use client';

/**
 * @tier 3 — run-chat-scoped.
 * 32px horizontal strip of file chips representing the agent's context window.
 */

import { Stack, Text, Button } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';

function getExt(path: string): string {
  const parts = path.split('.');
  return parts.length > 1 ? parts[parts.length - 1] : '';
}

function getFilename(path: string): string {
  return path.split('/').pop() ?? path;
}

function getDir(path: string): string {
  const parts = path.split('/');
  return parts.slice(0, -1).join('/');
}

export function ContextStrip() {
  const { contextFiles, views, openCanvas } = useRunContext();

  function handleFileClick(filePath: string) {
    // Find a view matching this path
    const filename = getFilename(filePath);
    const matchingView = Object.values(views).find(
      (v) => v.title === filename || (v.subtitle && filePath.startsWith(v.subtitle)),
    );
    if (matchingView) {
      openCanvas(matchingView.id);
    }
  }

  return (
    <Stack
      direction="horizontal"
      align="center"
      style={{
        gap: 7,
        padding: '5px 16px',
        background: 'var(--chat-surf)',
        borderBottom: '1px solid var(--chat-bds)',
        flexShrink: 0,
        overflowX: 'auto',
      }}
    >
      {/* "Context" label */}
      <Text
        as="span"
        style={{
          fontSize: 10.5,
          color: 'var(--chat-subtle)',
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
          flexShrink: 0,
        }}
      >
        Context
      </Text>

      {/* File chips */}
      {contextFiles.map((filePath) => {
        const ext = getExt(filePath);
        const filename = getFilename(filePath);
        const dir = getDir(filePath);

        return (
          <Button
            key={filePath}
            type="button"
            onClick={() => handleFileClick(filePath)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 4,
              padding: '2px 8px',
              background: 'var(--chat-surf2)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 20,
              fontSize: 11.5,
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              color: 'var(--chat-muted)',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
            }}
            title={filePath}
          >
            {ext && (
              <Text as="span" style={{ color: 'var(--chat-acc)', opacity: 0.75 }}>
                {ext}
              </Text>
            )}
            {dir && (
              <Text as="span" style={{ color: 'var(--chat-subtle)' }}>
                {dir}/
              </Text>
            )}
            <Text as="span" weight="heavy" style={{ color: 'var(--chat-fg)' }}>
              {filename}
            </Text>
          </Button>
        );
      })}

      {/* Add chip */}
      <Button
        type="button"
        style={{
          padding: '2px 8px',
          background: 'transparent',
          border: '1px dashed var(--chat-bds)',
          borderRadius: 20,
          fontSize: 11.5,
          color: 'var(--chat-subtle)',
          cursor: 'pointer',
          whiteSpace: 'nowrap',
        }}
      >
        + add file
      </Button>
    </Stack>
  );
}
