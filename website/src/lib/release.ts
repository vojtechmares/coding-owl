/*
 * The latest release, read from git at build time: the newest `v*` tag
 * reachable from the commit being built, which is what the release workflow
 * publishes. A checkout without tags, or one that predates the first
 * release, gives `undefined`, and the site says "unreleased" rather than
 * failing the build.
 */
import { execFileSync } from 'node:child_process';

function describe(): string | undefined {
  try {
    const out = execFileSync('git', ['describe', '--tags', '--abbrev=0', '--match', 'v[0-9]*'], {
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
    }).trim();
    return /^v\d+\.\d+\.\d+$/.test(out) ? out : undefined;
  } catch {
    return undefined;
  }
}

/** `v0.1.0`, or `undefined` before the first release. Prereleases do not count. */
export const latestRelease: string | undefined = describe();
