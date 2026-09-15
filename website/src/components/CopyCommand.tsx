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
    <div className="glass-strong flex items-center gap-3 rounded-(--radius-control) py-2.5 pr-2 pl-4 font-mono text-[13.5px] text-silver">
      <span aria-hidden="true" className="text-ink-faint select-none">
        $
      </span>
      <code className="min-w-0 flex-1 overflow-x-auto whitespace-nowrap">{command}</code>
      {canCopy && (
        <button
          type="button"
          onClick={copy}
          aria-live="polite"
          className="shrink-0 rounded-lg border border-glass-edge bg-glass px-2.5 py-1 font-sans text-xs text-ink-dim transition-colors hover:bg-glass-strong hover:text-ink"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      )}
    </div>
  );
}
