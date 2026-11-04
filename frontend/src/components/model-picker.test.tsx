import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import type { ModelObject } from '@runkora/shared/api';

import { ModelPicker } from './model-picker';

const MODELS = [
  { id: 'm-1', display_name: 'One', family: 'anthropic', owned_by: 'bedrock' },
  { id: 'm-2', display_name: 'Two', family: 'anthropic', owned_by: 'bedrock' },
] as ModelObject[];

describe('ModelPicker read-only', () => {
  it('disables the radios and ignores clicks on a model card', () => {
    const onChange = vi.fn();
    render(<ModelPicker models={MODELS} value="m-1" onChange={onChange} disabled />);
    for (const radio of screen.getAllByRole('radio')) {
      expect(radio).toBeDisabled();
    }
    fireEvent.click(screen.getByText('Two'));
    expect(onChange).not.toHaveBeenCalled();
  });
});
