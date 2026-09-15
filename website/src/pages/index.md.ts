import type { APIRoute } from 'astro';
import { getEntry } from 'astro:content';
import { homeMarkdown, markdownResponse } from '../lib/markdown';

export const GET: APIRoute = async () => {
  const page = await getEntry('pages', 'home');
  if (!page) return new Response('Not found', { status: 404 });
  return markdownResponse(homeMarkdown(page));
};
