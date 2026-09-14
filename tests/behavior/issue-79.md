# Issue #79: Switch the desktop frontend from npm to pnpm

The desktop app is Wails with a React frontend under `cmd/owl-desktop/frontend`
(ADR-0009). Its dependencies were installed and built with npm: the Wails
configuration named npm for install, build and the dev watcher, an npm lockfile
was committed, CI and the release workflow cached npm's store keyed on that
lockfile, and the Makefile and ADR-0009 named npm among the prerequisites.

The change is that pnpm is the only package manager for the frontend: every
place that invoked npm invokes pnpm, the npm lockfile is gone and a pnpm
lockfile is committed, and the pnpm version is pinned in `package.json` so a
local build and CI resolve the same way. Upgrading a frontend dependency, the
Vite or TypeScript configuration, and the Homebrew formula are out of scope.

The scenarios read the repository as a developer or CI would, and build the
app the way `make desktop` does.

## Scenarios

### S1 - the Wails configuration invokes pnpm and nothing else
Given the repository
When `cmd/owl-desktop/wails.json` is read
Then `frontend:install` runs `pnpm install --frozen-lockfile`
And `frontend:build` runs `pnpm build`
And `frontend:dev:watcher` runs `pnpm dev`

### S2 - the frontend pins pnpm and carries its lockfile
Given the repository
When `cmd/owl-desktop/frontend` is read
Then `package.json` carries a `packageManager` field naming pnpm at an exact version
And `pnpm-lock.yaml` is present and committed
And `package-lock.json` is absent

### S3 - CI and the release workflow use pnpm and its cache
Given the repository
When `.github/workflows/ci.yml` and `.github/workflows/release.yml` are read
Then each desktop job sets pnpm up before Node
And each caches `pnpm` keyed on `cmd/owl-desktop/frontend/pnpm-lock.yaml`
And neither names npm or the npm lockfile

### S4 - the app builds with pnpm and no npm on PATH
Given the Wails CLI, Node and pnpm, an `npm` on PATH that fails when run, and `OWL_DESKTOP_BUILD=1`
When `make desktop` runs
Then it exits 0 and leaves an app bundle under `cmd/owl-desktop/build/bin`
And without `OWL_DESKTOP_BUILD` the scenario is skipped, not failed

### S5 - nothing in the repository still invokes npm for the frontend
Given the repository, without `node_modules`, `dist` and `.git`
When it is searched for an npm invocation - `npm install`, `npm run`, `npm ci`, `cache: npm` - or for `package-lock.json`
Then nothing is found
And the Makefile, README and ADR-0009 name pnpm among the desktop app's prerequisites
