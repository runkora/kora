import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';

import { defaultTheme } from '@runkora/theme/default';
import { BuildConfigWizard } from './build-config-wizard';
import { GITHUB, SLACK } from './mcp-mock-data';

const SERVERS = [GITHUB, SLACK];

function open(onClose = vi.fn()) {
  render(<BuildConfigWizard servers={SERVERS} selected={['github', 'slack']} onClose={onClose} />);
  return onClose;
}

const next = () => fireEvent.click(screen.getByRole('button', { name: /Continue/ }));

describe('BuildConfigWizard', () => {
  it('starts on the selected servers step', () => {
    open();
    expect(screen.getByText('Selected servers')).toBeTruthy();
    expect(screen.getByText('GitHub')).toBeTruthy();
    expect(screen.getByText('Slack')).toBeTruthy();
    expect(document.body.textContent).toContain(
      `This config will connect 2 MCP servers to your client through the ${defaultTheme.productName} gateway.`,
    );
  });

  it('shows the servers and the client picker together', () => {
    open();
    expect(screen.getByText('Choose a client')).toBeTruthy();
    expect(screen.getByRole('button', { name: /Claude Code/ })).toBeTruthy();
  });

  it('continues to the config and switches output with the client', () => {
    open();
    fireEvent.click(screen.getByRole('button', { name: /Claude Code/ }));
    next();
    expect(screen.getByText('Your config')).toBeTruthy();
    expect(document.body.textContent).toContain('claude mcp add');
  });

  it('keeps the chosen client when going back and forward', () => {
    open();
    fireEvent.click(screen.getByRole('button', { name: /Claude Code/ }));
    next();
    fireEvent.click(screen.getAllByRole('button', { name: /Back/ })[0]!);
    expect(screen.getByRole('button', { name: /Claude Code/ }).getAttribute('aria-pressed')).toBe(
      'true',
    );
  });

  it('closes from the final step', () => {
    const onClose = open();
    next();
    fireEvent.click(screen.getByRole('button', { name: /Done/ }));
    expect(onClose).toHaveBeenCalled();
  });
});
