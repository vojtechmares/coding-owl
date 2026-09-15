/*
 * `/llms.txt`, after https://llmstxt.org/: a short index of the site for
 * language models. Every page links to its Markdown sibling (`…/index.md`),
 * the representation meant for them; the HTML lives at the same path
 * without `/index.md`.
 */
import type { APIRoute } from 'astro';
import { getCollection, getEntry } from 'astro:content';
import { REPO, SITE } from '../lib/markdown';

function md(pathname: string): string {
  return `${SITE}${pathname}/index.md`;
}

export const GET: APIRoute = async () => {
  const home = await getEntry('pages', 'home');
  if (!home) return new Response('Not found', { status: 404 });
  const docs = (await getCollection('docs')).sort((a, b) => a.data.order - b.data.order);
  const decisions = (await getCollection('decisions')).sort((a, b) => a.data.number - b.data.number);

  const lines = [
    `# ${home.data.title}`,
    '',
    `> ${home.data.description}`,
    '',
    `Every page on ${SITE} is also available as Markdown at \`<page>/index.md\`; the links below point there. The HTML page is the same path without \`/index.md\`.`,
    '',
    '## Docs',
    '',
    `- [Home](${md('')}): ${home.data.tagline}`,
    `- [Documentation](${md('/docs')}): the index of the docs below.`,
  ];
  for (const doc of docs) {
    lines.push(`- [${doc.data.title}](${md(`/docs/${doc.id}`)}): ${doc.data.description}`);
  }
  lines.push(
    `- [Decisions](${md('/docs/decisions')}): ${decisions.length} architecture decision records, one per decision.`,
    '',
    '## Changelog',
    '',
    `- [Changelog](${md('/changelog')}): every release, in the Keep a Changelog format.`,
    '',
    '## Decisions',
    '',
  );
  for (const d of decisions) {
    const num = String(d.data.number).padStart(4, '0');
    lines.push(`- [ADR-${num}: ${d.data.title}](${md(`/docs/decisions/${d.id}`)}): ${d.data.status}, ${d.data.date}`);
  }
  lines.push('', '## Optional', '', `- [Source on GitHub](${REPO}): the repository, issues and releases.`);

  return new Response(lines.join('\n') + '\n', {
    status: 200,
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
};
