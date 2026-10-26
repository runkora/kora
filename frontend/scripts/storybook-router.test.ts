import { createElement } from 'react';
import { renderToString } from 'react-dom/server';
import { useRouter } from 'next/navigation';
import { describe, expect, it } from 'vitest';
import * as navigationMock from '@storybook/nextjs/navigation.mock';
import * as nextjsPreview from '@storybook/nextjs/preview';

// Renders a component that calls Next's useRouter inside the app router mock that
// @storybook/nextjs wraps around page stories. A Next bump that reads more of the
// router context than the mock supplies fails here, before it breaks Storybook.
function Probe() {
  return createElement('span', null, useRouter().bfcacheId);
}

describe('Storybook app router mock', () => {
  it('lets useRouter render in a page story', () => {
    (navigationMock as unknown as { createNavigation: (r: object) => void }).createNavigation({
      pathname: '/x',
    });
    const annotations = (nextjsPreview as { decorators?: unknown[] }).decorators ?? [];
    const decorate = annotations.find(
      (d) => (d as { name: string }).name === 'RouterDecorator',
    ) as (s: () => unknown, c: unknown) => never;
    expect(decorate, 'RouterDecorator not found in @storybook/nextjs/preview').toBeDefined();
    const html = renderToString(
      decorate(() => createElement(Probe), {
        parameters: { nextjs: { appDirectory: true, navigation: { pathname: '/x' } } },
      }),
    );
    expect(html).toContain('_b_');
  });
});
