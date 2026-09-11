// Overview is owl status: what is running, how the Jobs stand, and what is
// waiting for a decision.

import { useState } from "react";
import {
  api,
  ago,
  errorText,
  runOutcome,
  runPill,
  type Job,
  type Machine,
  type Overview as OverviewData,
} from "../lib/api";
import { Banner, Button, Empty, Panel, StatePill, Tile } from "../components/ui";
import { JobsTable } from "./JobsTable";

export function Overview({
  data,
  error,
  refresh,
  onOpen,
}: {
  data: OverviewData | undefined;
  error: string | undefined;
  refresh: () => Promise<void>;
  onOpen: (id: number) => void;
}) {
  const [note, setNote] = useState<string | undefined>(undefined);
  const [busy, setBusy] = useState(false);

  const act = async (what: () => Promise<unknown>, done: (r: unknown) => string) => {
    setBusy(true);
    try {
      const r = await what();
      setNote(done(r));
    } catch (err) {
      setNote(errorText(err));
    } finally {
      setBusy(false);
      await refresh();
    }
  };

  const start = () =>
    act(
      () => api.start(),
      (r) => {
        const res = r as { started: boolean; job: Job; run: { ID: number } };
        return res.started ? `Started run ${res.run.ID} for job ${res.job.ID}` : "Nothing pending to run";
      },
    );
  const pause = () =>
    act(
      () => api.pause(),
      (r) => `Paused run ${(r as { ID: number }).ID}; the machine is yours`,
    );
  const resume = () =>
    act(
      () => api.resume(),
      (r) => `Resumed run ${(r as { ID: number }).ID}`,
    );
  const accept = (j: Job) => act(() => api.accept(j.ID, false), () => `Accepted job ${j.ID}`);
  const drop = (j: Job) => act(() => api.drop(j.ID, false), () => `Dropped job ${j.ID}`);

  // One Agent runs at a time (ADR-0029), so pause and resume take no
  // argument: they act on the Run in progress, whichever it is.
  const frozen = data?.Running.some((r) => r.Run.Paused) ?? false;
  const going = data?.Running.some((r) => !r.Run.Paused) ?? false;
  const counts = data?.Counts ?? [];
  const empty = data && data.Running.length === 0 && counts.length === 0 && data.Unfinished.length === 0;

  return (
    <>
      <div className="page-title">
        <h1>Overview</h1>
        <div className="actions">
          {going ? (
            <Button onClick={pause} disabled={busy}>
              Pause
            </Button>
          ) : null}
          {frozen ? (
            <Button kind="primary" onClick={resume} disabled={busy}>
              Resume
            </Button>
          ) : null}
          <Button kind="primary" onClick={start} disabled={busy}>
            Start next
          </Button>
        </div>
      </div>
      {error ? <Banner>{error}</Banner> : null}
      {note ? <Banner>{note}</Banner> : null}

      <Panel title="Machine">
        <div className="tiles">
          <Tile label="Idle" value={<StatePill state={machinePill(data?.Machine)} />} />
          <Tile label="Last input" value={data?.Machine?.Read ? sinceInput(data.Machine.Since) : "-"} />
          <Tile label="Power" value={powerLabel(data?.Machine)} />
        </div>
        {machineDetail(data?.Machine) ? <div className="dim">{machineDetail(data?.Machine)}</div> : null}
      </Panel>

      <Panel title="Running">
        {!data || data.Running.length === 0 ? (
          <Empty>No Run in progress.</Empty>
        ) : (
          <table>
            <thead>
              <tr>
                <th>Run</th>
                <th>Job</th>
                <th>Project</th>
                <th>Phase</th>
                <th>Agent</th>
                <th>Attempt</th>
                <th>Started</th>
                <th>Prompt</th>
              </tr>
            </thead>
            <tbody>
              {data.Running.map((r) => (
                <tr key={r.Run.ID} className="row" onClick={() => onOpen(r.Job.ID)}>
                  <td className="mono">{r.Run.ID}</td>
                  <td className="mono">{r.Job.ID}</td>
                  <td>{r.Job.Project}</td>
                  <td>{r.Run.Phase || "-"}</td>
                  <td>
                    <StatePill state={runPill(r.Run)} /> <span className="dim">{runOutcome(r.Run)}</span>
                  </td>
                  <td>{r.Run.Attempt}</td>
                  <td className="dim">{ago(r.Run.Started)}</td>
                  <td className="prompt">{r.Job.Prompt}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel title="Jobs">
        {empty ? (
          <Empty>Nothing queued yet. Register a Project and queue a Job with owl add.</Empty>
        ) : (
          <div className="tiles">
            {counts.map((c) => (
              <Tile key={c.State} label={c.State} value={c.Count} />
            ))}
          </div>
        )}
      </Panel>

      {data && data.Awaiting.length > 0 ? (
        <Panel title="Awaiting a decision">
          <JobsTable
            jobs={data.Awaiting}
            onOpen={onOpen}
            actions={(j) => (
              <div className="btn-row">
                <Button onClick={() => accept(j)} disabled={busy}>
                  Accept
                </Button>
                <Button kind="danger" onClick={() => drop(j)} disabled={busy}>
                  Drop
                </Button>
              </div>
            )}
          />
        </Panel>
      ) : null}

      {data && data.Blocked.length > 0 ? (
        <Panel title="Blocked">
          <JobsTable jobs={data.Blocked} onOpen={onOpen} reason />
        </Panel>
      ) : null}

      {data && data.Exhausted.length > 0 ? (
        <Panel title="Exhausted">
          <JobsTable jobs={data.Exhausted} onOpen={onOpen} reason />
        </Panel>
      ) : null}

      {data && data.Unfinished.length > 0 ? (
        <Panel title="Unfinished work found by garbage collection">
          <table>
            <thead>
              <tr>
                <th>Job</th>
                <th>Project</th>
                <th>Path</th>
                <th>Why it was left</th>
              </tr>
            </thead>
            <tbody>
              {data.Unfinished.map((u) => (
                <tr key={u.Path} className="row" onClick={() => u.Job && onOpen(u.Job)}>
                  <td className="mono">{u.Job || "-"}</td>
                  <td>{u.Project}</td>
                  <td className="mono">{u.Path}</td>
                  <td className="dim">{u.Reason}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </Panel>
      ) : null}
    </>
  );
}

// machinePill is whether Owl may work: Idle is the state the whole product
// waits for (ADR-0011), so it is said in the same words the CLI says it in.
// One lowercase word, because that is what a pill's class is made of.
function machinePill(m: Machine | undefined): string {
  if (!m || !m.Read) return "unknown";
  return m.Idle ? "idle" : "busy";
}

// machineDetail is why the machine is not one Owl may work on, or why it could
// not be read.
function machineDetail(m: Machine | undefined): string {
  if (!m) return "";
  return m.Read ? (m.Idle ? "" : m.Detail) : m.Detail || "The machine could not be read.";
}

function powerLabel(m: Machine | undefined): string {
  if (!m || !m.Read) return "-";
  return m.OnPower ? "AC power" : "Battery";
}

// sinceInput is how long it has been since anybody touched the machine. Go
// keeps a duration in nanoseconds, and a person reads seconds and minutes.
function sinceInput(ns: number): string {
  const seconds = Math.floor(ns / 1e9);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}
