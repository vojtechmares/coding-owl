import type { APIRoute } from 'astro';
import { changelogMarkdown, markdownResponse } from '../../lib/markdown';

export const GET: APIRoute = () => markdownResponse(changelogMarkdown());
