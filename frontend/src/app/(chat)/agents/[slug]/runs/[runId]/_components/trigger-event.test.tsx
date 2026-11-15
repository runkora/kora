import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { TriggerEventView, type TriggerEvent } from './trigger-event';

const event = (url: string): TriggerEvent => ({
  actor: { name: 'dev-reviewer', kind: 'person' },
  verb: 'commented on',
  object: { label: 'PR #6', url },
  trigger: 'mention',
  time: '09:14',
  prompt: 'Do it.',
});

describe('TriggerEventView', () => {
  it('links an https object', () => {
    render(<TriggerEventView e={event('https://github.com/o/r/pull/6')} />);
    expect(screen.getByRole('link', { name: 'PR #6' }).getAttribute('href')).toBe(
      'https://github.com/o/r/pull/6',
    );
  });

  it('renders a javascript: object URL as plain text', () => {
    render(<TriggerEventView e={event('javascript:alert(1)')} />);
    expect(screen.queryByRole('link')).toBeNull();
    expect(screen.getByText('PR #6')).toBeTruthy();
  });
});
