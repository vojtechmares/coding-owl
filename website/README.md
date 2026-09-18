# codingowl.dev

The website: Astro with React islands and Tailwind, prerendered, served by a
Cloudflare Worker in front of Workers Assets.

Content is not copied into this directory. The homepage is
`src/content/pages/home.md`; everything else is read from the repository at
build time by the loaders in `src/loaders/repo.ts`:

| Page | Source |
| --- | --- |
| `/docs/getting-started` | `docs/guide/getting-started.md` |
| `/docs/jobs` | `docs/guide/jobs.md` |
| `/docs/projects-and-accounts` | `docs/guide/projects-and-accounts.md` |
| `/docs/guide` | `README.md` |
| `/docs/decisions/*` | `docs/adr/NNNN-*.md` |
| `/changelog` | `CHANGELOG.md`, parsed as Keep a Changelog |

A link between two of those files is written the way GitHub reads it, relative
to the file it is in. The loader resolves it against that file's directory and
looks the result up in its `PUBLISHED` map, so a page that has a URL here is
linked here and anything else falls through to GitHub.

Every page is also emitted as an `index.md` sibling, and `worker/index.ts`
serves whichever of the two the request's `Accept` header prefers, with
`Vary: Accept` set. `curl -H 'Accept: text/markdown' https://codingowl.dev/docs`
returns Markdown; a browser gets HTML.

The build also emits `/sitemap-index.xml` (with `/robots.txt` pointing at
it), and `/llms.txt` and `/llms-full.txt`, an index of the site and the
Markdown of every page in one file, for language models.

```
pnpm install
pnpm dev              # Astro alone, HTML only, on :4321
pnpm check            # astro check, and the Worker's types
pnpm build            # dist/
pnpm preview:worker   # build, then the Worker with wrangler dev on :8787
pnpm deploy           # build, then wrangler deploy
```

The Open Graph image, `public/og.png`, is committed rather than built -
`pnpm og` regenerates it from `scripts/og.mjs` after a change to the copy or
the logo.

The footer names the latest release, read at build time with `git describe`
from the newest `v*` tag reachable from the commit being built. A checkout
without tags says "unreleased" instead.

`.github/workflows/website.yml` builds on every pull request, push to `main`
and release tag, and deploys on the push to `main`, the tag, or a manual run
of the workflow (the break-glass option for a fix that cannot wait). It needs
the repository secret `CLOUDFLARE_API_TOKEN` and the repository variable
`CLOUDFLARE_ACCOUNT_ID`. The token needs Account: Workers Scripts: Edit, and on
the `codingowl.dev` zone, Workers Routes: Edit and DNS: Edit, for the custom
domain.
