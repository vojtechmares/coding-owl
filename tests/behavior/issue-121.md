# Issue #121: a CLI manual and a focused Getting started page, split out of the all-in-one guide

The website's `docs` collection holds one page today, the whole of `README.md`
as `/docs/guide`. This issue splits the parts a working user reaches for into
pages of their own, under `docs/guide/` in the repository:

| File | Published at | Title |
| --- | --- | --- |
| `docs/guide/getting-started.md` | `/docs/getting-started` | Getting started |
| `docs/guide/jobs.md` | `/docs/jobs` | Working with Jobs |
| `docs/guide/projects-and-accounts.md` | `/docs/projects-and-accounts` | Projects and Accounts |

The scenarios are observable from outside: files on disk, the collection the
website builds from, and the `owl` binary's own `--help`, which is what decides
whether a documented command is a command Owl has. S4 is the one with teeth -
it walks every `owl ...` invocation the new pages show against the real command
tree, so a manual that drifts from the binary fails the suite.

`the new pages` below means those three files. `the manual pages` means the
last two of them.

## Scenarios

### S1 - Getting started is a page of its own, and it is the six-step happy path
Given the repository
When `docs/guide/getting-started.md` is read
Then it is titled `# Getting started`
And it walks, in this order, adding an Account, registering a Project,
committing a `.coding-owl.yaml`, queueing a Job, letting it run or starting it,
and reviewing what it did
And each of those steps is a `##` section showing the command that does it

### S2 - the new pages are published pages with URLs of their own
Given `website/src/content.config.ts`
When the `docs` collection's `repoFiles` list is read
Then it registers `getting-started`, `jobs` and `projects-and-accounts`
And each carries a title, a description, an order, and a source path under
`docs/guide/` that exists on disk
And the orders are distinct and put all three ahead of `guide`

### S3 - the manual covers the everyday commands, in the areas the issue names
Given `docs/guide/jobs.md` and `docs/guide/projects-and-accounts.md`
When they are read
Then `docs/guide/jobs.md` shows `owl add`, `owl queue list`,
`owl queue reorder`, `owl queue remove`, `owl start`, `owl pause`,
`owl resume`, `owl logs` with `-f`, `owl status`, `owl jobs show`,
`owl jobs accept`, `owl jobs drop` and `owl jobs extend`
And `docs/guide/projects-and-accounts.md` shows every `owl project` verb and
every `owl account` verb, including `owl account instructions`

### S4 - every command the new pages show is a command the binary has, with the flags it shows
Given the built `owl` binary
When every `owl ...` line in a fenced block and every `owl ...` inline span in
the new pages is taken
Then each resolves to a command path the binary has
And every flag it passes appears in that command's `--help`
And no invocation names a subcommand of a command that has none of that name

### S5 - the pages link to published pages, not to raw GitHub
Given the new pages
When their relative Markdown links are resolved against the directory each
file is in
Then every target exists in the repository
And every target that has a published page - `README.md` and the new pages
themselves - is a key of `PUBLISHED` in `website/src/loaders/repo.ts`

### S6 - the guide no longer carries a second copy of the quick start
Given `README.md`
When it is read
Then it carries no numbered quick-start steps
And it links to `docs/guide/getting-started.md` and to both manual pages
And those links are keys of `PUBLISHED`, so the guide page republishes them
rather than sending a reader to GitHub
