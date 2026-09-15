# codingowl.dev

The website: Astro with React islands and Tailwind, prerendered, served by a
Cloudflare Worker in front of Workers Assets.

Content is not copied into this directory. The homepage is
`src/content/pages/home.md`; everything else is read from the repository at
build time by the loaders in `src/loaders/repo.ts`:

| Page | Source |
| --- | --- |
| `/docs/guide` | `README.md` |
| `/docs/language` | `CONTEXT.md` |
| `/docs/decisions/*` | `docs/adr/NNNN-*.md` |
| `/changelog` | `CHANGELOG.md`, parsed as Keep a Changelog |

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

Deploys happen from `.github/workflows/website.yml` on every push to `main`
that touches the site or its sources. It needs two repository secrets,
`CLOUDFLARE_API_TOKEN` (Workers Scripts: Edit, plus Zone: Workers Routes: Edit
for the custom domain) and `CLOUDFLARE_ACCOUNT_ID`.
