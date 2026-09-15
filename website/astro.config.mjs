// @ts-check
import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import sitemap from '@astrojs/sitemap';
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
  integrations: [
    react(),
    // Only pages are listed: endpoints such as the `index.md` siblings never
    // reach the sitemap, and the 404 page is dropped. URLs carry no trailing
    // slash, matching `trailingSlash`. `lastmod` is the build time.
    sitemap({
      filter: (page) => new URL(page).pathname !== '/404',
      serialize: (item) => ({ ...item, lastmod: new Date().toISOString() }),
    }),
  ],
  vite: {
    plugins: [tailwindcss()],
    server: {
      // Docs, the changelog and the desktop screenshots are read from the
      // repository root, one level above this project.
      fs: { allow: ['..'] },
    },
  },
  markdown: {
    shikiConfig: { theme: 'github-light-default' },
  },
});
