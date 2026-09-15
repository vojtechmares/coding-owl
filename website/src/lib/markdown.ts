/*
 * The Markdown representation of every page. Each HTML page has an
 * `index.md` sibling built from the same collection entry, and the Worker in
 * front of the site serves whichever the client's Accept header prefers.
 */
import type { CollectionEntry } from 'astro:content';
import { changelog } from './changelog';

export const SITE = 'https://codingowl.dev';
export const REPO = 'https://github.com/vojtechmares/coding-owl';

export function markdownResponse(body: string): Response {
  return new Response(body.trimEnd() + '\n', {
    status: 200,
    headers: {
      'Content-Type': 'text/markdown; charset=utf-8',
      Vary: 'Accept',
    },
  });
}

/** Where the Markdown sibling of a page lives. */
export function markdownPath(pathname: string): string {
  const clean = pathname.replace(/\/+$/, '');
  return `${clean}/index.md`;
}

export function homeMarkdown(page: CollectionEntry<'pages'>): string {
  const { title, tagline, install } = page.data;
  return [
    `# ${title}`,
    '',
    `> ${tagline}`,
    '',
    '```',
    install,
    '```',
    '',
    page.body ?? '',
    '',
    '## More',
    '',
    `- [Documentation](${SITE}/docs)`,
    `- [Changelog](${SITE}/changelog)`,
    `- [Source on GitHub](${REPO})`,
  ].join('\n');
}

export function docsIndexMarkdown(
  docs: CollectionEntry<'docs'>[],
  decisions: CollectionEntry<'decisions'>[],
): string {
  const lines = ['# Documentation', ''];
  for (const doc of docs) {
    lines.push(`- [${doc.data.title}](${SITE}/docs/${doc.id}): ${doc.data.description}`);
  }
  lines.push(
    `- [Decisions](${SITE}/docs/decisions): ${decisions.length} architecture decision records, one per decision.`,
  );
  return lines.join('\n');
}

export function docMarkdown(doc: CollectionEntry<'docs'>): string {
  return [
    `# ${doc.data.title}`,
    '',
    doc.body ?? '',
    '',
    '---',
    '',
    `Source: [${doc.data.source}](${REPO}/blob/main/${doc.data.source})`,
  ].join('\n');
}

export function decisionsIndexMarkdown(decisions: CollectionEntry<'decisions'>[]): string {
  const lines = [
    '# Architecture decision records',
    '',
    'One record per decision, numbered in the order decisions were accepted.',
    '',
    '| ADR | Title | Status | Date |',
    '| --- | --- | --- | --- |',
  ];
  for (const d of decisions) {
    const num = String(d.data.number).padStart(4, '0');
    lines.push(`| [${num}](${SITE}/docs/decisions/${d.id}) | ${d.data.title} | ${d.data.status} | ${d.data.date} |`);
  }
  return lines.join('\n');
}

export function decisionMarkdown(d: CollectionEntry<'decisions'>): string {
  const num = String(d.data.number).padStart(4, '0');
  const meta = [`- **Status:** ${d.data.status}`, `- **Date:** ${d.data.date}`];
  if (d.data.supersedes) meta.push(`- **Supersedes:** ${d.data.supersedes}`);
  if (d.data.amended) meta.push(`- **Amended:** ${d.data.amended}`);
  return [
    `# ADR-${num}: ${d.data.title}`,
    '',
    ...meta,
    '',
    // Relative decision links become absolute so the document stands alone.
    (d.body ?? '').replaceAll('](/docs/decisions/', `](${SITE}/docs/decisions/`),
    '',
    '---',
    '',
    `Source: [${d.data.source}](${REPO}/blob/main/${d.data.source})`,
  ].join('\n');
}

export function changelogMarkdown(): string {
  return changelog.markdown;
}
