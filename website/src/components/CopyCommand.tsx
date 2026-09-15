import { useEffect, useState } from 'react';

interface Props {
  command: string;
}

/** The install command, with a button that copies it. */
export default function CopyCommand({ command }: Props) {
  const [copied, setCopied] = useState(false);
  const [canCopy, setCanCopy] = useState(false);

  useEffect(() => {
    setCanCopy(typeof navigator !== 'undefined' && Boolean(navigator.clipboard));
  }, []);

  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1600);
    return () => clearTimeout(t);
  }, [copied]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  }

  return (
    <div className="panel flex items-center gap-3 py-2 pr-2 pl-3.5 font-mono text-[13px] text-ink">
      <span aria-hidden="true" className="text-accent select-none">
        $
      </span>
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap">{command}</code>
      {canCopy && (
        <button
          type="button"
          onClick={copy}
          aria-live="polite"
          className="shrink-0 rounded-(--radius-control) border border-line bg-neutral-50 px-2 py-1 text-[11px] tracking-wide text-ink-dim uppercase transition-colors hover:border-line-strong hover:text-ink"
        >
          {copied ? 'copied' : 'copy'}
        </button>
      )}
    </div>
  );
}
