import { defineCollection } from 'astro:content';
import { z } from 'astro/zod';
import { glob } from 'astro/loaders';
import { adrs, repoFiles } from './loaders/repo';

/** Hand-written pages, one Markdown file each. */
const pages = defineCollection({
  loader: glob({ pattern: '*.md', base: './src/content/pages' }),
  schema: z.object({
    title: z.string(),
    eyebrow: z.string(),
    tagline: z.string(),
    description: z.string(),
    install: z.string(),
    features: z.array(z.object({ label: z.string(), title: z.string(), text: z.string() })),
  }),
});

/** Documentation lifted from the repository at build time. */
const docs = defineCollection({
  loader: repoFiles([
    {
      id: 'guide',
      path: 'README.md',
      title: 'Guide',
      description: 'How Owl works, installing it, a quick start, configuration and the command overview.',
      order: 1,
    },
    {
      id: 'language',
      path: 'CONTEXT.md',
      title: 'Language',
      description: 'The words Coding Owl uses, and the ones it avoids.',
      order: 2,
    },
  ]),
  schema: z.object({
    title: z.string(),
    description: z.string(),
    order: z.number(),
    source: z.string(),
  }),
});

/** Architecture decision records, one per file in docs/adr. */
const decisions = defineCollection({
  loader: adrs({ dir: 'docs/adr', base: '/docs/decisions' }),
  schema: z.object({
    number: z.number(),
    title: z.string(),
    status: z.string(),
    date: z.string(),
    supersedes: z.string().optional(),
    amended: z.string().optional(),
    source: z.string(),
  }),
});

export const collections = { pages, docs, decisions };
