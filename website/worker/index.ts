/*
 * The Worker in front of the static site. The build emits every page twice,
 * as `…/index.html` and `…/index.md`; this picks one by the request's Accept
 * header (RFC 9110 content negotiation) and hands everything else to Workers
 * Assets untouched.
 *
 * After https://acceptmarkdown.com/recipes/cloudflare-workers.md. With
 * `run_worker_first` set, every request comes through here.
 */

interface Env {
  ASSETS: Fetcher;
}

const HTML = 'text/html';
const MARKDOWN = 'text/markdown';
const PRODUCES = [HTML, MARKDOWN];

interface AcceptEntry {
  type: string;
  q: number;
  specificity: number;
}

function parseAccept(header: string): AcceptEntry[] {
  return header
    .split(',')
    .map((raw) => {
      const parts = raw.trim().split(';').map((s) => s.trim());
      const type = parts[0].toLowerCase();
      let q = 1;
      for (const param of parts.slice(1)) {
        const [name, value] = param.split('=').map((s) => s.trim());
        if (name === 'q') {
          const parsed = Number(value);
          if (!Number.isNaN(parsed)) q = Math.max(0, Math.min(1, parsed));
        }
      }
      const specificity = type === '*/*' ? 0 : type.endsWith('/*') ? 1 : 2;
      return { type, q, specificity };
    })
    .filter((e) => e.type.length > 0);
}

function matches(entry: AcceptEntry, candidate: string): boolean {
  if (entry.type === '*/*') return true;
  if (entry.type.endsWith('/*')) return candidate.startsWith(entry.type.slice(0, -1));
  return entry.type === candidate;
}

/**
 * The representation the client prefers, or null when it rejects both. No
 * header, or one that matches nothing, means HTML.
 */
function preferredType(header: string | null): string | null {
  if (!header) return HTML;
  const entries = parseAccept(header);
  if (entries.length === 0) return HTML;

  let best: string | null = null;
  let bestQ = -1;
  let bestPosition = Infinity;
  let anyMatch = false;

  for (const candidate of PRODUCES) {
    let matched: AcceptEntry | null = null;
    let matchedPosition = Infinity;
    for (let idx = 0; idx < entries.length; idx++) {
      const e = entries[idx];
      if (!matches(e, candidate)) continue;
      if (
        matched === null ||
        e.specificity > matched.specificity ||
        (e.specificity === matched.specificity && idx < matchedPosition)
      ) {
        matched = e;
        matchedPosition = idx;
      }
    }
    if (matched === null) continue;
    anyMatch = true;
    if (matched.q <= 0) continue;
    if (matched.q > bestQ || (matched.q === bestQ && matchedPosition < bestPosition)) {
      bestQ = matched.q;
      bestPosition = matchedPosition;
      best = candidate;
    }
  }

  return anyMatch ? best : HTML;
}

function appendVaryAccept(headers: Headers): void {
  const existing = headers.get('Vary');
  if (!existing) {
    headers.set('Vary', 'Accept');
    return;
  }
  const tokens = existing.split(',').map((s) => s.trim().toLowerCase());
  if (!tokens.includes('accept') && !tokens.includes('*')) {
    headers.set('Vary', `${existing}, Accept`);
  }
}

/** Requests for a file (`/favicon.svg`, `/_astro/x.css`) are not pages. */
function isDocument(pathname: string): boolean {
  const last = pathname.split('/').pop() ?? '';
  return !last.includes('.');
}

/** `/docs/installing` and `/docs/installing/` both live at `/docs/installing/index.md`. */
function markdownPath(pathname: string): string {
  return pathname.replace(/\/+$/, '') + '/index.md';
}

function withHeaders(response: Response, set: (headers: Headers) => void): Response {
  const out = new Response(response.body, response);
  set(out.headers);
  return out;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    if (!isDocument(url.pathname) || (request.method !== 'GET' && request.method !== 'HEAD')) {
      return env.ASSETS.fetch(request);
    }

    const chosen = preferredType(request.headers.get('Accept'));
    if (chosen === null) {
      return new Response('Not Acceptable: this resource is available as text/html or text/markdown.\n', {
        status: 406,
        headers: { 'Content-Type': 'text/plain; charset=utf-8', Vary: 'Accept' },
      });
    }

    const mdUrl = new URL(url);
    mdUrl.pathname = markdownPath(url.pathname);
    const alternate = `<${mdUrl.pathname}>; rel="alternate"; type="text/markdown"`;

    if (chosen === MARKDOWN) {
      const md = await env.ASSETS.fetch(new Request(mdUrl, request));
      if (md.ok) {
        return withHeaders(md, (h) => {
          h.set('Content-Type', 'text/markdown; charset=utf-8');
          h.set('X-Content-Type-Options', 'nosniff');
          h.set('Content-Location', mdUrl.pathname);
          appendVaryAccept(h);
        });
      }
      // No Markdown sibling; fall through to the HTML (a 404 page, usually).
    }

    const html = await env.ASSETS.fetch(request);
    return withHeaders(html, (h) => {
      appendVaryAccept(h);
      if (html.ok && (h.get('Content-Type') ?? '').startsWith(HTML)) {
        h.append('Link', alternate);
      }
    });
  },
} satisfies ExportedHandler<Env>;
