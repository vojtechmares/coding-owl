import type { APIRoute } from 'astro';
import { getCollection } from 'astro:content';
import { docMarkdown, markdownResponse } from '../../../lib/markdown';

export async function getStaticPaths() {
  const docs = await getCollection('docs');
  return docs.map((doc) => ({ params: { slug: doc.id }, props: { doc } }));
}

export const GET: APIRoute = ({ props }) => markdownResponse(docMarkdown(props.doc));
