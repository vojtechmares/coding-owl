// Small pieces the views share: pills, panels, tiles, banners, buttons.

import type { ReactNode } from "react";

export function Panel({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section className="panel">
      {title ? <h2>{title}</h2> : null}
      {children}
    </section>
  );
}

export function StatePill({ state }: { state: string }) {
  return <span className={`pill ${state}`}>{state}</span>;
}

export function Verdict({ passed }: { passed: boolean }) {
  return <span className={`pill ${passed ? "pass" : "fail"}`}>{passed ? "passed" : "failed"}</span>;
}

export function Tile({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="tile">
      <div className="label">{label}</div>
      <div className="value">{value}</div>
    </div>
  );
}

// A banner is either a note about what just happened or an alert about what is
// wrong. Only the alert takes colour, so that when the app is red something
// actually is.
export function Banner({ children, alert }: { children: ReactNode; alert?: boolean }) {
  return <div className={`banner ${alert ? "alert" : ""}`}>{children}</div>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

export function Button({
  children,
  onClick,
  kind,
  disabled,
}: {
  children: ReactNode;
  onClick: () => void;
  kind?: "primary" | "danger";
  disabled?: boolean;
}) {
  return (
    <button className={`btn ${kind ?? ""}`} onClick={onClick} disabled={disabled}>
      {children}
    </button>
  );
}
