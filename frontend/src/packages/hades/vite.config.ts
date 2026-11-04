import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

/**
 * Vite config for @runkora/hades Storybook development.
 *
 * Tailwind CSS v4 is processed via the official Vite plugin — no
 * tailwind.config.js required. The design tokens are declared in
 * src/tokens/tokens.css using @theme inline.
 */
export default defineConfig({
  plugins: [react(), tailwindcss()],
});
