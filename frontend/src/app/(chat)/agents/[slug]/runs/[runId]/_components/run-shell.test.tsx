import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { RunShell } from './run-shell';

vi.mock('../_context/run-context', () => ({
  RunProvider: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock('./run-page', async () => {
  const { Text } = await import('@runkora/hades');
  return { RunPage: () => <Text>sessions pane</Text> };
});
vi.mock('@runkora/app/(chat)/chat/_components/chat-run', async () => {
  const { Text } = await import('@runkora/hades');
  return { ChatRunRoute: () => <Text>your runs pane</Text> };
});

describe('RunShell', () => {
  it('shows the viewer’s runs beside a run opened from the chat page', () => {
    render(<RunShell slug="coder" runId="run-1" fromChat />);
    expect(screen.getByText('your runs pane')).toBeInTheDocument();
  });

  it('shows the run’s sessions otherwise', () => {
    render(<RunShell slug="coder" runId="run-1" />);
    expect(screen.getByText('sessions pane')).toBeInTheDocument();
  });
});
