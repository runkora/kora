import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { Button } from '@runkora/hades';
import { PermissionHint, permissionHintText } from './permission-hint';

describe('PermissionHint', () => {
  it('renders the control alone when allowed', () => {
    renderWithProviders(
      <PermissionHint allowed action="start runs of this agent">
        <Button>Run</Button>
      </PermissionHint>,
    );
    expect(screen.getByRole('button', { name: 'Run' })).toBeEnabled();
    expect(screen.queryByLabelText(permissionHintText('start runs of this agent'))).toBeNull();
  });

  it('labels a focusable wrapper with the missing permission when denied', () => {
    renderWithProviders(
      <PermissionHint allowed={false} action="start runs of this agent">
        <Button disabled>Run</Button>
      </PermissionHint>,
    );
    const wrapper = screen.getByLabelText("You don't have permission to start runs of this agent.");
    expect(wrapper).toHaveAttribute('tabindex', '0');
    expect(screen.getByRole('button', { name: 'Run' })).toBeDisabled();
  });
});
