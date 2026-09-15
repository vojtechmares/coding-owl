/*
 * `/llms-full.txt`: the Markdown of every page in one file, in the order a
 * reader would visit them, for language models that want the whole site at
 * once. Each section names the page it came from.
 */
import type { APIRoute } from 'astro';
import { getCollection, getEntry } from 'astro:content';
import {
  SITE,
  changelogMarkdown,
  decisionMarkdown,
  decisionsIndexMarkdown,
  docMarkdown,
  docsIndexMarkdown,
  homeMarkdown,
} from '../lib/markdown';

export const GET: APIRoute = async () => {
  const home = await getEntry('pages', 'home');
  if (!home) return new Response('Not found', { status: 404 });
  const docs = (await getCollection('docs')).sort((a, b) => a.data.order - b.data.order);
  const decisions = (await getCollection('decisions')).sort((a, b) => a.data.number - b.data.number);

  const sections: [string, string][] = [
    ['', homeMarkdown(home)],
    ['/docs', docsIndexMarkdown(docs, decisions)],
    ...docs.map((doc): [string, string] => [`/docs/${doc.id}`, docMarkdown(doc)]),
    ['/docs/decisions', decisionsIndexMarkdown(decisions)],
    ...decisions.map((d): [string, string] => [`/docs/decisions/${d.id}`, decisionMarkdown(d)]),
    ['/changelog', changelogMarkdown()],
  ];

  const body = sections
    .map(([pathname, markdown]) => `<!-- Source: ${SITE}${pathname}/index.md -->\n\n${markdown.trimEnd()}`)
    .join('\n\n---\n\n');

  return new Response(body + '\n', {
    status: 200,
    headers: { 'Content-Type': 'text/plain; charset=utf-8' },
  });
};
