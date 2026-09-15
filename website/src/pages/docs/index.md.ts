import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';
import { docsIndexMarkdown, markdownResponse } from '../../lib/markdown';

export const GET: APIRoute = async () => {
  const docs = (await getCollection('docs')).sort((a, b) => a.data.order - b.data.order);
  const decisions = await getCollection('decisions');
  return markdownResponse(docsIndexMarkdown(docs, decisions));
};
