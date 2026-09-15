/*
 * Structured data for every page. `Base.astro` describes the page it is
 * rendering and this file turns that into one schema.org graph: the site and
 * its publisher on every page, a WebPage (or CollectionPage) for the page
 * itself, a TechArticle for docs and decisions, a BreadcrumbList for pages
 * that show a breadcrumb, and whatever extra nodes the page adds.
 */
import { REPO, SITE } from './markdown';

export const AUTHOR = 'Vojtěch Mareš';
export const AUTHOR_URL = 'https://github.com/vojtechmares';
export const OG_IMAGE = `${SITE}/og.png`;
export const OG_IMAGE_ALT = 'The Coding Owl logo next to the tagline: your machine works while you are away.';
/** The same sentence as the home page description, so the site node reads the same everywhere. */
export const SITE_DESCRIPTION =
  'Coding Owl runs coding agents on your machine while it is idle, so the work you queued up is waiting for review when you come back.';

const WEBSITE_ID = `${SITE}/#website`;
const PUBLISHER_ID = `${SITE}/#publisher`;

export type PageType = 'website' | 'article';

export interface Crumb {
  href?: string;
  label: string;
}

/** What a page tells the layout about itself. */
export interface PageSeo {
  /** Absolute URL without a trailing slash. */
  canonical: string;
  title: string;
  description: string;
  /** Absolute URL of the social image. */
  image: string;
  /** `article` adds a TechArticle node; `website` does not. */
  type: PageType;
  /** Index pages are a CollectionPage rather than a plain WebPage. */
  collection?: boolean;
  crumbs?: Crumb[];
  datePublished?: string;
  dateModified?: string;
  /** Extra nodes merged into the graph, such as the SoftwareApplication on the home page. */
  extra?: Record<string, unknown>[];
}

/** A site-relative or absolute href as an absolute URL without a trailing slash. */
export function absolute(href: string): string {
  const url = href.startsWith('http') ? href : `${SITE}${href.startsWith('/') ? href : `/${href}`}`;
  return url === `${SITE}/` ? SITE : url.replace(/\/+$/, '');
}

const website = () => ({
  '@type': 'WebSite',
  '@id': WEBSITE_ID,
  name: 'Coding Owl',
  url: SITE,
  description: SITE_DESCRIPTION,
  inLanguage: 'en',
  publisher: { '@id': PUBLISHER_ID },
});

const publisher = () => ({
  '@type': 'Person',
  '@id': PUBLISHER_ID,
  name: AUTHOR,
  url: AUTHOR_URL,
});

const image = (page: PageSeo) => ({
  '@type': 'ImageObject',
  '@id': `${page.canonical}/#primaryimage`,
  url: page.image,
  contentUrl: page.image,
  width: 1200,
  height: 630,
  caption: OG_IMAGE_ALT,
});

const webPage = (page: PageSeo) => ({
  '@type': page.collection ? 'CollectionPage' : 'WebPage',
  '@id': `${page.canonical}/#webpage`,
  url: page.canonical,
  name: page.title,
  description: page.description,
  isPartOf: { '@id': WEBSITE_ID },
  inLanguage: 'en',
  primaryImageOfPage: { '@id': `${page.canonical}/#primaryimage` },
  ...(page.crumbs?.length ? { breadcrumb: { '@id': `${page.canonical}/#breadcrumb` } } : {}),
});

const article = (page: PageSeo) => ({
  '@type': 'TechArticle',
  '@id': `${page.canonical}/#article`,
  headline: page.title,
  description: page.description,
  url: page.canonical,
  inLanguage: 'en',
  image: { '@id': `${page.canonical}/#primaryimage` },
  author: { '@id': PUBLISHER_ID },
  publisher: { '@id': PUBLISHER_ID },
  isPartOf: { '@id': `${page.canonical}/#webpage` },
  mainEntityOfPage: { '@id': `${page.canonical}/#webpage` },
  ...(page.datePublished ? { datePublished: page.datePublished } : {}),
  ...(page.dateModified ?? page.datePublished
    ? { dateModified: page.dateModified ?? page.datePublished }
    : {}),
});

/** The breadcrumb as shown, with the home page in front and the current page last. */
const breadcrumbList = (page: PageSeo, crumbs: Crumb[]) => {
  const items = [{ label: 'Coding Owl', href: '/' }, ...crumbs];
  return {
    '@type': 'BreadcrumbList',
    '@id': `${page.canonical}/#breadcrumb`,
    itemListElement: items.map((c, i) => ({
      '@type': 'ListItem',
      position: i + 1,
      name: c.label,
      item: c.href ? absolute(c.href) : page.canonical,
    })),
  };
};

/**
 * The application itself, for the home page. Everything here comes from the
 * repository README: a free Homebrew formula for macOS, released on GitHub.
 */
export function softwareApplication(description: string): Record<string, unknown> {
  return {
    '@type': 'SoftwareApplication',
    '@id': `${SITE}/#software`,
    name: 'Coding Owl',
    url: SITE,
    description,
    applicationCategory: 'DeveloperApplication',
    operatingSystem: 'macOS',
    downloadUrl: `${REPO}/releases`,
    installUrl: `${REPO}/releases`,
    softwareHelp: { '@type': 'CreativeWork', url: `${SITE}/docs` },
    isAccessibleForFree: true,
    offers: { '@type': 'Offer', price: 0, priceCurrency: 'USD', url: SITE },
    author: { '@id': PUBLISHER_ID },
    image: OG_IMAGE,
  };
}

/** The whole graph for one page. */
export function schemaGraph(page: PageSeo): Record<string, unknown> {
  const graph: Record<string, unknown>[] = [website(), publisher(), image(page), webPage(page)];
  if (page.type === 'article') graph.push(article(page));
  if (page.crumbs?.length) graph.push(breadcrumbList(page, page.crumbs));
  graph.push(...(page.extra ?? []));
  return { '@context': 'https://schema.org', '@graph': graph };
}

/** JSON for a `<script type="application/ld+json">`; `<` is escaped so no content can close the tag. */
export function serialize(graph: Record<string, unknown>): string {
  return JSON.stringify(graph).replace(/</g, '\\u003c');
}
