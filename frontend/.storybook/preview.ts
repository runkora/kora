import type { Decorator, Preview } from '@storybook/react';
import '../src/packages/terra/src/tokens/tokens.css';
import './canvas.css';
import './docs.css';

// Sets the selected brand and theme on <html>, where Terra's token selectors apply.
const withBrand: Decorator = (Story, context) => {
  const { brand, theme } = context.globals;
  const root = document.documentElement;
  root.dataset.brand = brand;
  root.dataset.theme = theme;
  return Story();
};

const preview: Preview = {
  decorators: [withBrand],
  globalTypes: {
    brand: {
      description: 'Brand candidate',
      toolbar: {
        title: 'Brand',
        icon: 'paintbrush',
        items: [{ value: 'kora', title: 'Kora' }],
        dynamicTitle: true,
      },
    },
    theme: {
      description: 'Colour scheme',
      toolbar: {
        title: 'Theme',
        icon: 'circlehalf',
        items: [
          { value: 'dark', title: 'Dark' },
          { value: 'light', title: 'Light' },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: { brand: 'kora', theme: 'dark' },
  parameters: {
    // The canvas uses Terra's background colour, so it changes with the brand and theme.
    backgrounds: { disabled: true },
    layout: 'padded',
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /date$/i,
      },
    },
    options: {
      storySort: {
        order: [
          'Kora',
          ['Brand', '*'],
          'Terra',
          [
            'Overview',
            'Foundation',
            ['Colour', 'Typography', 'Radius', 'Spacing', 'Elevation', 'Contrast'],
            'Layout',
            ['Stack', 'Grid', 'Section', 'SidebarLayout', 'PageLayout', 'Navigation', 'Show'],
            'Atoms',
            'Molecules',
            'Organisms',
          ],
          'App',
          ['Atoms', 'Molecules', 'Organisms', 'Runs'],
        ],
      },
    },
  },
};

export default preview;
