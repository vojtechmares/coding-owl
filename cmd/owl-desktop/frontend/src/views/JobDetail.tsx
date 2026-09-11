// JobDetail is owl jobs show: the Job, its Runs, and tabs for the plan, the
// handoff, what Verification said, what the branch changed, the live log and
// the system prompt. Accept, drop, start, pause and resume are the daemon's
// RPCs.

import { useEffect, useRef, useState } from "react";
import {
  api,
  ago,
  errorText,
  orNone,
  runOutcome,
  runPill,
  usePoll,
  useEvent,
  when,
  EVENT_LOG_END,
  EVENT_LOG_LINE,
  type JobDetails,
  type LogEnd,
  type LogLine,
  type Run,
} from "../lib/api";
import { Banner, Button, Empty, Panel, StatePill, Verdict } from "../components/ui";

type Tab = "plan" | "handoff" | "verification" | "diff" | "log" | "prompt";

const tabs: { key: Tab; label: string }[] = [
  { key: "plan", label: "Plan" },
  { key: "handoff", label: "Handoff" },
  { key: "verification", label: "Verification" },
  { key: "diff", label: "Diff" },
  { key: "log", label: "Log" },
  { key: "prompt", label: "System prompt" },
];

export function JobDetail({ id, onBack }: { id: number; onBack: () => void }) {
  const { data, error, refresh } = usePoll(() => api.job(id), 2000, [id]);
  const [tab, setTab] = useState<Tab>("plan");
  const [note, setNote] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);

  const act = async (what: () => Promise<unknown>, done: string) => {
    setBusy(true);
    try {
      await what();
      setNote(done);
    } catch (err) {
      setNote(errorText(err));
    } finally {
      setBusy(false);
      await refresh();
    }
  };

  const j = data?.Job;
  const inProgress = data?.Runs.find((r) => !r.Outcome);
  const running = inProgress !== undefined;

  return (
    <>
      <div className="back">
        <Button onClick={onBack}>← Back</Button>
      </div>
      <div className="page-title">
        <h1>
          Job {id} {j ? <StatePill state={j.State} /> : null}
        </h1>
        <div className="actions">
          {inProgress && !inProgress.Paused ? (
            <Button onClick={() => act(() => api.pause(), "Paused; the machine is yours")} disabled={busy}>
              Pause
            </Button>
          ) : null}
          {inProgress && inProgress.Paused ? (
            <Button kind="primary" onClick={() => act(() => api.resume(), "Resumed")} disabled={busy}>
              Resume
            </Button>
          ) : null}
          {j && j.State === "pending" ? (
            <Button
              kind="primary"
              onClick={() =>
                act(async () => {
                  const r = await api.start();
                  if (!r.started) throw new Error("Nothing pending to run");
                  if (r.job.ID !== id) throw new Error(`Started job ${r.job.ID}, which is at the head of the queue`);
                }, "Started")
              }
              disabled={busy}
            >
              Start next
            </Button>
          ) : null}
          {j && j.State === "review" ? (
            <>
              <Button kind="primary" onClick={() => act(() => api.accept(id, false), "Accepted")} disabled={busy}>
                Accept
              </Button>
              <Button kind="danger" onClick={() => act(() => api.drop(id, false), "Dropped")} disabled={busy}>
                Drop
              </Button>
            </>
          ) : null}
          {j && (j.State === "blocked" || j.State === "exhausted") ? (
            <Button kind="danger" onClick={() => act(() => api.drop(id, true), "Dropped")} disabled={busy}>
              Drop
            </Button>
          ) : null}
        </div>
      </div>
      {error ? <Banner>{error}</Banner> : null}
      {note ? <Banner>{note}</Banner> : null}
      {inProgress && inProgress.Paused ? (
        <Banner>
          Run {inProgress.ID} is paused: the Agent and everything it started are frozen. Resume continues it where
          it was; left for the whole grace window, it is ended and the Job goes back in the queue.
        </Banner>
      ) : null}

      {j ? (
        <Panel>
          <div className="detail-head">
            <Field k="Project" v={j.Project} />
            <Field k="Attempts" v={`${j.TTL} left`} />
            <Field k="Account" v={orNone(j.Account)} />
            <Field k="Planned" v={j.Planned ? "yes" : "no"} />
            <Field k="Source" v={`${j.Source}:${j.SourceRef}`} mono />
            <Field k="Created" v={when(j.Created)} />
            <Field k="Branch" v={orNone(j.Branch)} mono />
            <Field k="Worktree" v={orNone(j.Worktree)} mono />
            {j.Reason ? <Field k="Reason" v={j.Reason} wide /> : null}
            <Field k="Prompt" v={j.Prompt} wide />
          </div>
        </Panel>
      ) : null}

      {data ? (
        <Panel title="Runs">
          {data.Runs.length === 0 ? (
            <Empty>This Job has not run yet.</Empty>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Run</th>
                  <th>Attempt</th>
                  <th>Phase</th>
                  <th>Outcome</th>
                  <th>Exit</th>
                  <th>Started</th>
                  <th>Ended</th>
                  <th>Error</th>
                </tr>
              </thead>
              <tbody>
                {data.Runs.map((r) => (
                  <tr key={r.ID} className="row">
                    <td className="mono">{r.ID}</td>
                    <td>{r.Attempt}</td>
                    <td>{r.Phase || "-"}</td>
                    <td>
                      <StatePill state={runPill(r)} /> <span className="dim">{runOutcome(r)}</span>
                    </td>
                    <td className="mono">{r.ExitCode < 0 ? "-" : r.ExitCode}</td>
                    <td className="dim">{ago(r.Started)}</td>
                    <td className="dim">{r.Ended ? ago(r.Ended) : "-"}</td>
                    <td className="dim">{r.Error}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Panel>
      ) : null}

      {data ? (
        <Panel>
          <div className="tabs">
            {tabs.map((t) => (
              <button key={t.key} className={tab === t.key ? "active" : ""} onClick={() => setTab(t.key)}>
                {t.label}
                {t.key === "log" && running ? " ●" : ""}
              </button>
            ))}
          </div>
          {tab === "plan" ? (
            <Doc
              text={data.Job.Plan}
              none={data.Job.Planned ? "No plan yet: the first Run writes one." : "This Job is not planned; it is carried out straight from its prompt."}
            />
          ) : null}
          {tab === "handoff" ? <Doc text={data.Handoff} none="No handoff on the branch yet." /> : null}
          {tab === "verification" ? <Checks d={data} /> : null}
          {tab === "diff" ? <Diff d={data} /> : null}
          {tab === "log" ? <Log runs={data.Runs} /> : null}
          {tab === "prompt" ? <Prompts d={data} /> : null}
        </Panel>
      ) : null}
    </>
  );
}

function Field({ k, v, mono, wide }: { k: string; v: string; mono?: boolean; wide?: boolean }) {
  return (
    <div className={wide ? "wide" : ""}>
      <div className="k">{k}</div>
      <div className={`v ${mono ? "mono" : ""}`}>{v}</div>
    </div>
  );
}

// Prompts is what Owl put in front of every Agent it started for this Job: the
// one that did the work, and the one that judged it (ADR-0017).
function Prompts({ d }: { d: JobDetails }) {
  if (!d.VerifierSystemPrompt) return <Doc text={d.SystemPrompt} none="" />;
  return (
    <>
      <Doc text={d.SystemPrompt} none="" />
      <h2>Verifier system prompt</h2>
      <Doc text={d.VerifierSystemPrompt} none="" />
    </>
  );
}

function Doc({ text, none }: { text: string; none: string }) {
  if (!text) return <Empty>{none}</Empty>;
  return <pre className="code">{text}</pre>;
}

function Checks({ d }: { d: JobDetails }) {
  if (d.Checks.length === 0) return <Empty>No Run of this Job has been verified yet.</Empty>;
  return (
    <>
      {d.Checks.map((c) => (
        <div className="check" key={c.Name}>
          <div className="name">
            <strong>{c.Name}</strong>
            <Verdict passed={c.Passed} />
            <span className="cmd mono">{c.Command}</span>
            {!c.Passed && c.Reason ? <span className="dim">{c.Reason}</span> : null}
          </div>
          {c.Output ? <pre className="code">{c.Output}</pre> : null}
        </div>
      ))}
    </>
  );
}

function Diff({ d }: { d: JobDetails }) {
  const files = d.Diff?.Files ?? [];
  if (files.length === 0) return <Empty>The branch has changed nothing against the base branch.</Empty>;
  return (
    <table>
      <thead>
        <tr>
          <th>File</th>
          <th>Added</th>
          <th>Removed</th>
        </tr>
      </thead>
      <tbody>
        {files.map((f) => (
          <tr key={f.Path}>
            <td className="mono">{f.Path}</td>
            <td className="mono diff-add">+{f.Insertions}</td>
            <td className="mono diff-del">-{f.Deletions}</td>
          </tr>
        ))}
        <tr>
          <td className="dim">
            {files.length} {files.length === 1 ? "file" : "files"} changed
          </td>
          <td className="mono diff-add">+{d.Diff.Insertions}</td>
          <td className="mono diff-del">-{d.Diff.Deletions}</td>
        </tr>
      </tbody>
    </table>
  );
}

// Log follows one Run's structured output, the most recent by default, as
// the daemon streams it.
function Log({ runs }: { runs: Run[] }) {
  const latest = runs.length > 0 ? runs[runs.length - 1].ID : undefined;
  const [runId, setRunId] = useState<number | undefined>(latest);
  const [lines, setLines] = useState<string[]>([]);
  const [ended, setEnded] = useState<string | undefined>(undefined);
  const [err, setErr] = useState<string | undefined>(undefined);
  const box = useRef<HTMLPreElement>(null);

  const chosen = runId ?? latest;

  useEffect(() => {
    if (chosen === undefined) return;
    let alive = true;
    setLines([]);
    setEnded(undefined);
    setErr(undefined);
    api.followLog(chosen).catch((e) => {
      if (alive) setErr(errorText(e));
    });
    return () => {
      alive = false;
      void api.stopLog(chosen);
    };
  }, [chosen]);

  useEvent<LogLine>(EVENT_LOG_LINE, (ev) => {
    if (ev.runId !== chosen) return;
    setLines((prev) => [...prev, ev.line]);
  });
  useEvent<LogEnd>(EVENT_LOG_END, (ev) => {
    if (ev.runId !== chosen) return;
    setEnded(ev.error ? `stream ended: ${ev.error}` : "end of log");
  });

  useEffect(() => {
    const el = box.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [lines.length]);

  if (runs.length === 0) return <Empty>No Run to show a log for.</Empty>;
  return (
    <>
      <div className="log-head">
        <span>Run</span>
        <select value={chosen} onChange={(e) => setRunId(Number(e.target.value))}>
          {runs.map((r) => (
            <option key={r.ID} value={r.ID}>
              {r.ID} · attempt {r.Attempt} · {r.Phase || "-"} · {runOutcome(r)}
            </option>
          ))}
        </select>
        <span className="faint">{ended ?? (err ? err : "following")}</span>
        <span className="faint">{lines.length} lines</span>
      </div>
      <pre className="code log" ref={box}>
        {lines.join("\n")}
      </pre>
    </>
  );
}
