import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';
import { decisionMarkdown, markdownResponse } from '../../../../lib/markdown';

export async function getStaticPaths() {
  const decisions = await getCollection('decisions');
  return decisions.map((d) => ({ params: { slug: d.id }, props: { d } }));
}

export const GET: APIRoute = ({ props }) => markdownResponse(decisionMarkdown(props.d));
