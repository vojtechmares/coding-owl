// The window: a sidebar with the daemon's state and the navigation, and one
// view at a time. State lives in the daemon; the app only shows it.

import { useState } from "react";
import { api, usePoll } from "./lib/api";
import { Overview } from "./views/Overview";
import { Projects } from "./views/Projects";
import { Skills } from "./views/Skills";
import { Queue } from "./views/Queue";
import { Jobs } from "./views/Jobs";
import { JobDetail } from "./views/JobDetail";

type View = "overview" | "projects" | "skills" | "queue" | "jobs";

const views: { key: View; label: string }[] = [
  { key: "overview", label: "Overview" },
  { key: "projects", label: "Projects" },
  { key: "skills", label: "Skills" },
  { key: "queue", label: "Queue" },
  { key: "jobs", label: "Jobs" },
];

export default function App() {
  const [view, setView] = useState<View>("overview");
  const [jobId, setJobId] = useState<number | undefined>(undefined);
  const status = usePoll(() => api.status(), 2000);
  const overview = usePoll(() => api.overview(), 2000);

  const st = status.data;
  const down = !st || !st.running;
  const counts = new Map((overview.data?.Counts ?? []).map((c) => [c.State, c.Count]));
  const pending = counts.get("pending") ?? 0;
  const running = overview.data?.Running.length ?? 0;
  const awaiting = overview.data?.Awaiting.length ?? 0;

  const open = (id: number) => setJobId(id);
  const close = () => setJobId(undefined);
  const go = (v: View) => {
    setJobId(undefined);
    setView(v);
  };

  return (
    <>
      <div className="titlebar" />
      <div className="shell">
        <aside className="sidebar">
          <div className="brand">
            <span className="owl">◉</span>
            Coding Owl
          </div>
          <nav className="nav">
            {views.map((v) => (
              <button key={v.key} className={view === v.key && jobId === undefined ? "active" : ""} onClick={() => go(v.key)}>
                <span>{v.label}</span>
                <span className="count">
                  {v.key === "queue" && pending > 0 ? pending : null}
                  {v.key === "overview" && running > 0 ? `${running} running` : null}
                  {v.key === "jobs" && awaiting > 0 ? `${awaiting} to review` : null}
                </span>
              </button>
            ))}
          </nav>
          <div className="spacer" />
          <div className={`daemon ${down ? "down" : ""}`}>
            <div>
              <span className="dot" />
              {down ? "Daemon not running" : `Daemon ${st.version}`}
            </div>
            {down ? null : <div className="faint">up {st.uptime}</div>}
            <div className="socket">{st?.socketPath ?? ""}</div>
          </div>
        </aside>
        <main className="content">
          {down ? (
            <div className="banner">
              The daemon is not answering. Start it with <span className="mono">owl daemon run</span>, or install it as
              a service with <span className="mono">owl daemon install</span>.
              {st?.error ? (
                <div className="mono" style={{ marginTop: "var(--space-2)" }}>
                  {st.error}
                </div>
              ) : null}
            </div>
          ) : null}
          {jobId !== undefined ? (
            <JobDetail id={jobId} onBack={close} />
          ) : view === "overview" ? (
            <Overview data={overview.data} error={down ? undefined : overview.error} refresh={overview.refresh} onOpen={open} />
          ) : view === "projects" ? (
            <Projects />
          ) : view === "skills" ? (
            <Skills />
          ) : view === "queue" ? (
            <Queue onOpen={open} />
          ) : (
            <Jobs onOpen={open} />
          )}
        </main>
      </div>
    </>
  );
}
