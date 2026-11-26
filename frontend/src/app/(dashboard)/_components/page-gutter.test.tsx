import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Text } from '@runkora/hades';
import { PageGutter } from './page-gutter';

describe('PageGutter', () => {
  it('pads page content once with the standard gutter', () => {
    render(
      <PageGutter>
        <Text>Page</Text>
      </PageGutter>,
    );
    const wrapper = screen.getByText('Page').parentElement;
    expect(wrapper).toHaveClass('p-4');
    expect(wrapper?.parentElement?.querySelectorAll('.p-4')).toHaveLength(1);
  });
});
