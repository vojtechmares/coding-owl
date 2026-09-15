import { useEffect, useState } from 'react';

/**
 * A short owl session, revealed one line at a time. The commands are the ones
 * in the README's quick start; the output is what those commands say.
 */
type Line =
  | { kind: 'cmd'; text: string }
  | { kind: 'out'; text: string; tone?: 'dim' | 'ok' | 'accent' }
  | { kind: 'gap' };

const SESSION: Line[] = [
  { kind: 'cmd', text: 'owl project add ~/code/my-app' },
  { kind: 'out', text: 'registered my-app, base main', tone: 'dim' },
  { kind: 'gap' },
  { kind: 'cmd', text: 'owl add "Add a --json flag to status"' },
  { kind: 'out', text: 'queued job 1 in my-app, planned first', tone: 'dim' },
  { kind: 'gap' },
  { kind: 'out', text: '22:41  idle for 10m, on AC power', tone: 'dim' },
  { kind: 'out', text: '22:41  run 1 of job 1 on owl/job-1', tone: 'accent' },
  { kind: 'out', text: '23:17  agent exited, check test: ok', tone: 'ok' },
  { kind: 'out', text: '23:17  job 1 waiting for review', tone: 'ok' },
  { kind: 'gap' },
  { kind: 'cmd', text: 'owl status' },
  { kind: 'out', text: 'JOB  PROJECT  STATE   BRANCH     RUNS', tone: 'dim' },
  { kind: 'out', text: '1    my-app   review  owl/job-1  1' },
];

const STEP_MS = 420;

export default function Terminal() {
  const [shown, setShown] = useState(SESSION.length);

  useEffect(() => {
    const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduce) return;
    setShown(0);
    let i = 0;
    const id = setInterval(() => {
      i += 1;
      setShown(i);
      if (i >= SESSION.length) clearInterval(id);
    }, STEP_MS);
    return () => clearInterval(id);
  }, []);

  return (
    <div className="panel overflow-hidden font-mono text-[13px] leading-6">
      <div className="flex items-center gap-2 border-b border-line bg-neutral-50 px-3.5 py-2">
        <span className="dot text-line-strong" aria-hidden="true" />
        <span className="dot text-line-strong" aria-hidden="true" />
        <span className="dot text-line-strong" aria-hidden="true" />
        <span className="label ml-2">owl session</span>
        <span className="label ml-auto">zsh</span>
      </div>
      <pre className="m-0 min-h-[21.5rem] overflow-x-auto px-4 py-3 whitespace-pre-wrap text-ink" aria-live="polite">
        {SESSION.slice(0, shown).map((line, i) => {
          if (line.kind === 'gap') return <span key={i}>{'\n'}</span>;
          if (line.kind === 'cmd') {
            return (
              <span key={i} className="block">
                <span className="text-accent select-none">$ </span>
                {line.text}
              </span>
            );
          }
          const tone =
            line.tone === 'dim'
              ? 'text-ink-faint'
              : line.tone === 'ok'
                ? 'text-teal-700'
                : line.tone === 'accent'
                  ? 'text-accent-strong'
                  : 'text-ink';
          return (
            <span key={i} className={`block ${tone}`}>
              {line.text}
            </span>
          );
        })}
        {shown < SESSION.length && (
          <span className="inline-block h-4 w-2 translate-y-0.5 animate-pulse bg-accent" aria-hidden="true" />
        )}
      </pre>
    </div>
  );
}
