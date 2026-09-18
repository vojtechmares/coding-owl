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

/**
 * Documentation lifted from the repository at build time.
 *
 * `order` drives /docs and /llms.txt, and is numbered in tens so that a new
 * page slots in without renumbering the rest. Reserved: 10 and 11 for the
 * install pages (#127), 50 and 51 for the configuration pages (#124), 60 for
 * the Desktop app overview (#122), which links to the chat providers page. The
 * guide is last because it is the whole of the README on one page, no longer
 * the place to start.
 */
const docs = defineCollection({
  loader: repoFiles([
    {
      id: 'getting-started',
      path: 'docs/guide/getting-started.md',
      title: 'Getting started',
      description: 'Six steps from a fresh install to reviewing work Owl did while you were away.',
      order: 20,
    },
    {
      id: 'jobs',
      path: 'docs/guide/jobs.md',
      title: 'Working with Jobs',
      description: 'Queueing and ordering Jobs, following a Run, and deciding what to do with the work.',
      order: 30,
    },
    {
      id: 'projects-and-accounts',
      path: 'docs/guide/projects-and-accounts.md',
      title: 'Projects and Accounts',
      description: 'The repositories Jobs are queued against, the subscriptions they run on, and the instructions every Run reads.',
      order: 40,
    },
    {
      id: 'chat-providers',
      path: 'docs/guide/chat-providers.md',
      title: 'Chat model providers',
      description: 'Where the desktop chat gets its model: Anthropic, OpenRouter, and how a key is configured.',
      order: 61,
    },
    {
      id: 'guide',
      path: 'README.md',
      title: 'Guide',
      description: 'How Owl works, installing it, a quick start, configuration and the command overview.',
      order: 90,
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
