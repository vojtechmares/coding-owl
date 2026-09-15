/*
 * Loaders that read Markdown straight out of the repository, one level above
 * this project, so the website never holds a copy of what the repo already
 * says. Each entry is rendered here, at load time, with Astro's own Markdown
 * pipeline, so pages can `render()` it like any other collection entry.
 */
import { readdir, readFile } from 'node:fs/promises';
import path from 'node:path';
import type { Loader } from 'astro/loaders';

export const REPO_ROOT = path.resolve(process.cwd(), '..');

/**
 * Splits a leading `# Title` off a Markdown document, after dropping any raw
 * HTML block ahead of it (the README's centred logo).
 */
function splitTitle(source: string): { title: string | undefined; body: string } {
  const text = source.replace(/^\s*<p\b[\s\S]*?<\/p>\s*/, '');
  const match = text.match(/^\s*#\s+(.+?)\s*\n([\s\S]*)$/);
  if (!match) return { title: undefined, body: text.trim() };
  return { title: match[1], body: match[2].trim() };
}

/**
 * Links to other files in the repository point at the published page when
 * there is one, and at GitHub otherwise, so nothing dangles.
 */
const REPO_URL = 'https://github.com/vojtechmares/coding-owl/blob/main';
const PUBLISHED: Record<string, string> = {
  'CONTEXT.md': '/docs/language',
  'CHANGELOG.md': '/changelog',
  'docs/adr/README.md': '/docs/decisions',
  'docs/adr': '/docs/decisions',
};

function rewriteRepoLinks(body: string): string {
  return body.replace(/\]\(((?![a-z]+:|\/|#)[^)\s]+?)(#[^)]*)?\)/g, (_, target: string, hash = '') => {
    const clean = target.replace(/^\.\//, '').replace(/\/$/, '');
    const adr = clean.match(/^docs\/adr\/(\d{4}-[a-z0-9-]+)\.md$/);
    if (adr) return `](/docs/decisions/${adr[1]}${hash})`;
    if (PUBLISHED[clean]) return `](${PUBLISHED[clean]}${hash})`;
    return `](${REPO_URL}/${clean}${hash})`;
  });
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

export interface RepoFile {
  /** The entry id, which becomes the URL slug. */
  id: string;
  /** Path relative to the repository root. */
  path: string;
  /** Overrides the file's own H1. */
  title?: string;
  description: string;
  order: number;
}

/** One entry per listed file. */
export function repoFiles(files: RepoFile[]): Loader {
  return {
    name: 'repo-files',
    async load({ store, renderMarkdown, watcher, generateDigest, logger }) {
      store.clear();
      for (const file of files) {
        const abs = path.join(REPO_ROOT, file.path);
        watcher?.add(abs);
        const source = await readFile(abs, 'utf8');
        const { title, body: rest } = splitTitle(source);
        const data = {
          title: file.title ?? title ?? file.id,
          description: file.description,
          order: file.order,
          source: file.path,
        };
        // A document whose first section is named like the page would show
        // the same heading twice; the page's own H1 stands in for it.
        const body = rewriteRepoLinks(
          rest.replace(new RegExp(`^##\\s+${escapeRegExp(data.title)}\\s*\\n+`), ''),
        );
        store.set({
          id: file.id,
          data,
          body,
          filePath: path.relative(process.cwd(), abs),
          digest: generateDigest(source),
          rendered: await renderMarkdown(body),
        });
        logger.debug(`loaded ${file.path} as ${file.id}`);
      }
    },
  };
}

interface AdrMeta {
  number?: number;
  title: string;
  status: string;
  date: string;
  supersedes?: string;
  amended?: string;
}

export interface AdrData extends Omit<AdrMeta, 'number'> {
  number: number;
  source: string;
}

const ADR_FILE = /^(\d{4})-[a-z0-9-]+\.md$/;
const META_LINE = /^- \*\*(Status|Date|Supersedes|Amended):\*\*\s*(.*)$/;

/**
 * Parses the header of an ADR written to the repository's own format
 * (docs/adr/README.md): an H1 of `ADR-NNNN: Title`, then a bullet list of
 * bold-labelled metadata, then the sections.
 */
function parseAdr(source: string): { meta: AdrMeta; body: string } {
  const { title: heading, body: rest } = splitTitle(source);
  const h1 = heading?.match(/^ADR-(\d{4}):\s*(.+)$/);
  const meta: Record<string, string> = {};
  const lines = rest.split('\n');
  let i = 0;
  // Metadata is the bullet list directly under the title; continuation lines
  // are indented.
  while (i < lines.length) {
    const line = lines[i];
    const m = line.match(META_LINE);
    if (m) {
      meta[m[1].toLowerCase()] = m[2].trim();
      i++;
      while (i < lines.length && /^\s{2,}\S/.test(lines[i])) {
        meta[m[1].toLowerCase()] += ' ' + lines[i].trim();
        i++;
      }
      continue;
    }
    if (line.trim() === '' && Object.keys(meta).length === 0) {
      i++;
      continue;
    }
    break;
  }
  return {
    meta: {
      number: h1 ? Number(h1[1]) : undefined,
      title: h1 ? h1[2] : (heading ?? 'Untitled'),
      status: meta.status ?? 'Unknown',
      date: meta.date ?? '',
      supersedes: meta.supersedes,
      amended: meta.amended,
    },
    body: lines.slice(i).join('\n').trim(),
  };
}

/**
 * Turns `ADR-NNNN` mentions and `](NNNN-title.md)` links into links to the
 * published decision, leaving fenced code alone.
 */
function linkAdrs(body: string, slugs: Map<string, string>, base: string): string {
  const out: string[] = [];
  let fenced = false;
  for (const line of body.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) {
      fenced = !fenced;
      out.push(line);
      continue;
    }
    if (fenced) {
      out.push(line);
      continue;
    }
    let next = line.replace(/\]\(((\d{4})-[a-z0-9-]+)\.md\)/g, (_, slug) => `](${base}/${slug})`);
    // Plain mentions, but not ones already inside a link text or inline code.
    next = next.replace(/(^|[^\[`\w/-])ADR-(\d{4})(?![\w-])/g, (whole, lead, num) => {
      const slug = slugs.get(num);
      return slug ? `${lead}[ADR-${num}](${base}/${slug})` : whole;
    });
    out.push(next);
  }
  return out.join('\n');
}

/** One entry per `NNNN-title.md` in the ADR directory. */
export function adrs(options: { dir: string; base: string }): Loader {
  return {
    name: 'repo-adrs',
    async load({ store, renderMarkdown, watcher, generateDigest, logger }) {
      const dir = path.join(REPO_ROOT, options.dir);
      watcher?.add(dir);
      const names = (await readdir(dir)).filter((n) => ADR_FILE.test(n)).sort();
      const slugs = new Map<string, string>(names.map((n) => [n.match(ADR_FILE)![1], n.replace(/\.md$/, '')]));
      store.clear();
      for (const name of names) {
        const abs = path.join(dir, name);
        const source = await readFile(abs, 'utf8');
        const { meta, body: raw } = parseAdr(source);
        const id = name.replace(/\.md$/, '');
        const number = meta.number ?? Number(name.match(ADR_FILE)![1]);
        const body = linkAdrs(raw, slugs, options.base);
        const data: AdrData = {
          number,
          title: meta.title,
          status: meta.status,
          date: meta.date,
          supersedes: meta.supersedes,
          amended: meta.amended,
          source: path.posix.join(options.dir, name),
        };
        store.set({
          id,
          data: { ...data },
          body,
          filePath: path.relative(process.cwd(), abs),
          digest: generateDigest(source),
          rendered: await renderMarkdown(body),
        });
      }
      logger.info(`loaded ${names.length} decisions from ${options.dir}`);
    },
  };
}
