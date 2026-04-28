import type { Preview } from '@storybook/react';
import '../src/packages/terra/src/tokens/tokens.css';

const preview: Preview = {
  parameters: {
    backgrounds: {
      default: 'orbit',
      values: [
        { name: 'orbit', value: '#070a0f' },
        { name: 'surface', value: '#0d1117' },
        { name: 'light', value: '#ffffff' },
      ],
    },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /date$/i,
      },
    },
    options: {
      storySort: {
        order: [
          'Terra',
          ['Overview', 'Foundation', ['*', 'Typography'], 'Layout', 'Atoms', 'Molecules'],
          'App',
          ['Shared'],
        ],
      },
    },
  },
};

export default preview;
