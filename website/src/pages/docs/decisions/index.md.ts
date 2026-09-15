import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';
import { decisionsIndexMarkdown, markdownResponse } from '../../../lib/markdown';

export const GET: APIRoute = async () => {
  const decisions = (await getCollection('decisions')).sort((a, b) => a.data.number - b.data.number);
  return markdownResponse(decisionsIndexMarkdown(decisions));
};
