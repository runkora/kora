import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';

const searchUsersAction = vi.fn();
vi.mock('../_actions/grants', () => ({
  searchUsersAction: (...args: unknown[]) => searchUsersAction(...args),
}));

import { UserPicker } from './user-picker';

const MAYA = { id: 'usr_1', email: 'maya@example.com', display_name: 'Maya Chen' };
const DEV = { id: 'usr_2', email: 'dev@example.com', display_name: '' };

/** Lets the pending timers and the awaited action settle. */
const settle = () => act(() => vi.advanceTimersByTimeAsync(400));

describe('UserPicker', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    searchUsersAction.mockReset();
  });
  afterEach(() => vi.useRealTimers());

  it('lists users and reports the chosen one with its record', async () => {
    searchUsersAction.mockResolvedValue({ ok: true, data: [MAYA, DEV] });
    const onChange = vi.fn();
    render(<UserPicker value="" onChange={onChange} />);
    await settle();
    expect(searchUsersAction).toHaveBeenCalledWith('');
    fireEvent.click(screen.getByRole('radio', { name: /Maya Chen/ }));
    expect(onChange).toHaveBeenCalledWith('usr_1', MAYA);
  });

  it('waits for typing to pause before searching', async () => {
    searchUsersAction.mockResolvedValue({ ok: true, data: [MAYA] });
    render(<UserPicker value="" onChange={vi.fn()} />);
    await settle();
    searchUsersAction.mockClear();
    const input = screen.getByPlaceholderText('Search users…');
    fireEvent.change(input, { target: { value: 'ma' } });
    await act(() => vi.advanceTimersByTimeAsync(100));
    fireEvent.change(input, { target: { value: 'may' } });
    await act(() => vi.advanceTimersByTimeAsync(200));
    expect(searchUsersAction).not.toHaveBeenCalled();
    await act(() => vi.advanceTimersByTimeAsync(150));
    expect(searchUsersAction).toHaveBeenCalledTimes(1);
    expect(searchUsersAction).toHaveBeenCalledWith('may');
  });

  it('says so when nothing matches', async () => {
    searchUsersAction.mockResolvedValue({ ok: true, data: [] });
    render(<UserPicker value="" onChange={vi.fn()} />);
    await settle();
    fireEvent.change(screen.getByPlaceholderText('Search users…'), { target: { value: 'zz' } });
    await settle();
    expect(screen.getByText('No users match your search')).toBeInTheDocument();
  });

  it.each([
    [
      'fails',
      () => searchUsersAction.mockResolvedValue({ ok: false, status: 503, message: 'down' }),
    ],
    ['throws', () => searchUsersAction.mockRejectedValue(new Error('network'))],
  ])('shows an error when the search %s, then recovers', async (_name, fail) => {
    fail();
    render(<UserPicker value="" onChange={vi.fn()} />);
    await settle();
    expect(screen.getByText('Could not load users. Try searching again.')).toBeInTheDocument();
    expect(screen.queryByText('Loading…')).toBeNull();

    searchUsersAction.mockReset();
    searchUsersAction.mockResolvedValue({ ok: true, data: [MAYA] });
    fireEvent.change(screen.getByPlaceholderText('Search users…'), { target: { value: 'maya' } });
    await settle();
    expect(screen.queryByText('Could not load users. Try searching again.')).toBeNull();
    expect(screen.getByRole('radio', { name: /Maya Chen/ })).toBeInTheDocument();
  });
});
