// @ts-check
import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import tailwindcss from '@tailwindcss/vite';

// https://astro.build/config
export default defineConfig({
  site: 'https://codingowl.dev',
  // Every page is prerendered. A small Worker in front of the static output
  // (worker/index.ts) negotiates between the HTML and the Markdown sibling
  // each page emits, so nothing here runs at request time.
  output: 'static',
  build: { format: 'directory' },
  trailingSlash: 'never',
  integrations: [react()],
  vite: {
    plugins: [tailwindcss()],
    server: {
      // Docs, the changelog and the desktop screenshots are read from the
      // repository root, one level above this project.
      fs: { allow: ['..'] },
    },
  },
  markdown: {
    shikiConfig: { theme: 'github-dark-default' },
  },
});
