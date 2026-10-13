'use client';

/**
 * @tier 3 — run-chat-scoped.
 * 32px horizontal strip of file chips representing the agent's context window.
 */

import { Stack, Text, Button } from '@clear-route/hades';
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
        background: 'var(--color-surface)',
        borderBottom: '1px solid var(--color-border-subtle)',
        flexShrink: 0,
        overflowX: 'auto',
      }}
    >
      <Text
        as="span"
        style={{
          fontSize: 12,
          color: 'var(--color-subtle)',
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
          flexShrink: 0,
          fontFamily: 'var(--font-mono)',
        }}
      >
        Context
      </Text>

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
              background: 'var(--color-surface-elevated)',
              border: '1px solid var(--color-border-subtle)',
              borderRadius: 20,
              fontSize: 12,
              fontFamily: 'var(--font-mono)',
              color: 'var(--color-muted)',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
            }}
            title={filePath}
          >
            {ext && (
              <Text as="span" style={{ fontSize: 12, color: 'var(--color-accent)', opacity: 0.75 }}>
                {ext}
              </Text>
            )}
            {dir && (
              <Text as="span" style={{ fontSize: 12, color: 'var(--color-subtle)' }}>
                {dir}/
              </Text>
            )}
            <Text
              as="span"
              weight="heavy"
              style={{ fontSize: 12, color: 'var(--color-foreground)' }}
            >
              {filename}
            </Text>
          </Button>
        );
      })}

      <Button
        type="button"
        style={{
          padding: '2px 8px',
          background: 'transparent',
          border: '1px dashed var(--color-border-subtle)',
          borderRadius: 20,
          fontSize: 12,
          fontFamily: 'var(--font-mono)',
          color: 'var(--color-subtle)',
          cursor: 'pointer',
          whiteSpace: 'nowrap',
        }}
      >
        + add file
      </Button>
    </Stack>
  );
}
