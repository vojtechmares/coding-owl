/*
 * A parser for CHANGELOG.md in the Keep a Changelog format
 * (https://keepachangelog.com/en/1.1.0/): an H1, a preamble, then one H2 per
 * release - `## [Unreleased]` or `## [1.2.0] - 2026-01-31`, optionally marked
 * `[YANKED]` - each holding H3 sections named after the kind of change, each
 * a bullet list. Link references at the bottom give every release a URL.
 */
import source from '../../../CHANGELOG.md?raw';

export const CHANGE_TYPES = ['Added', 'Changed', 'Deprecated', 'Removed', 'Fixed', 'Security'] as const;
export type ChangeType = (typeof CHANGE_TYPES)[number];

export interface Section {
  type: string;
  /** Each item is a Markdown fragment, inline formatting included. */
  items: string[];
}

export interface Release {
  /** `Unreleased`, or a version such as `0.4.0`. */
  version: string;
  date?: string;
  yanked: boolean;
  url?: string;
  /** Free text between the release heading and its first section. */
  notes: string;
  sections: Section[];
}

export interface Changelog {
  title: string;
  /** Markdown between the H1 and the first release. */
  intro: string;
  releases: Release[];
  /** The file as written, for the Markdown representation of the page. */
  markdown: string;
}

const RELEASE = /^##\s+\[([^\]]+)\](?:\s*-\s*(\d{4}-\d{2}-\d{2}))?(\s*\[YANKED\])?\s*$/i;
const SECTION = /^###\s+(.+?)\s*$/;
const LINK_REF = /^\[([^\]]+)\]:\s*(\S+)\s*$/;
const ITEM = /^[-*]\s+(.*)$/;

export function parseChangelog(text: string): Changelog {
  const lines = text.replace(/\r\n/g, '\n').split('\n');
  const links = new Map<string, string>();
  for (const line of lines) {
    const m = line.match(LINK_REF);
    if (m) links.set(m[1].toLowerCase(), m[2]);
  }

  let title = 'Changelog';
  const intro: string[] = [];
  const releases: Release[] = [];
  let release: Release | undefined;
  let section: Section | undefined;

  for (const line of lines) {
    if (LINK_REF.test(line)) continue;

    const h1 = line.match(/^#\s+(.+?)\s*$/);
    if (h1 && !release) {
      title = h1[1];
      continue;
    }

    const rel = line.match(RELEASE);
    if (rel) {
      release = {
        version: rel[1],
        date: rel[2],
        yanked: Boolean(rel[3]),
        url: links.get(rel[1].toLowerCase()),
        notes: '',
        sections: [],
      };
      section = undefined;
      releases.push(release);
      continue;
    }

    if (!release) {
      intro.push(line);
      continue;
    }

    const sec = line.match(SECTION);
    if (sec) {
      section = { type: sec[1], items: [] };
      release.sections.push(section);
      continue;
    }

    if (!section) {
      release.notes += (release.notes ? '\n' : '') + line;
      continue;
    }

    const item = line.match(ITEM);
    if (item) {
      section.items.push(item[1]);
    } else if (/^\s+\S/.test(line) && section.items.length) {
      // A wrapped bullet continues the previous item.
      section.items[section.items.length - 1] += ' ' + line.trim();
    }
  }

  for (const r of releases) r.notes = r.notes.trim();

  return { title, intro: intro.join('\n').trim(), releases, markdown: text };
}

export const changelog: Changelog = parseChangelog(source);
